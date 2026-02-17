package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/events"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func TestNewServer(t *testing.T) {
	server := NewServer(&MockStore{}, &MockBroker{}, &MockWorkflowService{}, config.Config{})
	require.NotNil(t, server)
	require.NotNil(t, server.Router())
}

func TestHealth(t *testing.T) {
	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, "ok", payload["status"])
}

func TestReady(t *testing.T) {
	t.Run("ready when dependencies healthy", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListRuns", mock.Anything).Return([]store.RunSummary{}, nil).Once()

		toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/ready", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		defer toolRunner.Close()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
		defer server.Close()

		resp, err := http.Get(server.URL + "/ready")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var payload readinessResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.Equal(t, "ok", payload.Status)
		require.Equal(t, "ok", payload.Subsystems["store"].Status)
		require.Equal(t, "ok", payload.Subsystems["tool_runner"].Status)
		storeMock.AssertExpectations(t)
	})

	t.Run("degraded when store unavailable", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListRuns", mock.Anything).Return(nil, errors.New("db unavailable")).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/ready")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var payload readinessResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.Equal(t, "degraded", payload.Status)
		require.Equal(t, "error", payload.Subsystems["store"].Status)
		storeMock.AssertExpectations(t)
	})

	t.Run("falls back to /health when /ready missing", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListRuns", mock.Anything).Return([]store.RunSummary{}, nil).Once()

		requested := make([]string, 0, 2)
		toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requested = append(requested, r.URL.Path)
			if r.URL.Path == "/ready" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok"}`))
				return
			}
			http.Error(w, "unexpected path", http.StatusBadRequest)
		}))
		defer toolRunner.Close()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
		defer server.Close()

		resp, err := http.Get(server.URL + "/ready")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, []string{"/ready", "/health"}, requested)
		storeMock.AssertExpectations(t)
	})
}

func TestCreateRun(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		workflows := &MockWorkflowService{}

		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("CreateRun", mock.Anything, mock.MatchedBy(func(run store.Run) bool {
			return run.ID != "" && run.Status == "running" && run.CreatedAt != "" && run.UpdatedAt != ""
		})).Return(nil).Once()
		storeMock.On("NextSeq", mock.Anything, mock.AnythingOfType("string")).Return(int64(1), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "run.started" && event.Seq == 1
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()
		workflows.On("StartRun", mock.Anything, mock.AnythingOfType("string")).Return(nil).Once()

		server := newTestServer(t, storeMock, brokerMock, workflows, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var payload map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.NotEmpty(t, payload["run_id"])
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
		workflows.AssertExpectations(t)
	})

	t.Run("llm required", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusPreconditionFailed, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("CreateRun", mock.Anything, mock.Anything).Return(errors.New("boom")).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestDeleteRun(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("DeleteRun", mock.Anything, "run-1").Return(nil).Once()

	server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/runs/run-1", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	storeMock.AssertExpectations(t)
}

func TestGetRun(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListRuns", mock.Anything).Return([]store.RunSummary{
			{
				ID:           "run-1",
				Status:       "partial",
				Title:        "Website build",
				CreatedAt:    "2026-02-07T00:00:00Z",
				UpdatedAt:    "2026-02-07T00:00:10Z",
				MessageCount: 3,
			},
		}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/runs/run-1")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var payload runSummaryResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		require.Equal(t, "run-1", payload.ID)
		require.Equal(t, "partial", payload.Status)
		require.Equal(t, "Website build", payload.Title)
		storeMock.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListRuns", mock.Anything).Return([]store.RunSummary{}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Get(server.URL + "/runs/missing")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestListRunSteps(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("ListRunSteps", mock.Anything, "run-1").Return([]store.RunStep{
		{
			RunID:     "run-1",
			ID:        "assistant_reply",
			Kind:      "step",
			Name:      "Generate assistant reply",
			Status:    "failed",
			Source:    "llm",
			Seq:       1,
			StartedAt: "2026-02-07T00:00:00Z",
			Error:     "provider timeout",
			Diagnostics: map[string]any{
				"reason": "timeout",
			},
		},
		{
			RunID:       "run-1",
			ID:          "tool-1",
			Kind:        "tool",
			Name:        "editor.write",
			Status:      "completed",
			Source:      "tool_runner",
			Seq:         2,
			StartedAt:   "2026-02-07T00:00:01Z",
			CompletedAt: "2026-02-07T00:00:02Z",
		},
	}, nil).Once()

	server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	resp, err := http.Get(server.URL + "/runs/run-1/steps")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var payload listRunStepsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Len(t, payload.Steps, 2)

	require.Equal(t, "assistant_reply", payload.Steps[0].ID)
	require.Equal(t, "step", payload.Steps[0].Kind)
	require.Equal(t, "failed", payload.Steps[0].Status)
	require.Equal(t, "provider timeout", payload.Steps[0].Error)
	require.Equal(t, "timeout", payload.Steps[0].Diagnostics["reason"])

	require.Equal(t, "tool-1", payload.Steps[1].ID)
	require.Equal(t, "tool", payload.Steps[1].Kind)
	require.Equal(t, "completed", payload.Steps[1].Status)
	storeMock.AssertExpectations(t)
}

func TestAddMessage(t *testing.T) {
	t.Run("success user", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		workflows := &MockWorkflowService{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("AddMessage", mock.Anything, mock.MatchedBy(func(msg store.Message) bool {
			return msg.RunID == "run-1" && msg.Role == "user" && msg.Content == "hello"
		})).Return(nil).Once()
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, nil).Once()
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(2), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "message.added" && event.Seq == 2
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()
		workflows.On("SignalMessage", mock.Anything, "run-1", "hello").Return(nil).Once()

		server := newTestServer(t, storeMock, brokerMock, workflows, config.Config{})
		defer server.Close()

		payload := strings.NewReader(`{"content":"hello"}`)
		resp, err := http.Post(server.URL+"/runs/run-1/messages", "application/json", payload)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
		workflows.AssertExpectations(t)
	})

	t.Run("indexes memory when enabled", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		workflows := &MockWorkflowService{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("AddMessage", mock.Anything, mock.MatchedBy(func(msg store.Message) bool {
			return msg.RunID == "run-1" && msg.Role == "user" && strings.Contains(msg.Content, "memory indexing")
		})).Return(nil).Once()
		storeMock.On("GetMemorySettings", mock.Anything).Return(&store.MemorySettings{Enabled: true}, nil).Once()
		storeMock.On("UpsertMemoryEntry", mock.Anything, mock.MatchedBy(func(entry store.MemoryEntry) bool {
			return entry.Content != "" && entry.Metadata["source"] == "chat_message"
		})).Return(true, nil).Once()
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(2), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "message.added" && event.Seq == 2
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()
		workflows.On("SignalMessage", mock.Anything, "run-1", mock.AnythingOfType("string")).Return(nil).Once()

		cfg := config.Config{MemoryMinContentChars: 1, MemoryChunkChars: 50, MemoryChunkOverlap: 10, MemoryMaxChunks: 2}
		server := newTestServer(t, storeMock, brokerMock, workflows, cfg)
		defer server.Close()

		payload := strings.NewReader(`{"content":"hello memory indexing"}`)
		resp, err := http.Post(server.URL+"/runs/run-1/messages", "application/json", payload)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
		workflows.AssertExpectations(t)
	})

	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs/run-1/messages", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("llm required", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := strings.NewReader(`{"content":"hello"}`)
		resp, err := http.Post(server.URL+"/runs/run-1/messages", "application/json", payload)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusPreconditionFailed, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("store error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("AddMessage", mock.Anything, mock.Anything).Return(errors.New("boom")).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		payload := strings.NewReader(`{"content":"hello"}`)
		resp, err := http.Post(server.URL+"/runs/run-1/messages", "application/json", payload)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestExecuteAutomationRun(t *testing.T) {
	t.Run("creates run, waits for terminal event, and returns final response with diagnostics", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		workflows := &MockWorkflowService{}

		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("CreateRun", mock.Anything, mock.MatchedBy(func(run store.Run) bool {
			return run.ID != "" && run.Status == "running" && run.Phase == "planning"
		})).Return(nil).Once()
		workflows.On("StartRun", mock.Anything, mock.AnythingOfType("string")).Return(nil).Once()
		storeMock.On("NextSeq", mock.Anything, mock.AnythingOfType("string")).Return(int64(1), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "run.started" && event.Seq == 1
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		storeMock.On("AddMessage", mock.Anything, mock.MatchedBy(func(msg store.Message) bool {
			return msg.RunID != "" &&
				msg.Role == "user" &&
				strings.Contains(msg.Content, "top current news stories") &&
				msg.Metadata["browser_mode"] == "user_tab" &&
				msg.Metadata["browser_preferred_browser"] == "brave"
		})).Return(nil).Once()
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, nil).Once()
		workflows.On("SignalMessage", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil).Once()
		storeMock.On("NextSeq", mock.Anything, mock.AnythingOfType("string")).Return(int64(2), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "message.added" && event.Seq == 2
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		storeMock.On("ListEvents", mock.Anything, mock.AnythingOfType("string"), int64(0)).Return([]store.RunEvent{
			{
				Type: "tool.completed",
				Payload: map[string]any{
					"tool_name": "browser.extract",
					"output": map[string]any{
						"url":   "https://example.com/news/2026/02/06/rwa-liquidity-shifts",
						"title": "RWA Liquidity Shifts",
						"diagnostics": map[string]any{
							"status":              "ok",
							"extractable_content": true,
							"word_count":          240,
						},
					},
				},
			},
			{
				Type: "tool.completed",
				Payload: map[string]any{
					"tool_name": "browser.extract",
					"output": map[string]any{
						"url":   "https://cointelegraph.com/terms-and-privacy",
						"title": "Terms and Privacy",
						"diagnostics": map[string]any{
							"status":              "empty",
							"reason_code":         "no_extractable_content",
							"reason_detail":       "legal_or_policy_page",
							"extractable_content": false,
							"word_count":          42,
						},
					},
				},
			},
			{
				Type: "run.completed",
				Payload: map[string]any{
					"status":            "completed",
					"phase":             "completed",
					"completion_reason": "research_evidence_complete",
				},
			},
		}, nil).Once()
		storeMock.On("ListMessages", mock.Anything, mock.AnythingOfType("string")).Return([]store.Message{
			{Role: "assistant", Content: "Here is the February 2026 RWA and crypto summary with sources."},
		}, nil).Once()

		server := newTestServer(t, storeMock, brokerMock, workflows, config.Config{})
		defer server.Close()

		payload := strings.NewReader(`{
			"prompt":"Browse the web and give me the top current news stories surrounding RWAs and crypto February 2026 and a comprehensive summary",
			"browser_mode":"user_tab",
			"browser_preferred_browser":"brave",
			"timeout_ms":60000,
			"poll_interval_ms":50
		}`)
		resp, err := http.Post(server.URL+"/automation/execute", "application/json", payload)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out automationExecuteResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.NotEmpty(t, out.RunID)
		require.Equal(t, "completed", out.Status)
		require.Equal(t, "completed", out.Phase)
		require.Equal(t, "research_evidence_complete", out.CompletionReason)
		require.False(t, out.TimedOut)
		require.Contains(t, out.FinalResponse, "RWA and crypto summary")
		require.Equal(t, 1, out.Diagnostics.UsableSources)
		require.Equal(t, 0, out.Diagnostics.BlockedSources)
		require.Equal(t, 1, out.Diagnostics.LowQuality)
		require.Equal(t, "run.completed", out.Diagnostics.TerminalEvent)
		require.Equal(t, "research_evidence_complete", out.Diagnostics.TerminalReason)
		require.Len(t, out.Diagnostics.Sources, 2)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
		workflows.AssertExpectations(t)
	})

	t.Run("returns accepted when wait_for_completion is false", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		workflows := &MockWorkflowService{}

		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("CreateRun", mock.Anything, mock.MatchedBy(func(run store.Run) bool {
			return run.ID != "" && run.Status == "running"
		})).Return(nil).Once()
		workflows.On("StartRun", mock.Anything, mock.AnythingOfType("string")).Return(nil).Once()
		storeMock.On("NextSeq", mock.Anything, mock.AnythingOfType("string")).Return(int64(1), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "run.started"
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()
		storeMock.On("AddMessage", mock.Anything, mock.AnythingOfType("store.Message")).Return(nil).Once()
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, nil).Once()
		workflows.On("SignalMessage", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil).Once()
		storeMock.On("NextSeq", mock.Anything, mock.AnythingOfType("string")).Return(int64(2), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "message.added"
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		server := newTestServer(t, storeMock, brokerMock, workflows, config.Config{})
		defer server.Close()

		payload := strings.NewReader(`{"prompt":"research rwx","wait_for_completion":false}`)
		resp, err := http.Post(server.URL+"/automation/execute", "application/json", payload)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		var out automationExecuteResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.NotEmpty(t, out.RunID)
		require.Equal(t, "running", out.Status)
		require.Equal(t, "planning", out.Phase)
		require.NotNil(t, out.Diagnostics.Sources)
		require.Len(t, out.Diagnostics.Sources, 0)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
		workflows.AssertExpectations(t)
	})
}

func TestResumeRun(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		workflows := &MockWorkflowService{}

		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("ListRuns", mock.Anything).Return([]store.RunSummary{
			{ID: "run-1", Status: "failed", CreatedAt: "2026-02-07T00:00:00Z", UpdatedAt: "2026-02-07T00:05:00Z"},
		}, nil).Once()
		storeMock.On("AddMessage", mock.Anything, mock.MatchedBy(func(msg store.Message) bool {
			return msg.RunID == "run-1" && msg.Role == "user" && strings.Contains(msg.Content, "Continue from the latest checkpoint")
		})).Return(nil).Once()
		storeMock.On("GetMemorySettings", mock.Anything).Return(nil, nil).Once()
		workflows.On("ResumeRun", mock.Anything, "run-1", mock.AnythingOfType("string")).Return(nil).Once()
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(9), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "run.resumed" && event.Seq == 9
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		server := newTestServer(t, storeMock, brokerMock, workflows, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs/run-1/resume", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
		workflows.AssertExpectations(t)
	})

	t.Run("already running", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("ListRuns", mock.Anything).Return([]store.RunSummary{
			{ID: "run-1", Status: "running", CreatedAt: "2026-02-07T00:00:00Z", UpdatedAt: "2026-02-07T00:05:00Z"},
		}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, &MockWorkflowService{}, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs/run-1/resume", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusConflict, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		storeMock.On("ListRuns", mock.Anything).Return([]store.RunSummary{}, nil).Once()

		server := newTestServer(t, storeMock, &MockBroker{}, &MockWorkflowService{}, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs/run-1/resume", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		storeMock.AssertExpectations(t)
	})
}

func TestCancelRun(t *testing.T) {
	storeMock := &MockStore{}
	brokerMock := &MockBroker{}
	workflows := &MockWorkflowService{}

	storeMock.On("NextSeq", mock.Anything, "run-2").Return(int64(7), nil).Once()
	storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
		return event.Type == "run.cancelled" && event.Seq == 7
	})).Return(nil).Once()
	brokerMock.On("Publish", mock.Anything).Once()
	workflows.On("CancelRun", mock.Anything, "run-2").Return(nil).Once()

	server := newTestServer(t, storeMock, brokerMock, workflows, config.Config{})
	defer server.Close()

	resp, err := http.Post(server.URL+"/runs/run-2/cancel", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	storeMock.AssertExpectations(t)
	brokerMock.AssertExpectations(t)
	workflows.AssertExpectations(t)
}

func TestIngestEvent(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs/run-1/events", "application/json", strings.NewReader("{"))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing type", func(t *testing.T) {
		server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
		defer server.Close()

		resp, err := http.Post(server.URL+"/runs/run-1/events", "application/json", strings.NewReader(`{"source":"tool"}`))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("success", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(3), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "tool.completed" && event.Seq == 3
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		server := newTestServer(t, storeMock, brokerMock, nil, config.Config{})
		defer server.Close()

		payload := `{"type":"tool.completed","source":"tool","timestamp":"2024-01-01T00:00:00Z"}`
		resp, err := http.Post(server.URL+"/runs/run-1/events", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
	})

	t.Run("default timestamp", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(4), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Timestamp != "" && event.Type == "tool.started" && event.Seq == 4
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		server := newTestServer(t, storeMock, brokerMock, nil, config.Config{})
		defer server.Close()

		payload := `{"type":"tool.started","source":"tool"}`
		resp, err := http.Post(server.URL+"/runs/run-1/events", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
	})

	t.Run("stores artifacts from tool output", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(5), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "tool.completed" && event.Seq == 5
		})).Return(nil).Once()
		storeMock.On("UpsertArtifact", mock.Anything, mock.MatchedBy(func(artifact store.Artifact) bool {
			return artifact.ID == "artifact-1" && artifact.RunID == "run-1" && artifact.URI == "http://example.com/file.txt"
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		server := newTestServer(t, storeMock, brokerMock, nil, config.Config{})
		defer server.Close()

		payload := `{"type":"tool.completed","source":"tool","payload":{"artifacts":[{"artifact_id":"artifact-1","type":"file","uri":"http://example.com/file.txt","content_type":"text/plain","size_bytes":12}]}}`
		resp, err := http.Post(server.URL+"/runs/run-1/events", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
	})

	t.Run("stores process state from process tool output", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(7), nil).Once()
		storeMock.On("AppendEvent", mock.Anything, mock.MatchedBy(func(event store.RunEvent) bool {
			return event.Type == "tool.completed" && event.Seq == 7
		})).Return(nil).Once()
		storeMock.On("UpsertRunProcess", mock.Anything, mock.MatchedBy(func(process store.RunProcess) bool {
			return process.RunID == "run-1" &&
				process.ProcessID == "proc-1" &&
				process.Status == "running"
		})).Return(nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		server := newTestServer(t, storeMock, brokerMock, nil, config.Config{})
		defer server.Close()

		payload := `{"type":"tool.completed","source":"tool","payload":{"tool_name":"process.start","output":{"process_id":"proc-1","command":"npm","args":["run","dev"],"cwd":".","status":"running","pid":1234,"started_at":"2026-02-07T00:00:00Z","preview_urls":["http://localhost:3000"]}}}`
		resp, err := http.Post(server.URL+"/runs/run-1/events", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
	})

	t.Run("transient events skip storage", func(t *testing.T) {
		storeMock := &MockStore{}
		brokerMock := &MockBroker{}
		storeMock.On("NextSeq", mock.Anything, "run-1").Return(int64(6), nil).Once()
		brokerMock.On("Publish", mock.Anything).Once()

		server := newTestServer(t, storeMock, brokerMock, nil, config.Config{})
		defer server.Close()

		payload := `{"type":"browser.snapshot","source":"browser_worker","payload":{"uri":"http://example.com/live.png","transient":true}}`
		resp, err := http.Post(server.URL+"/runs/run-1/events", "application/json", strings.NewReader(payload))
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
	})
}

func TestListArtifacts(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("ListArtifacts", mock.Anything, "run-1").Return([]store.Artifact{{
		ID:          "artifact-1",
		RunID:       "run-1",
		Type:        "file",
		URI:         "http://example.com/file.txt",
		ContentType: "text/plain",
		SizeBytes:   5,
		CreatedAt:   "2024-01-01T00:00:00Z",
	}}, nil).Once()

	server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	resp, err := http.Get(server.URL + "/runs/run-1/artifacts")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload listArtifactsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Len(t, payload.Artifacts, 1)
	require.Equal(t, "artifact-1", payload.Artifacts[0].ID)
	require.Equal(t, 1, payload.Total)
	require.Equal(t, 1, payload.Page)
	require.Equal(t, 50, payload.PageSize)
	storeMock.AssertExpectations(t)
}

func TestListArtifacts_FilterAndPagination(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("ListArtifacts", mock.Anything, "run-1").Return([]store.Artifact{
		{
			ID:             "artifact-1",
			RunID:          "run-1",
			Type:           "screenshot",
			Category:       "image",
			URI:            "http://example.com/preview-1.png",
			ContentType:    "image/png",
			Labels:         []string{"preview", "hero"},
			SearchableText: "homepage preview",
			CreatedAt:      "2026-02-07T00:00:02Z",
		},
		{
			ID:             "artifact-2",
			RunID:          "run-1",
			Type:           "screenshot",
			Category:       "image",
			URI:            "http://example.com/preview-2.png",
			ContentType:    "image/png",
			Labels:         []string{"preview"},
			SearchableText: "pricing preview",
			CreatedAt:      "2026-02-07T00:00:01Z",
		},
		{
			ID:          "artifact-3",
			RunID:       "run-1",
			Type:        "file",
			Category:    "document",
			URI:         "http://example.com/readme.txt",
			ContentType: "text/plain",
			Labels:      []string{"notes"},
			CreatedAt:   "2026-02-07T00:00:00Z",
		},
	}, nil).Once()

	server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	resp, err := http.Get(server.URL + "/runs/run-1/artifacts?query=preview&category=image&content_type=image/png&label=preview&page=1&page_size=1")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload listArtifactsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, 2, payload.Total)
	require.Equal(t, 2, payload.TotalPages)
	require.Equal(t, 1, payload.Page)
	require.Equal(t, 1, payload.PageSize)
	require.Len(t, payload.Artifacts, 1)
	require.Equal(t, "artifact-1", payload.Artifacts[0].ID)

	storeMock.AssertExpectations(t)
}

func TestListWorkspace(t *testing.T) {
	toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "editor.list", req["tool_name"])
		writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"root": "/", "entries": []any{}}})
	}))
	defer toolRunner.Close()

	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
	defer server.Close()

	resp, err := http.Get(server.URL + "/runs/run-1/workspace")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload toolRunnerResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, "completed", payload.Status)
}

func TestWorkspaceTree_List(t *testing.T) {
	toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "editor.list", req["tool_name"])
		writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"entries": []any{
			map[string]any{"name": "notes.txt", "path": "notes.txt", "type": "file"},
		}}})
	}))
	defer toolRunner.Close()

	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
	defer server.Close()

	resp, err := http.Get(server.URL + "/runs/run-1/workspace/tree")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	files, ok := payload["files"].([]any)
	require.True(t, ok)
	require.Len(t, files, 1)
}

func TestWorkspaceFile_Read(t *testing.T) {
	toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "editor.read", req["tool_name"])
		writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"content": "hello"}})
	}))
	defer toolRunner.Close()

	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
	defer server.Close()

	resp, err := http.Get(server.URL + "/runs/run-1/workspace/file?path=notes.txt")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload toolRunnerResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, "completed", payload.Status)
	require.Equal(t, "hello", payload.Output["content"])
}

func TestWorkspaceFile_Write(t *testing.T) {
	toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "editor.write", req["tool_name"])
		writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"path": "notes.txt"}})
	}))
	defer toolRunner.Close()

	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
	defer server.Close()

	payload := `{"path":"notes.txt","content":"hello"}`
	req, err := http.NewRequest(http.MethodPut, server.URL+"/runs/run-1/workspace/file", strings.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestWorkspaceProcessExec(t *testing.T) {
	toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "process.exec", req["tool_name"])
		writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"stdout": "ok\n", "stderr": "", "exit_code": 0}})
	}))
	defer toolRunner.Close()

	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
	defer server.Close()

	resp, err := http.Post(server.URL+"/runs/run-1/processes/exec", "application/json", strings.NewReader(`{"command":"echo","args":["ok"]}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload toolRunnerResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, "completed", payload.Status)
	require.Equal(t, "ok\n", payload.Output["stdout"])
}

func TestWorkspaceProcessLifecycleEndpoints(t *testing.T) {
	calls := make(chan string, 8)
	toolRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		toolName, _ := req["tool_name"].(string)
		calls <- toolName
		switch toolName {
		case "process.start":
			writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"process_id": "proc-1", "status": "running"}})
		case "process.status":
			writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"process_id": "proc-1", "status": "running"}})
		case "process.logs":
			writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{
				"process_id": "proc-1",
				"logs": []any{
					map[string]any{"stream": "stdout", "text": "ready on http://localhost:3000"},
				},
			}})
		case "process.stop":
			writeJSON(w, toolRunnerResponse{Status: "completed", Output: map[string]any{"process_id": "proc-1", "status": "exited"}})
		default:
			t.Fatalf("unexpected tool call: %s", toolName)
		}
	}))
	defer toolRunner.Close()

	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{ToolRunnerURL: toolRunner.URL})
	defer server.Close()

	startResp, err := http.Post(server.URL+"/runs/run-1/processes/start", "application/json", strings.NewReader(`{"command":"npm","args":["run","dev"]}`))
	require.NoError(t, err)
	defer startResp.Body.Close()
	require.Equal(t, http.StatusOK, startResp.StatusCode)

	statusResp, err := http.Get(server.URL + "/runs/run-1/processes/proc-1")
	require.NoError(t, err)
	defer statusResp.Body.Close()
	require.Equal(t, http.StatusOK, statusResp.StatusCode)

	logsResp, err := http.Get(server.URL + "/runs/run-1/processes/proc-1/logs?tail=10")
	require.NoError(t, err)
	defer logsResp.Body.Close()
	require.Equal(t, http.StatusOK, logsResp.StatusCode)

	stopResp, err := http.Post(server.URL+"/runs/run-1/processes/proc-1/stop", "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	defer stopResp.Body.Close()
	require.Equal(t, http.StatusOK, stopResp.StatusCode)

	require.Equal(t, "process.start", <-calls)
	require.Equal(t, "process.status", <-calls)
	require.Equal(t, "process.logs", <-calls)
	require.Equal(t, "process.stop", <-calls)
}

func TestWorkspaceProcessFallbackToStore(t *testing.T) {
	storeMock := &MockStore{}
	storeMock.On("ListRunProcesses", mock.Anything, "run-1").Return([]store.RunProcess{
		{
			RunID:       "run-1",
			ProcessID:   "proc-1",
			Command:     "npm",
			Args:        []string{"run", "dev"},
			Cwd:         ".",
			Status:      "running",
			PID:         1234,
			StartedAt:   "2026-02-07T00:00:00Z",
			PreviewURLs: []string{"http://localhost:3000"},
		},
	}, nil).Once()
	storeMock.On("GetRunProcess", mock.Anything, "run-1", "proc-1").Return(&store.RunProcess{
		RunID:       "run-1",
		ProcessID:   "proc-1",
		Command:     "npm",
		Status:      "running",
		PID:         1234,
		StartedAt:   "2026-02-07T00:00:00Z",
		PreviewURLs: []string{"http://localhost:3000"},
	}, nil).Once()

	server := newTestServer(t, storeMock, &MockBroker{}, nil, config.Config{ToolRunnerURL: "http://127.0.0.1:1"})
	defer server.Close()

	resp, err := http.Get(server.URL + "/runs/run-1/processes")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	statusResp, err := http.Get(server.URL + "/runs/run-1/processes/proc-1")
	require.NoError(t, err)
	defer statusResp.Body.Close()
	require.Equal(t, http.StatusOK, statusResp.StatusCode)
	storeMock.AssertExpectations(t)
}

func TestStreamEvents(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		storeMock := &MockStore{}
		broker := events.NewBroker()
		storeMock.On("ListEvents", mock.Anything, "run-9", int64(2)).Return([]store.RunEvent{
			{RunID: "run-9", Seq: 1, Type: "run.started", Timestamp: "2024-01-01T00:00:00Z"},
		}, nil).Once()

		server := newTestServer(t, storeMock, broker, nil, config.Config{})
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/runs/run-9/events?after_seq=2", nil)
		require.NoError(t, err)

		client := &http.Client{Timeout: time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		go func() {
			time.Sleep(20 * time.Millisecond)
			broker.Publish(events.RunEvent{RunID: "run-9", Seq: 2, Type: "message.added", Ts: "2024-01-01T00:00:01Z"})
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil && !errors.Is(err, context.Canceled) {
			require.NoError(t, err)
		}
		text := string(body)
		require.Contains(t, text, "event: run_event")
		require.Contains(t, text, "run.started")
		require.Contains(t, text, "message.added")
		storeMock.AssertExpectations(t)
	})

	t.Run("list error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListEvents", mock.Anything, "run-1", int64(0)).Return(nil, errors.New("boom")).Once()

		req := httptest.NewRequest(http.MethodGet, "/runs/run-1/events", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "run-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server := NewServer(storeMock, events.NewBroker(), nil, config.Config{})
		server.streamEvents(w, req)

		require.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("no flusher", func(t *testing.T) {
		storeMock := &MockStore{}
		req := httptest.NewRequest(http.MethodGet, "/runs/run-1/events", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "run-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := &noFlushWriter{}

		server := NewServer(storeMock, events.NewBroker(), nil, config.Config{})
		server.streamEvents(w, req)

		require.Equal(t, http.StatusInternalServerError, w.status)
	})

	t.Run("closed channel", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("ListEvents", mock.Anything, "run-1", int64(0)).Return([]store.RunEvent{}, nil).Once()
		brokerMock := &MockBroker{}
		ch := make(chan events.RunEvent)
		close(ch)
		brokerMock.On("Subscribe", mock.Anything, "run-1").Return(ch).Once()

		req := httptest.NewRequest(http.MethodGet, "/runs/run-1/events", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "run-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server := NewServer(storeMock, brokerMock, nil, config.Config{})
		server.streamEvents(w, req)

		storeMock.AssertExpectations(t)
		brokerMock.AssertExpectations(t)
	})
}

func TestCORSMiddleware(t *testing.T) {
	server := newTestServer(t, &MockStore{}, &MockBroker{}, nil, config.Config{})
	defer server.Close()

	req, err := http.NewRequest(http.MethodOptions, server.URL+"/health", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
	require.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "OPTIONS")
}

func TestEnsureLLMConfigured(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, errors.New("boom")).Once()

		req := httptest.NewRequest(http.MethodPost, "/runs", nil)
		w := httptest.NewRecorder()
		server := NewServer(storeMock, &MockBroker{}, nil, config.Config{})

		require.False(t, server.ensureLLMConfigured(w, req.Context()))
		require.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("missing", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(nil, nil).Once()
		w := httptest.NewRecorder()
		server := NewServer(storeMock, &MockBroker{}, nil, config.Config{})

		require.False(t, server.ensureLLMConfigured(w, context.Background()))
		require.Equal(t, http.StatusPreconditionFailed, w.Result().StatusCode)
		storeMock.AssertExpectations(t)
	})

	t.Run("configured", func(t *testing.T) {
		storeMock := &MockStore{}
		storeMock.On("GetLLMSettings", mock.Anything).Return(&store.LLMSettings{Provider: "openai"}, nil).Once()
		server := NewServer(storeMock, &MockBroker{}, nil, config.Config{})

		require.True(t, server.ensureLLMConfigured(httptest.NewRecorder(), context.Background()))
		storeMock.AssertExpectations(t)
	})
}

func TestParseAfterSeq(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/runs/run-1/events?after_seq=9", nil)
	require.Equal(t, int64(9), parseAfterSeq("run-1", req))

	req = httptest.NewRequest(http.MethodGet, "/runs/run-1/events", nil)
	req.Header.Set("Last-Event-ID", "run-1:12")
	require.Equal(t, int64(12), parseAfterSeq("run-1", req))

	req = httptest.NewRequest(http.MethodGet, "/runs/run-1/events", nil)
	req.Header.Set("Last-Event-ID", "other:12")
	require.Equal(t, int64(0), parseAfterSeq("run-1", req))

	req = httptest.NewRequest(http.MethodGet, "/runs/run-1/events", nil)
	req.Header.Set("Last-Event-ID", "bad")
	require.Equal(t, int64(0), parseAfterSeq("run-1", req))

	req = httptest.NewRequest(http.MethodGet, "/runs/run-1/events", nil)
	req.Header.Set("Last-Event-ID", "run-1:abc")
	require.Equal(t, int64(0), parseAfterSeq("run-1", req))

	req = httptest.NewRequest(http.MethodGet, "/runs/run-1/events?after_seq=bad", nil)
	require.Equal(t, int64(0), parseAfterSeq("run-1", req))
}

func TestStart(t *testing.T) {
	storeMock := &MockStore{}
	brokerMock := &MockBroker{}
	server := NewServer(storeMock, brokerMock, nil, config.Config{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	result := make(chan error, 1)
	go func() {
		result <- server.Start(ctx, addr)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	err = <-result
	require.Error(t, err)
}

func TestSendSSE(t *testing.T) {
	buf := &bytes.Buffer{}
	w := bufio.NewWriter(buf)

	writer := &bufferWriter{Writer: w, header: http.Header{}}
	sendSSE(writer, events.RunEvent{RunID: "run-1", Seq: 5, Type: "run.started"})
	w.Flush()

	text := buf.String()
	require.Contains(t, text, "id: run-1:5")
	require.Contains(t, text, "event: run_event")
	require.Contains(t, text, "run.started")
}

type noFlushWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *noFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *noFlushWriter) WriteHeader(status int) {
	w.status = status
}

func (w *noFlushWriter) Write(data []byte) (int, error) {
	return w.body.Write(data)
}

type bufferWriter struct {
	*bufio.Writer
	header http.Header
}

func (w *bufferWriter) Header() http.Header {
	return w.header
}

func (w *bufferWriter) WriteHeader(statusCode int) {
}
