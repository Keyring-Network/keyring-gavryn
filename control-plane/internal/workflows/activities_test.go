package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/llm"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/personality"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/secrets"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
	"github.com/stretchr/testify/require"
)

type stubStore struct {
	listRunsFunc               func(ctx context.Context) ([]store.RunSummary, error)
	listMessagesFunc           func(ctx context.Context, runID string) ([]store.Message, error)
	appendEventFunc            func(ctx context.Context, event store.RunEvent) error
	nextSeqFunc                func(ctx context.Context, runID string) (int64, error)
	listEventsFunc             func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error)
	listRunStepsFunc           func(ctx context.Context, runID string) ([]store.RunStep, error)
	getLLMSettingsFunc         func(ctx context.Context) (*store.LLMSettings, error)
	getMemorySettingsFunc      func(ctx context.Context) (*store.MemorySettings, error)
	getPersonalitySettingsFunc func(ctx context.Context) (*store.PersonalitySettings, error)
	searchMemoryFunc           func(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error)
	searchMemoryEmbeddingFunc  func(ctx context.Context, query string, embedding []float32, limit int) ([]store.MemoryEntry, error)
}

func (s *stubStore) ListRuns(ctx context.Context) ([]store.RunSummary, error) {
	if s.listRunsFunc != nil {
		return s.listRunsFunc(ctx)
	}
	return nil, nil
}
func (s *stubStore) DeleteRun(ctx context.Context, runID string) error       { return nil }
func (s *stubStore) CreateRun(ctx context.Context, run store.Run) error      { return nil }
func (s *stubStore) AddMessage(ctx context.Context, msg store.Message) error { return nil }
func (s *stubStore) ListMessages(ctx context.Context, runID string) ([]store.Message, error) {
	if s.listMessagesFunc != nil {
		return s.listMessagesFunc(ctx, runID)
	}
	return nil, nil
}
func (s *stubStore) GetLLMSettings(ctx context.Context) (*store.LLMSettings, error) {
	if s.getLLMSettingsFunc != nil {
		return s.getLLMSettingsFunc(ctx)
	}
	return nil, nil
}
func (s *stubStore) UpsertLLMSettings(ctx context.Context, settings store.LLMSettings) error {
	return nil
}
func (s *stubStore) ListSkills(ctx context.Context) ([]store.Skill, error) { return nil, nil }
func (s *stubStore) GetSkill(ctx context.Context, skillID string) (*store.Skill, error) {
	return nil, nil
}
func (s *stubStore) CreateSkill(ctx context.Context, skill store.Skill) error { return nil }
func (s *stubStore) UpdateSkill(ctx context.Context, skill store.Skill) error { return nil }
func (s *stubStore) DeleteSkill(ctx context.Context, skillID string) error    { return nil }
func (s *stubStore) ListSkillFiles(ctx context.Context, skillID string) ([]store.SkillFile, error) {
	return nil, nil
}
func (s *stubStore) UpsertSkillFile(ctx context.Context, file store.SkillFile) error { return nil }
func (s *stubStore) DeleteSkillFile(ctx context.Context, skillID string, path string) error {
	return nil
}
func (s *stubStore) ListContextNodes(ctx context.Context) ([]store.ContextNode, error) {
	return nil, nil
}
func (s *stubStore) GetContextFile(ctx context.Context, nodeID string) (*store.ContextNode, error) {
	return nil, nil
}
func (s *stubStore) CreateContextFolder(ctx context.Context, node store.ContextNode) error {
	return nil
}
func (s *stubStore) CreateContextFile(ctx context.Context, node store.ContextNode) error { return nil }
func (s *stubStore) DeleteContextNode(ctx context.Context, nodeID string) error          { return nil }
func (s *stubStore) GetMemorySettings(ctx context.Context) (*store.MemorySettings, error) {
	if s.getMemorySettingsFunc != nil {
		return s.getMemorySettingsFunc(ctx)
	}
	return nil, nil
}
func (s *stubStore) UpsertMemorySettings(ctx context.Context, settings store.MemorySettings) error {
	return nil
}
func (s *stubStore) UpsertMemoryEntry(ctx context.Context, entry store.MemoryEntry) (bool, error) {
	return true, nil
}
func (s *stubStore) GetPersonalitySettings(ctx context.Context) (*store.PersonalitySettings, error) {
	if s.getPersonalitySettingsFunc != nil {
		return s.getPersonalitySettingsFunc(ctx)
	}
	return nil, nil
}
func (s *stubStore) UpsertPersonalitySettings(ctx context.Context, settings store.PersonalitySettings) error {
	return nil
}
func (s *stubStore) SearchMemory(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
	if s.searchMemoryFunc != nil {
		return s.searchMemoryFunc(ctx, query, limit)
	}
	return nil, nil
}
func (s *stubStore) SearchMemoryWithEmbedding(ctx context.Context, query string, embedding []float32, limit int) ([]store.MemoryEntry, error) {
	if s.searchMemoryEmbeddingFunc != nil {
		return s.searchMemoryEmbeddingFunc(ctx, query, embedding, limit)
	}
	return nil, nil
}
func (s *stubStore) AppendEvent(ctx context.Context, event store.RunEvent) error {
	if s.appendEventFunc != nil {
		return s.appendEventFunc(ctx, event)
	}
	return nil
}
func (s *stubStore) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
	if s.listEventsFunc != nil {
		return s.listEventsFunc(ctx, runID, afterSeq)
	}
	return nil, nil
}
func (s *stubStore) ListRunSteps(ctx context.Context, runID string) ([]store.RunStep, error) {
	if s.listRunStepsFunc != nil {
		return s.listRunStepsFunc(ctx, runID)
	}
	return nil, nil
}
func (s *stubStore) UpsertRunProcess(ctx context.Context, process store.RunProcess) error { return nil }
func (s *stubStore) GetRunProcess(ctx context.Context, runID string, processID string) (*store.RunProcess, error) {
	return nil, nil
}
func (s *stubStore) ListRunProcesses(ctx context.Context, runID string) ([]store.RunProcess, error) {
	return nil, nil
}
func (s *stubStore) NextSeq(ctx context.Context, runID string) (int64, error) {
	if s.nextSeqFunc != nil {
		return s.nextSeqFunc(ctx, runID)
	}
	return 0, nil
}
func (s *stubStore) UpsertArtifact(ctx context.Context, artifact store.Artifact) error { return nil }
func (s *stubStore) ListArtifacts(ctx context.Context, runID string) ([]store.Artifact, error) {
	return nil, nil
}
func (s *stubStore) ListAutomations(ctx context.Context) ([]store.Automation, error) {
	return nil, nil
}
func (s *stubStore) GetAutomation(ctx context.Context, automationID string) (*store.Automation, error) {
	return nil, nil
}
func (s *stubStore) CreateAutomation(ctx context.Context, automation store.Automation) error {
	return nil
}
func (s *stubStore) UpdateAutomation(ctx context.Context, automation store.Automation) error {
	return nil
}
func (s *stubStore) DeleteAutomation(ctx context.Context, automationID string) error { return nil }
func (s *stubStore) ListAutomationInbox(ctx context.Context, automationID string) ([]store.AutomationInboxEntry, error) {
	return nil, nil
}
func (s *stubStore) CreateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	return nil
}
func (s *stubStore) UpdateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	return nil
}
func (s *stubStore) MarkAutomationInboxEntryRead(ctx context.Context, automationID string, entryID string) error {
	return nil
}
func (s *stubStore) MarkAutomationInboxReadAll(ctx context.Context, automationID string) error {
	return nil
}

type stubProvider struct {
	generate func(ctx context.Context, messages []llm.Message) (string, error)
}

func (p stubProvider) Generate(ctx context.Context, messages []llm.Message) (string, error) {
	return p.generate(ctx, messages)
}

type errorRoundTripper struct {
	err error
}

func (e errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, e.err
}

func waitForPayloadType(t *testing.T, ch <-chan map[string]any, expectedType string) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case payload := <-ch:
			eventType, _ := payload["type"].(string)
			if eventType == expectedType {
				return payload
			}
		case <-deadline:
			t.Fatalf("timed out waiting for payload type %q", expectedType)
		}
	}
}

func waitForPayloadTypes(t *testing.T, ch <-chan map[string]any, expectedTypes ...string) map[string]any {
	t.Helper()
	if len(expectedTypes) == 0 {
		t.Fatal("expected at least one payload type")
	}
	allowed := map[string]struct{}{}
	for _, expectedType := range expectedTypes {
		allowed[expectedType] = struct{}{}
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case payload := <-ch:
			eventType, _ := payload["type"].(string)
			if _, ok := allowed[eventType]; ok {
				return payload
			}
		case <-deadline:
			t.Fatalf("timed out waiting for payload types %q", strings.Join(expectedTypes, ", "))
		}
	}
}

func waitForRequestPath(t *testing.T, ch <-chan struct {
	path string
	body map[string]any
}, path string) struct {
	path string
	body map[string]any
} {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case request := <-ch:
			if request.path == path {
				return request
			}
		case <-deadline:
			t.Fatalf("timed out waiting for request path %q", path)
		}
	}
}

func TestNewRunActivities(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai"}, []byte("key"), "http://example.com/", "http://tools.example.com/")

	if activities == nil {
		t.Fatal("expected activities")
	}
	if activities.store != storeStub {
		t.Fatal("expected store")
	}
	if activities.controlPlane != "http://example.com" {
		t.Fatalf("expected control plane trimmed, got %s", activities.controlPlane)
	}
	if activities.toolRunner != "http://tools.example.com" {
		t.Fatalf("expected tool runner trimmed, got %s", activities.toolRunner)
	}
}

func TestGenerateAssistantReply_Success(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	var gotMessages []llm.Message
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				gotMessages = messages
				return "assistant response", nil
			}
			return "assistant response", nil
		}}, nil
	}

	cpRequests := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		cpRequests <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{
				{Role: "assistant", Content: ""},
				{Role: "user", Content: "Hello there"},
				{Role: "assistant", Content: "existing"},
			}, nil
		},
		getMemorySettingsFunc: func(ctx context.Context) (*store.MemorySettings, error) {
			return &store.MemorySettings{Enabled: true}, nil
		},
		searchMemoryFunc: func(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
			return []store.MemoryEntry{{Content: "remember this"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, []byte("key"), cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-1"})
	require.NoError(t, err)

	require.Len(t, gotMessages, 4)
	require.Equal(t, "system", gotMessages[0].Role)
	require.True(t, strings.HasPrefix(gotMessages[0].Content, "Relevant memory:"))
	require.Equal(t, "system", gotMessages[1].Role)
	require.Contains(t, gotMessages[1].Content, "System resources:")
	require.Equal(t, "user", gotMessages[2].Role)
	require.Equal(t, "assistant", gotMessages[3].Role)

	posted := <-cpRequests
	require.Equal(t, "assistant", posted["role"])
	require.Equal(t, "assistant response", posted["content"])
}

func TestGenerateAssistantReply_ToolLoopExecutes(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	responses := []string{
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"notes.txt\",\"content\":\"hello\"}}]}\n```",
		"final response",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return "", errors.New("too many calls")
			}
			response := responses[callCount]
			callCount++
			return response, nil
		}}, nil
	}

	toolCalls := make(chan map[string]any, 32)
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolCalls <- payload
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"path": "notes.txt"}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Write a file"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-1"})
	require.NoError(t, err)

	called := <-toolCalls
	require.Equal(t, "editor.write", called["tool_name"])

	posted := <-cpMessages
	require.Equal(t, "assistant", posted["role"])
	require.Equal(t, "final response", posted["content"])
}

func TestGenerateAssistantReply_RepromptsExecutionPromiseIntoToolCall(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	responses := []string{
		"I'll create the file now.",
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"summary.txt\",\"content\":\"done\"}}]}\n```",
		"Created summary.txt.",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return "", errors.New("too many calls")
			}
			response := responses[callCount]
			callCount++
			return response, nil
		}}, nil
	}

	toolCalls := make(chan map[string]any, 32)
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolCalls <- payload
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"path": "summary.txt"}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "create a summary file"}}, nil
		},
		listEventsFunc: func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
			return []store.RunEvent{{Type: "run.title.updated", Payload: map[string]any{"title": "existing"}}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-reprompt"})
	require.NoError(t, err)
	require.Equal(t, 3, callCount)

	called := <-toolCalls
	require.Equal(t, "editor.write", called["tool_name"])

	posted := <-cpMessages
	require.Equal(t, "Created summary.txt.", posted["content"])
}

func TestGenerateAssistantReply_RepromptsToolRequiredResponseWithoutPromisePhrase(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	responses := []string{
		"Starting task execution.",
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"index.js\",\"content\":\"console.log('ok')\"}}]}\n```",
		"Done. Created the file.",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return "", errors.New("too many calls")
			}
			response := responses[callCount]
			callCount++
			return response, nil
		}}, nil
	}

	toolCalls := make(chan map[string]any, 32)
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolCalls <- payload
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"path": "index.js"}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "create a starter file"}}, nil
		},
		listEventsFunc: func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
			return []store.RunEvent{{Type: "run.title.updated", Payload: map[string]any{"title": "existing"}}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-tool-required"})
	require.NoError(t, err)
	require.Equal(t, 3, callCount)

	called := <-toolCalls
	require.Equal(t, "editor.write", called["tool_name"])

	posted := <-cpMessages
	require.Equal(t, "Done. Created the file.", posted["content"])
}

func TestGenerateAssistantReply_MissingToolCallsReturnsPartialFallback(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			return "Starting task execution.", nil
		}}, nil
	}

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "browse the web for current defi news"}}, nil
		},
		listEventsFunc: func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
			return []store.RunEvent{{Type: "run.title.updated", Payload: map[string]any{"title": "existing"}}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, "http://tool-runner")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-missing-tool-calls"})
	require.NoError(t, err)
	require.Equal(t, maxToolIntentReprompts+1, callCount)

	posted := <-cpMessages
	require.Contains(t, strings.ToLower(posted["content"]), "returned prose instead of tool calls")
}

func TestGenerateAssistantReply_StopsAfterRepeatedInvalidToolBlocks(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			return "```tool\nnot-json\n```", nil
		}}, nil
	}

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Browse and summarize this site"}}, nil
		},
		listEventsFunc: func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
			return []store.RunEvent{{Type: "run.title.updated", Payload: map[string]any{"title": "existing"}}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, "http://tool-runner")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-invalid-tools"})
	require.NoError(t, err)
	require.Equal(t, maxToolRecoveryReprompts+1, callCount)

	posted := <-cpMessages
	require.Contains(t, strings.ToLower(posted["content"]), "could not parse valid tool instructions")
}

func TestGenerateAssistantReply_ToolRunnerUnavailable_RespondsFriendly(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	responses := []string{
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"notes.txt\",\"content\":\"hello\"}}]}\n```",
		"I cannot run tools right now because execution is unavailable.",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return responses[len(responses)-1], nil
			}
			value := responses[callCount]
			callCount++
			return value, nil
		}}, nil
	}

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Write a file"}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, "")
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-unavailable"})
	require.NoError(t, err)

	posted := <-cpMessages
	require.Equal(t, "I cannot run tools right now because execution is unavailable.", posted["content"])
}

func TestGenerateAssistantReply_ToolCallParseGuard(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\nnot json\n```", nil
			}
			return "I can continue without tools.", nil
		}}, nil
	}

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "hello"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, "")
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-2"})
	require.NoError(t, err)

	posted := <-cpMessages
	require.Equal(t, "I can continue without tools.", posted["content"])
}

func TestGenerateAssistantReply_OversizedToolPayloadStillExecutes(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	largeContent := strings.Repeat("a", 30000)
	responses := []string{
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"notes.txt\",\"content\":\"" + largeContent + "\"}}]}\n```",
		"final response",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return "", errors.New("too many calls")
			}
			response := responses[callCount]
			callCount++
			return response, nil
		}}, nil
	}

	toolCalls := make(chan map[string]any, 32)
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolCalls <- payload
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"path": "notes.txt"}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Write a large file"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-large-tool"})
	require.NoError(t, err)

	called := <-toolCalls
	require.Equal(t, "editor.write", called["tool_name"])

	posted := <-cpMessages
	require.Equal(t, "final response", posted["content"])
}

func TestGenerateAssistantReply_RecoversFromPartialFencedToolBlock(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	responses := []string{
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"notes.txt\",\"content\":\"hel",
		"lo\"}}]}\n```",
		"done",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return "", errors.New("too many calls")
			}
			response := responses[callCount]
			callCount++
			return response, nil
		}}, nil
	}

	toolCalls := make(chan map[string]any, 32)
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolCalls <- payload
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"path": "notes.txt"}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Write a file with partial response"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-partial-tool"})
	require.NoError(t, err)

	called := <-toolCalls
	require.Equal(t, "editor.write", called["tool_name"])

	posted := <-cpMessages
	require.Equal(t, "done", posted["content"])
}

func TestGenerateAssistantReply_RunIDRequired(t *testing.T) {
	activities := NewRunActivities(&stubStore{}, llm.Config{}, nil, "http://example.com", "")
	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{})
	require.EqualError(t, err, "run_id required")
}

func TestGenerateAssistantReply_ListMessagesError(t *testing.T) {
	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return nil, errors.New("list failed")
		},
	}
	activities := NewRunActivities(storeStub, llm.Config{Mode: "local"}, nil, "http://example.com", "")
	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-1"})
	require.EqualError(t, err, "list failed")
}

func TestGenerateAssistantReply_NoAPIKey(t *testing.T) {
	cpRequests := make(chan map[string]any, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		cpRequests <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
	}
	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", Model: "gpt"}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-2"})
	require.EqualError(t, err, "missing API key for provider")

	posted := waitForPayloadType(t, cpRequests, "run.failed")
	require.Equal(t, "run.failed", posted["type"])
}

func TestGenerateAssistantReply_InvalidModel(t *testing.T) {
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
	}
	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-3"})
	require.EqualError(t, err, "missing model for remote provider")
}

func TestGenerateAssistantReply_LLMErrorPostsTransientFallback(t *testing.T) {
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer llmServer.Close()

	cpRequests := make(chan struct {
		path string
		body map[string]any
	}, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		cpRequests <- struct {
			path string
			body map[string]any
		}{path: r.URL.Path, body: payload}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
	}
	activities := NewRunActivities(
		storeStub,
		llm.Config{Provider: "openai", Model: "gpt", OpenAIAPIKey: "key", BaseURL: llmServer.URL},
		nil,
		cpServer.URL,
		"",
	)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-4"})
	require.NoError(t, err)

	posted := waitForRequestPath(t, cpRequests, "/runs/run-4/messages")
	require.Equal(t, "/runs/run-4/messages", posted.path)
	content, _ := posted.body["content"].(string)
	require.Contains(t, strings.ToLower(content), "temporarily unavailable")
}

func TestGenerateAssistantReply_RetriesTransientLLMError(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount < 3 {
				return "", errors.New("opencode request failed: 429 rate limit")
			}
			return "assistant response", nil
		}}, nil
	}

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
		listEventsFunc: func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
			return []store.RunEvent{{Type: "run.title.updated", Payload: map[string]any{"title": "existing"}}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", Model: "gpt", OpenAIAPIKey: "key"}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-retry"})
	require.NoError(t, err)
	require.Equal(t, 2, callCount)

	posted := <-cpMessages
	require.Equal(t, "assistant", posted["role"])
	require.Contains(t, strings.ToLower(posted["content"]), "temporarily unavailable")
}

func TestGenerateAssistantReply_FailsOverProvidersOnGatewayErrors(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	primaryCalls := 0
	fallbackCalls := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		switch cfg.Provider {
		case "openai":
			return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
				primaryCalls++
				return "", errors.New("opencode request failed: 502 Bad Gateway error code: 500")
			}}, nil
		case "openrouter":
			return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
				fallbackCalls++
				return "fallback response", nil
			}}, nil
		default:
			return nil, errors.New("unexpected provider")
		}
	}

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
		listEventsFunc: func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
			return []store.RunEvent{{Type: "run.title.updated", Payload: map[string]any{"title": "existing"}}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{
		Provider:         "openai",
		Model:            "gpt",
		OpenAIAPIKey:     "primary",
		FallbackProvider: "openrouter",
		OpenRouterAPIKey: "fallback",
	}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-failover"})
	require.NoError(t, err)
	require.Equal(t, 1, primaryCalls)
	require.Equal(t, 1, fallbackCalls)

	posted := <-cpMessages
	require.Equal(t, "fallback response", posted["content"])
}

func TestGenerateAssistantReply_GatewayErrorDoesSingleAttemptWithoutFallback(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			return "", errors.New("opencode request failed: 502 Bad Gateway error code: 500")
		}}, nil
	}

	cpMessages := make(chan map[string]string, 8)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{
		Provider:     "openai",
		Model:        "gpt",
		OpenAIAPIKey: "key",
	}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-gateway-fast"})
	require.NoError(t, err)
	require.Equal(t, 1, callCount)

	posted := <-cpMessages
	require.Equal(t, "assistant", posted["role"])
	require.Contains(t, strings.ToLower(posted["content"]), "temporarily unavailable")
}

func TestBuildProviderCandidates_UsesPrimaryOnlyWithoutExplicitFallback(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	modelByProvider := map[string]string{}
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		modelByProvider[cfg.Provider] = cfg.Model
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			return "ok", nil
		}}, nil
	}

	activities := NewRunActivities(&stubStore{}, llm.Config{}, nil, "http://example.com", "")
	candidates, err := activities.buildProviderCandidates(llm.Config{
		Provider:         "openai",
		Model:            "gpt-4.1",
		OpenAIAPIKey:     "openai-key",
		OpenRouterAPIKey: "openrouter-key",
		OpenCodeAPIKey:   "opencode-key",
		CodexAuthPath:    "/tmp/codex/auth.json",
	}, "")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "openai", candidates[0].Name)
	require.Equal(t, "gpt-4.1", modelByProvider["openai"])
}

func TestBuildProviderCandidates_ExplicitFallbackSameProviderUsesFallbackModel(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	models := make([]string, 0, 4)
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		models = append(models, cfg.Model)
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			return "ok", nil
		}}, nil
	}

	activities := NewRunActivities(&stubStore{}, llm.Config{}, nil, "http://example.com", "")
	candidates, err := activities.buildProviderCandidates(llm.Config{
		Provider:         "openai",
		Model:            "gpt-4.1",
		FallbackProvider: "openai",
		FallbackModel:    "gpt-4.1-mini",
		OpenAIAPIKey:     "openai-key",
	}, "")
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "openai", candidates[0].Name)
	require.Equal(t, "openai", candidates[1].Name)
	require.Equal(t, "gpt-4.1", models[0])
	require.Equal(t, "gpt-4.1-mini", models[1])
}

func TestBuildProviderCandidates_ModelRouteOverridesDefaultChain(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	captured := make([]string, 0, 4)
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		captured = append(captured, cfg.Provider+":"+cfg.Model)
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			return "ok", nil
		}}, nil
	}

	activities := NewRunActivities(&stubStore{}, llm.Config{}, nil, "http://example.com", "")
	candidates, err := activities.buildProviderCandidates(llm.Config{
		Provider:         "openai",
		Model:            "gpt-4.1",
		FallbackProvider: "openrouter",
		FallbackModel:    "meta-llama/3.1-8b",
		OpenAIAPIKey:     "openai-key",
		OpenCodeAPIKey:   "opencode-key",
	}, "opencode-zen:kimi-k2.5,openai:gpt-4o-mini")
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "opencode-zen", candidates[0].Name)
	require.Equal(t, "openai", candidates[1].Name)
	require.Equal(t, []string{"opencode-zen:kimi-k2.5", "openai:gpt-4o-mini"}, captured)
}

func TestParseModelRoute(t *testing.T) {
	entries := parseModelRoute("opencode-zen:kimi-k2.5; openai:gpt-4o-mini\nopenrouter")
	require.Len(t, entries, 3)
	require.Equal(t, "opencode-zen", entries[0].provider)
	require.Equal(t, "kimi-k2.5", entries[0].model)
	require.Equal(t, "openai", entries[1].provider)
	require.Equal(t, "gpt-4o-mini", entries[1].model)
	require.Equal(t, "openrouter", entries[2].provider)
	require.Equal(t, "", entries[2].model)
}

func TestGenerateAssistantReply_NewProviderError(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return nil, errors.New("provider init failed")
	}

	cpRequests := make(chan map[string]any, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		cpRequests <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
	}
	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", Model: "gpt", OpenAIAPIKey: "key"}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-5"})
	require.EqualError(t, err, "provider init failed")

	posted := waitForPayloadType(t, cpRequests, "run.failed")
	require.Equal(t, "run.failed", posted["type"])
}

func TestGenerateAssistantReply_GenerateError(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			return "", errors.New("generate failed")
		}}, nil
	}

	cpRequests := make(chan map[string]any, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		cpRequests <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
	}
	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", Model: "gpt", OpenAIAPIKey: "key"}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-6"})
	require.EqualError(t, err, "generate failed")

	posted := waitForPayloadType(t, cpRequests, "run.failed")
	require.Equal(t, "run.failed", posted["type"])
}

func TestGenerateAssistantReply_PostMessageError(t *testing.T) {
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Hi"}}]}`))
	}))
	defer llmServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Hello"}}, nil
		},
	}
	activities := NewRunActivities(
		storeStub,
		llm.Config{Provider: "openai", Model: "gpt", OpenAIAPIKey: "key", BaseURL: llmServer.URL},
		nil,
		"http://example.com",
		"",
	)
	activities.httpClient = &http.Client{Transport: errorRoundTripper{err: errors.New("post failed")}}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-7"})
	require.ErrorContains(t, err, "post failed")
}

func TestGenerateAssistantReply_EmptyMessages(t *testing.T) {
	originalProvider := newProvider
	originalBuildSystem := buildSystem
	originalBuildMemory := buildMemory
	defer func() {
		newProvider = originalProvider
		buildSystem = originalBuildSystem
		buildMemory = originalBuildMemory
	}()

	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			return "ok", nil
		}}, nil
	}
	buildSystem = func(a *RunActivities, ctx context.Context) string { return "" }
	buildMemory = func(a *RunActivities, ctx context.Context, messages []store.Message) string { return "" }

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: ""}}, nil
		},
	}
	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, "http://example.com", "")
	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-8"})
	require.NoError(t, err)
}

func TestResolveConfig(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	encrypted, err := secrets.Encrypt(key, "router-key")
	require.NoError(t, err)

	defaultCfg := llm.Config{
		Provider:     "openai",
		Model:        "base",
		OpenAIAPIKey: "openai-key",
	}

	openAIEncrypted, err := secrets.Encrypt(key, "openai-secret")
	require.NoError(t, err)

	t.Run("decrypt_and_override", func(t *testing.T) {
		storeStub := &stubStore{
			getLLMSettingsFunc: func(ctx context.Context) (*store.LLMSettings, error) {
				return &store.LLMSettings{
					Provider:  "openrouter",
					Model:     "router-model",
					APIKeyEnc: encrypted,
				}, nil
			},
		}
		activities := NewRunActivities(storeStub, defaultCfg, key, "http://example.com", "")
		messages := []store.Message{{Role: "user", Metadata: map[string]any{"llm_provider": "openai", "llm_model": "override"}}}

		cfg, err := activities.resolveConfig(context.Background(), messages)
		require.NoError(t, err)
		require.Equal(t, "openai", cfg.Provider)
		require.Equal(t, "override", cfg.Model)
		require.Equal(t, "openai-key", cfg.OpenAIAPIKey)
		require.Equal(t, "router-key", cfg.OpenRouterAPIKey)
	})

	t.Run("settings_error", func(t *testing.T) {
		storeStub := &stubStore{
			getLLMSettingsFunc: func(ctx context.Context) (*store.LLMSettings, error) {
				return nil, errors.New("settings failed")
			},
		}
		activities := NewRunActivities(storeStub, defaultCfg, key, "http://example.com", "")
		_, err := activities.resolveConfig(context.Background(), nil)
		require.EqualError(t, err, "settings failed")
	})

	t.Run("default_provider_key", func(t *testing.T) {
		storeStub := &stubStore{
			getLLMSettingsFunc: func(ctx context.Context) (*store.LLMSettings, error) {
				return &store.LLMSettings{Provider: "openai", APIKeyEnc: openAIEncrypted}, nil
			},
		}
		activities := NewRunActivities(storeStub, defaultCfg, key, "http://example.com", "")
		cfg, err := activities.resolveConfig(context.Background(), nil)
		require.NoError(t, err)
		require.Equal(t, "openai-secret", cfg.OpenAIAPIKey)
	})

	t.Run("missing_secret_key", func(t *testing.T) {
		storeStub := &stubStore{
			getLLMSettingsFunc: func(ctx context.Context) (*store.LLMSettings, error) {
				return &store.LLMSettings{Provider: "openai", APIKeyEnc: encrypted}, nil
			},
		}
		activities := NewRunActivities(storeStub, defaultCfg, nil, "http://example.com", "")
		_, err := activities.resolveConfig(context.Background(), nil)
		require.EqualError(t, err, "LLM_SECRETS_KEY is required to decrypt API keys")
	})

	t.Run("decrypt_error", func(t *testing.T) {
		storeStub := &stubStore{
			getLLMSettingsFunc: func(ctx context.Context) (*store.LLMSettings, error) {
				return &store.LLMSettings{Provider: "openai", APIKeyEnc: "invalid"}, nil
			},
		}
		activities := NewRunActivities(storeStub, defaultCfg, key, "http://example.com", "")
		_, err := activities.resolveConfig(context.Background(), nil)
		require.Error(t, err)
	})

	t.Run("missing_openai_key", func(t *testing.T) {
		storeStub := &stubStore{}
		cfg := llm.Config{Provider: "openai", Model: "gpt"}
		activities := NewRunActivities(storeStub, cfg, nil, "http://example.com", "")
		_, err := activities.resolveConfig(context.Background(), nil)
		require.EqualError(t, err, "missing API key for provider")
	})

	t.Run("missing_openrouter_key", func(t *testing.T) {
		storeStub := &stubStore{}
		cfg := llm.Config{Provider: "openrouter", Model: "gpt"}
		activities := NewRunActivities(storeStub, cfg, nil, "http://example.com", "")
		_, err := activities.resolveConfig(context.Background(), nil)
		require.EqualError(t, err, "missing API key for provider")
	})
}

func TestExtractOverrides(t *testing.T) {
	provider, model := extractOverrides([]store.Message{{Role: "assistant", Content: "skip"}})
	require.Empty(t, provider)
	require.Empty(t, model)

	provider, model = extractOverrides([]store.Message{
		{Role: "user", Metadata: map[string]any{"llm_provider": " openai ", "llm_model": " gpt-4 "}},
	})
	require.Equal(t, "openai", provider)
	require.Equal(t, "gpt-4", model)

	provider, model = extractOverrides([]store.Message{
		{Role: "user", Metadata: map[string]any{"llm_provider": 123}},
	})
	require.Empty(t, provider)
	require.Empty(t, model)
}

func TestResolveBrowserUserTabConfig(t *testing.T) {
	config := resolveBrowserUserTabConfig(nil)
	require.False(t, config.Enabled)
	require.False(t, config.InteractionAllowed)
	require.Nil(t, config.DomainAllowlist)

	config = resolveBrowserUserTabConfig([]store.Message{
		{Role: "assistant", Metadata: map[string]any{"browser_mode": "user_tab"}},
		{
			Role: "user",
			Metadata: map[string]any{
				"browser_mode":             "user_tab",
				"browser_interaction":      "enabled",
				"browser_domain_allowlist": "defillama.com, https://cointelegraph.com, cointelegraph.com",
			},
		},
	})
	require.True(t, config.Enabled)
	require.True(t, config.InteractionAllowed)
	require.ElementsMatch(t, []string{"defillama.com", "cointelegraph.com"}, config.DomainAllowlist)

	config = resolveBrowserUserTabConfig([]store.Message{
		{
			Role: "user",
			Metadata: map[string]any{
				"browser_mode": "playwright",
			},
		},
	})
	require.False(t, config.Enabled)
}

func TestBuildMemoryPrompt(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

	prompt := activities.buildMemoryPrompt(context.Background(), nil)
	require.Empty(t, prompt)

	storeStub.getMemorySettingsFunc = func(ctx context.Context) (*store.MemorySettings, error) {
		return &store.MemorySettings{Enabled: false}, nil
	}
	prompt = activities.buildMemoryPrompt(context.Background(), []store.Message{{Role: "user", Content: "Hi"}})
	require.Empty(t, prompt)

	storeStub.getMemorySettingsFunc = func(ctx context.Context) (*store.MemorySettings, error) {
		return &store.MemorySettings{Enabled: true}, nil
	}
	prompt = activities.buildMemoryPrompt(context.Background(), []store.Message{{Role: "user", Content: ""}})
	require.Empty(t, prompt)

	storeStub.searchMemoryFunc = func(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
		return nil, errors.New("search failed")
	}
	prompt = activities.buildMemoryPrompt(context.Background(), []store.Message{{Role: "user", Content: "Find"}})
	require.Empty(t, prompt)

	storeStub.searchMemoryFunc = func(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
		return []store.MemoryEntry{}, nil
	}
	prompt = activities.buildMemoryPrompt(context.Background(), []store.Message{{Role: "user", Content: "Find"}})
	require.Empty(t, prompt)

	storeStub.searchMemoryFunc = func(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
		return []store.MemoryEntry{{Content: "  entry one  "}, {Content: "entry two"}}, nil
	}
	prompt = activities.buildMemoryPrompt(context.Background(), []store.Message{{Role: "user", Content: "Find"}})
	require.Contains(t, prompt, "Relevant memory:")
	require.Contains(t, prompt, "entry one")
	require.Contains(t, prompt, "entry two")
}

func TestBuildMemoryPrompt_UsesLatestUserMessage(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

	storeStub.getMemorySettingsFunc = func(ctx context.Context) (*store.MemorySettings, error) {
		return &store.MemorySettings{Enabled: true}, nil
	}
	var gotQuery string
	storeStub.searchMemoryFunc = func(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
		gotQuery = query
		return []store.MemoryEntry{{Content: "entry"}}, nil
	}
	messages := []store.Message{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "second question"},
	}
	prompt := activities.buildMemoryPrompt(context.Background(), messages)
	require.Contains(t, prompt, "Relevant memory:")
	require.Equal(t, "second question", gotQuery)
}

func TestBuildMemoryPrompt_UsesEmbeddingWhenPresent(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

	storeStub.getMemorySettingsFunc = func(ctx context.Context) (*store.MemorySettings, error) {
		return &store.MemorySettings{Enabled: true}, nil
	}
	calledEmbedding := false
	storeStub.searchMemoryEmbeddingFunc = func(ctx context.Context, query string, embedding []float32, limit int) ([]store.MemoryEntry, error) {
		calledEmbedding = true
		require.Equal(t, "find embeddings", query)
		require.Equal(t, []float32{0.1, 0.2}, embedding)
		return []store.MemoryEntry{{Content: "entry"}}, nil
	}
	prompt := activities.buildMemoryPrompt(context.Background(), []store.Message{{
		Role:    "user",
		Content: "find embeddings",
		Metadata: map[string]any{
			"embedding": []any{0.1, 0.2},
		},
	}})
	require.True(t, calledEmbedding)
	require.Contains(t, prompt, "Relevant memory:")
}

func TestBuildSystemPrompt(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

	prompt := activities.buildSystemPrompt(context.Background())
	t.Logf("system prompt: %s", prompt)
	require.Contains(t, prompt, "You are Gavryn")
	require.Contains(t, prompt, "System resources:")
	require.Contains(t, prompt, "Memory: disabled")

	storeStub.getMemorySettingsFunc = func(ctx context.Context) (*store.MemorySettings, error) {
		return &store.MemorySettings{Enabled: true}, nil
	}
	prompt = activities.buildSystemPrompt(context.Background())
	require.Contains(t, prompt, "Memory: enabled")
}

func TestBuildSystemPrompt_UsesPersonalityFile(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

	customDir := t.TempDir()
	customFile := filepath.Join(customDir, personality.FileName)
	require.NoError(t, os.WriteFile(customFile, []byte("File personality override."), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(customDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	prompt := activities.buildSystemPrompt(context.Background())
	require.Contains(t, prompt, "File personality override.")
}

func TestBuildSystemPrompt_FallsBackToDefaultWhenFileMissing(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

	customDir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(customDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	prompt := activities.buildSystemPrompt(context.Background())
	require.Contains(t, prompt, personality.Default)
}

func TestRequiresAPIKey(t *testing.T) {
	require.True(t, requiresAPIKey("openai"))
	require.True(t, requiresAPIKey("openrouter"))
	require.True(t, requiresAPIKey("opencode-zen"))
	require.False(t, requiresAPIKey("local"))
}

func TestAllowedToolNames_IncludeBrowserAndDocumentTools(t *testing.T) {
	require.True(t, isToolAllowed("browser.navigate"))
	require.True(t, isToolAllowed("browser.snapshot"))
	require.False(t, isToolAllowed("browser.extract_text"))
	require.True(t, isToolAllowed("document.create_pdf"))
	require.True(t, isToolAllowed("process.exec"))
	require.False(t, isToolAllowed("browser.unknown"))
}

func TestGenerateAssistantReply_PartialSuccessFallbackIncludesWrites(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			return "```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"notes.txt\",\"content\":\"hello\"}}]}\n```", nil
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"path": "notes.txt"}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Write a file"}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-partial"})
	require.NoError(t, err)

	posted := <-cpMessages
	require.Contains(t, strings.ToLower(posted["content"]), "completed this run")
	require.Contains(t, posted["content"], "notes.txt")
	require.GreaterOrEqual(t, callCount, defaultMaxToolIterations)
}

func TestGenerateAssistantReply_EmptyModelResponseFallsBackGracefully(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			return "", errors.New("LLM response had no content")
		}}, nil
	}

	cpRequests := make(chan struct {
		path string
		body map[string]any
	}, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		if !strings.HasSuffix(r.URL.Path, "/messages") && !strings.HasSuffix(r.URL.Path, "/events") {
			w.WriteHeader(http.StatusOK)
			return
		}
		cpRequests <- struct {
			path string
			body map[string]any
		}{path: r.URL.Path, body: payload}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "hello"}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, "")
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-empty-content"})
	require.NoError(t, err)

	requestOne := waitForRequestPath(t, cpRequests, "/runs/run-empty-content/messages")
	require.Equal(t, "/runs/run-empty-content/messages", requestOne.path)
	content, _ := requestOne.body["content"].(string)
	require.Contains(t, content, "empty response")

	deadline := time.After(2 * time.Second)
	for {
		select {
		case extra := <-cpRequests:
			if extra.path != "/runs/run-empty-content/events" {
				continue
			}
			if extra.body["type"] == "run.partial" {
				return
			}
		case <-deadline:
			t.Fatal("expected run.partial event for empty model response fallback")
		}
	}
}

func TestGenerateAssistantReply_NoContentAfterToolsPostsCompletionSummary(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"site/index.ts\",\"content\":\"export default 1\"}}]}\n```", nil
			}
			return "", errors.New("LLM response had no content")
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{
			Status: "completed",
			Output: map[string]any{"path": "site/index.ts"},
		})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 32)
	cpEvents := make(chan map[string]any, 64)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			cpMessages <- payload
		case strings.HasSuffix(r.URL.Path, "/events"):
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Create files"}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-no-content-tools"})
	require.NoError(t, err)

	postedMessage := <-cpMessages
	content, _ := postedMessage["content"].(string)
	require.Contains(t, strings.ToLower(content), "completed this run")
	require.Contains(t, content, "site/index.ts")

	postedEvent := waitForPayloadType(t, cpEvents, "run.completed")
	eventPayload, _ := postedEvent["payload"].(map[string]any)
	require.Equal(t, "completed", eventPayload["status"])
	require.Equal(t, "llm_no_content_after_tools", eventPayload["completion_reason"])
}

func TestGenerateAssistantReply_TransientAfterToolsPostsCompletionSummary(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"process.start\",\"input\":{\"command\":\"npm\",\"args\":[\"run\",\"dev\"]}}]}\n```", nil
			}
			return "", errors.New("502 bad gateway")
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{
			Status: "completed",
			Output: map[string]any{
				"process_id":   "proc-1",
				"preview_urls": []string{"http://localhost:3000"},
			},
		})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 32)
	cpEvents := make(chan map[string]any, 64)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			cpMessages <- payload
		case strings.HasSuffix(r.URL.Path, "/events"):
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Start preview server"}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-transient-tools"})
	require.NoError(t, err)

	postedMessage := <-cpMessages
	content, _ := postedMessage["content"].(string)
	require.Contains(t, strings.ToLower(content), "completed this run")
	require.Contains(t, content, "localhost:3000")

	postedEvent := waitForPayloadType(t, cpEvents, "run.completed")
	eventPayload, _ := postedEvent["payload"].(map[string]any)
	require.Equal(t, "completed", eventPayload["status"])
	require.Equal(t, "llm_transient_after_tools", eventPayload["completion_reason"])
}

func TestGenerateAssistantReply_BlankModelContentAfterToolsPostsCompletionSummary(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"notes.txt\",\"content\":\"hello\"}}]}\n```", nil
			}
			return "   ", nil
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{
			Status: "completed",
			Output: map[string]any{"path": "notes.txt"},
		})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 32)
	cpEvents := make(chan map[string]any, 64)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			cpMessages <- payload
		case strings.HasSuffix(r.URL.Path, "/events"):
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Write notes to file"}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-blank-content-tools"})
	require.NoError(t, err)

	postedMessage := <-cpMessages
	content, _ := postedMessage["content"].(string)
	require.Contains(t, strings.ToLower(content), "completed this run")
	require.Contains(t, content, "notes.txt")

	postedEvent := waitForPayloadType(t, cpEvents, "run.completed")
	eventPayload, _ := postedEvent["payload"].(map[string]any)
	require.Equal(t, "completed", eventPayload["status"])
	require.Equal(t, "llm_no_content_after_tools", eventPayload["completion_reason"])
}

func TestGenerateAssistantReply_WebResearchDeterministicFallbackIncludesSources(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.navigate\",\"input\":{\"url\":\"https://example.com/defi-news\"}},{\"tool_name\":\"browser.extract\",\"input\":{\"mode\":\"metadata\"}},{\"tool_name\":\"browser.navigate\",\"input\":{\"url\":\"https://example.org/market-update\"}},{\"tool_name\":\"browser.extract\",\"input\":{\"mode\":\"metadata\"}}]}\n```", nil
			}
			return "", errors.New("502 bad gateway")
		}}, nil
	}

	extractCount := 0
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolName, _ := payload["tool_name"].(string)
		switch toolName {
		case "browser.navigate":
			input, _ := payload["input"].(map[string]any)
			urlValue, _ := input["url"].(string)
			title := "Source"
			if strings.Contains(urlValue, "example.com") {
				title = "DeFi TVL Jumps on L2 Rotation"
			}
			if strings.Contains(urlValue, "example.org") {
				title = "Stablecoin Liquidity Repriced"
			}
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{"url": urlValue, "title": title},
			})
		case "browser.extract":
			extractCount++
			if extractCount == 1 {
				_ = json.NewEncoder(w).Encode(toolRunnerResponse{
					Status: "completed",
					Output: map[string]any{
						"mode": "metadata",
						"extracted": map[string]any{
							"title":       "DeFi TVL Jumps on L2 Rotation",
							"url":         "https://example.com/defi-news",
							"description": "TVL climbed sharply after users rotated capital to lower-fee L2 ecosystems.",
						},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{
					"mode": "metadata",
					"extracted": map[string]any{
						"title":       "Stablecoin Liquidity Repriced",
						"url":         "https://example.org/market-update",
						"description": "Liquidity providers repriced stablecoin pairs as volatility increased in majors.",
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("unexpected tool"))
		}
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 32)
	cpEvents := make(chan map[string]any, 64)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			cpMessages <- payload
		case strings.HasSuffix(r.URL.Path, "/events"):
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Browse the web and give me the top 2 DeFi news items from February 2026 with source links and 1-line impact notes."}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-web-research-fallback"})
	require.NoError(t, err)

	postedMessage := <-cpMessages
	content, _ := postedMessage["content"].(string)
	require.Contains(t, content, "https://example.com/defi-news")
	require.NotContains(t, content, "https://example.org/market-update")
	require.NotContains(t, strings.ToLower(content), "could not produce a final assistant response")

	postedEvent := waitForPayloadTypes(t, cpEvents, "run.partial", "run.completed")
	eventPayload, _ := postedEvent["payload"].(map[string]any)
	status, _ := eventPayload["status"].(string)
	require.Contains(t, []string{"partial", "completed"}, status)
	if status == "partial" {
		require.Equal(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
	if status == "completed" {
		require.NotEqual(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
}

func TestGenerateAssistantReply_WebResearchTransientErrorInsufficientEvidenceMarksPartial(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.navigate\",\"input\":{\"url\":\"https://www.google.com/search?q=rwa+crypto+february+2026\"}}]}\n```", nil
			}
			return "", errors.New("502 bad gateway")
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolName, _ := payload["tool_name"].(string)
		input, _ := payload["input"].(map[string]any)
		switch toolName {
		case "browser.navigate":
			urlValue, _ := input["url"].(string)
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{
					"url":   urlValue,
					"title": "Search results",
				},
			})
		case "browser.evaluate":
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{"result": []any{}},
			})
		case "browser.scroll":
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{"scrolled": true},
			})
		case "browser.extract":
			mode, _ := input["mode"].(string)
			if strings.TrimSpace(mode) == "" {
				mode = "text"
			}
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{
					"mode": mode,
					"url":  "https://www.google.com/search?q=rwa+crypto+february+2026",
					"extracted": map[string]any{
						"title": "Google Search",
					},
					"diagnostics": map[string]any{
						"status":        "empty",
						"reason_code":   "no_extractable_content",
						"reason_detail": "search_results_page",
						"word_count":    0,
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("unexpected tool"))
		}
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 32)
	cpEvents := make(chan map[string]any, 64)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			cpMessages <- payload
		case strings.HasSuffix(r.URL.Path, "/events"):
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Browse the web and give me the top 4 RWA and crypto news items from February 2026 with source links and a comprehensive summary."}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-web-research-transient-insufficient"})
	require.NoError(t, err)

	postedMessage := <-cpMessages
	content, _ := postedMessage["content"].(string)
	require.Contains(t, strings.ToLower(content), "could not extract enough article-grade source")

	postedEvent := waitForPayloadTypes(t, cpEvents, "run.partial", "run.completed")
	eventPayload, _ := postedEvent["payload"].(map[string]any)
	status, _ := eventPayload["status"].(string)
	require.Contains(t, []string{"partial", "completed"}, status)
	if status == "partial" {
		require.Equal(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
	if status == "completed" {
		require.NotEqual(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
}

func TestGenerateAssistantReply_WebResearchIntentOnlyNarrativeDoesNotFinalizeAsSuccess(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.navigate\",\"input\":{\"url\":\"https://example.com/defi-news-1\"}},{\"tool_name\":\"browser.extract\",\"input\":{\"mode\":\"metadata\"}}]}\n```", nil
			}
			return "All three crypto news sites have protections. Let me try alternative sources next:\n- CoinMarketCap\n- DeFiLlama\n- Messari", nil
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolName, _ := payload["tool_name"].(string)
		switch toolName {
		case "browser.navigate":
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{
					"url":   "https://example.com/defi-news-1",
					"title": "DeFi L2 Liquidity Rebounds",
				},
			})
		case "browser.extract":
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{
					"mode": "metadata",
					"url":  "https://example.com/defi-news-1",
					"extracted": map[string]any{
						"title":       "DeFi L2 Liquidity Rebounds",
						"url":         "https://example.com/defi-news-1",
						"description": "Liquidity depth improved as L2 routing costs declined and stablecoin pairs tightened spreads.",
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("unexpected tool"))
		}
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 64)
	cpEvents := make(chan map[string]any, 128)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			cpMessages <- payload
		case strings.HasSuffix(r.URL.Path, "/events"):
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Browse the web and give me the top 4 DeFi news items from February 2026 with source links and impact notes."}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-web-research-intent-only"})
	require.NoError(t, err)

	postedMessage := <-cpMessages
	content, _ := postedMessage["content"].(string)
	require.NotContains(t, content, "Stopped before finalizing because additional browser tool calls were required")
	require.NotContains(t, content, "Coverage limitation: extracted")
	require.NotContains(t, content, "requested high-confidence sources")
	require.NotContains(t, content, "Let me try alternative sources next")

	postedEvent := waitForPayloadTypes(t, cpEvents, "run.partial", "run.completed")
	eventPayload, _ := postedEvent["payload"].(map[string]any)
	status, _ := eventPayload["status"].(string)
	require.Contains(t, []string{"partial", "completed"}, status)
	if status == "partial" {
		require.Equal(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
	if status == "completed" {
		require.NotEqual(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
}

func TestGenerateAssistantReply_WebResearchAutoDeepeningFromIndexPages(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.navigate\",\"input\":{\"url\":\"https://example.com/news\"}},{\"tool_name\":\"browser.extract\",\"input\":{\"mode\":\"text\"}}]}\n```", nil
			}
			return "I found an index page first. I'll keep looking for direct sources.", nil
		}}, nil
	}

	var currentURL string
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolName, _ := payload["tool_name"].(string)
		input, _ := payload["input"].(map[string]any)

		switch toolName {
		case "browser.navigate":
			currentURL = toString(input["url"])
			title := "Example site"
			switch currentURL {
			case "https://example.com/news":
				title = "Example News"
			case "https://example.com/news/2026/02/05/rwa-liquidity-shifts":
				title = "RWA Liquidity Shifts in February 2026"
			case "https://example.com/news/2026/02/08/defi-lending-risk-repricing":
				title = "DeFi Lending Risk Repricing"
			case "https://example.com/news/2026/02/12/stablecoin-flow-rotation":
				title = "Stablecoin Flow Rotation"
			}
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{"url": currentURL, "title": title},
			})
		case "browser.evaluate":
			script := toString(input["script"])
			if strings.Contains(script, "querySelectorAll('a[href]')") {
				_ = json.NewEncoder(w).Encode(toolRunnerResponse{
					Status: "completed",
					Output: map[string]any{
						"result": []any{
							map[string]any{
								"href": "https://example.com/news/2026/02/05/rwa-liquidity-shifts",
								"text": "RWA liquidity shifts as treasuries move on-chain",
							},
							map[string]any{
								"href": "https://example.com/news/2026/02/08/defi-lending-risk-repricing",
								"text": "DeFi lending risk repriced after collateral volatility",
							},
							map[string]any{
								"href": "https://example.com/news/2026/02/12/stablecoin-flow-rotation",
								"text": "Stablecoin flow rotation changed DEX liquidity depth",
							},
						},
					},
				})
				return
			}
			if strings.Contains(script, "rwa-liquidity-shifts") {
				currentURL = "https://example.com/news/2026/02/05/rwa-liquidity-shifts"
			} else if strings.Contains(script, "defi-lending-risk-repricing") {
				currentURL = "https://example.com/news/2026/02/08/defi-lending-risk-repricing"
			} else if strings.Contains(script, "stablecoin-flow-rotation") {
				currentURL = "https://example.com/news/2026/02/12/stablecoin-flow-rotation"
			}
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{
					"result": map[string]any{
						"clicked": true,
						"href":    currentURL,
					},
				},
			})
		case "browser.scroll":
			_ = json.NewEncoder(w).Encode(toolRunnerResponse{
				Status: "completed",
				Output: map[string]any{
					"scrolled": true,
					"url":      currentURL,
				},
			})
		case "browser.extract":
			mode := strings.ToLower(strings.TrimSpace(toString(input["mode"])))
			switch currentURL {
			case "https://example.com/news":
				_ = json.NewEncoder(w).Encode(toolRunnerResponse{
					Status: "completed",
					Output: map[string]any{
						"mode": mode,
						"url":  currentURL,
						"diagnostics": map[string]any{
							"status":              "empty",
							"reason_code":         "no_extractable_content",
							"reason_detail":       "section_index_page",
							"extractable_content": false,
							"word_count":          8,
						},
						"extracted": "Latest headlines",
					},
				})
			case "https://example.com/news/2026/02/05/rwa-liquidity-shifts":
				if mode == "metadata" {
					_ = json.NewEncoder(w).Encode(toolRunnerResponse{
						Status: "completed",
						Output: map[string]any{
							"mode": mode,
							"url":  currentURL,
							"extracted": map[string]any{
								"title":       "RWA Liquidity Shifts in February 2026",
								"url":         currentURL,
								"description": "Liquidity rotated toward tokenized treasury pools as yields tightened across DeFi venues.",
							},
							"diagnostics": map[string]any{
								"status":              "ok",
								"extractable_content": true,
								"word_count":          18,
							},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(toolRunnerResponse{
					Status: "completed",
					Output: map[string]any{
						"mode":      mode,
						"url":       currentURL,
						"extracted": "Tokenized treasury pools absorbed new capital while protocol incentives shifted toward conservative collateral pairs.",
						"diagnostics": map[string]any{
							"status":              "ok",
							"extractable_content": true,
							"word_count":          22,
						},
					},
				})
			case "https://example.com/news/2026/02/08/defi-lending-risk-repricing":
				if mode == "metadata" {
					_ = json.NewEncoder(w).Encode(toolRunnerResponse{
						Status: "completed",
						Output: map[string]any{
							"mode": mode,
							"url":  currentURL,
							"extracted": map[string]any{
								"title":       "DeFi Lending Risk Repricing",
								"url":         currentURL,
								"description": "Lending protocols adjusted risk curves and liquidation thresholds after volatility spikes in governance tokens.",
							},
							"diagnostics": map[string]any{
								"status":              "ok",
								"extractable_content": true,
								"word_count":          19,
							},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(toolRunnerResponse{
					Status: "completed",
					Output: map[string]any{
						"mode":      mode,
						"url":       currentURL,
						"extracted": "Risk committees raised collateral factors for volatile assets and shortened grace windows on cross-margin positions.",
						"diagnostics": map[string]any{
							"status":              "ok",
							"extractable_content": true,
							"word_count":          20,
						},
					},
				})
			case "https://example.com/news/2026/02/12/stablecoin-flow-rotation":
				if mode == "metadata" {
					_ = json.NewEncoder(w).Encode(toolRunnerResponse{
						Status: "completed",
						Output: map[string]any{
							"mode": mode,
							"url":  currentURL,
							"extracted": map[string]any{
								"title":       "Stablecoin Flow Rotation",
								"url":         currentURL,
								"description": "Cross-chain stablecoin flow rotated into Ethereum L2 pools, lifting on-chain settlement volumes.",
							},
							"diagnostics": map[string]any{
								"status":              "ok",
								"extractable_content": true,
								"word_count":          17,
							},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(toolRunnerResponse{
					Status: "completed",
					Output: map[string]any{
						"mode":      mode,
						"url":       currentURL,
						"extracted": "Stablecoin routing shifted toward lower-slippage venues, increasing settlement throughput in several RWA-linked pools.",
						"diagnostics": map[string]any{
							"status":              "ok",
							"extractable_content": true,
							"word_count":          18,
						},
					},
				})
			default:
				_ = json.NewEncoder(w).Encode(toolRunnerResponse{
					Status: "completed",
					Output: map[string]any{
						"mode": mode,
						"url":  currentURL,
						"diagnostics": map[string]any{
							"status":              "empty",
							"reason_code":         "no_extractable_content",
							"reason_detail":       "missing_summary_text",
							"extractable_content": false,
							"word_count":          0,
						},
						"extracted": "",
					},
				})
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("unexpected tool"))
		}
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 64)
	cpEvents := make(chan map[string]any, 128)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			cpMessages <- payload
		case strings.HasSuffix(r.URL.Path, "/events"):
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Browse the web and give me the top 3 RWA and DeFi news items from February 2026 with source links and impact notes."}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-web-research-auto-deepen"})
	require.NoError(t, err)

	postedMessage := <-cpMessages
	content, _ := postedMessage["content"].(string)
	require.Contains(t, content, "https://example.com/news/2026/02/05/rwa-liquidity-shifts")
	require.Contains(t, content, "https://example.com/news/2026/02/08/defi-lending-risk-repricing")
	require.NotContains(t, content, "Stopped before finalizing because additional browser tool calls were required")

	postedEvent := waitForPayloadTypes(t, cpEvents, "run.partial", "run.completed")
	eventPayload, _ := postedEvent["payload"].(map[string]any)
	status, _ := eventPayload["status"].(string)
	require.Contains(t, []string{"partial", "completed"}, status)
	if status == "partial" {
		require.Equal(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
	if status == "completed" {
		require.NotEqual(t, "insufficient_web_research_evidence", eventPayload["completion_reason"])
	}
}

func TestBuildDeterministicWebResearchSummary_ReportsBlockedSourcesWithoutPlaceholder(t *testing.T) {
	content := buildDeterministicWebResearchSummary(
		[]toolCall{
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://example.com/news/2026/02/05/rwa-liquidity-shifts",
					"title": "RWA liquidity shifts in February 2026",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":  "https://example.com/news/2026/02/05/rwa-liquidity-shifts",
					"mode": "metadata",
					"extracted": map[string]any{
						"title":       "RWA liquidity shifts in February 2026",
						"url":         "https://example.com/news/2026/02/05/rwa-liquidity-shifts",
						"description": "RWA liquidity and DeFi lending activity accelerated in February 2026 as treasury tokenization volumes rose.",
					},
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          22,
					},
				},
			},
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://blocked.example.com",
					"title": "Blocked source",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":  "https://blocked.example.com",
					"mode": "text",
					"diagnostics": map[string]any{
						"status":        "blocked",
						"reason_code":   "blocked_by_bot_protection",
						"reason_detail": "Detected anti-bot challenge content.",
						"word_count":    3,
					},
					"extracted": "Just a moment... verify you are human",
				},
			},
		},
		webResearchRequirements{Enabled: true, MinimumItems: 1},
	)

	require.Contains(t, content, "https://example.com/news/2026/02/05/rwa-liquidity-shifts")
	require.NotContains(t, content, "blocked_by_bot_protection")
	require.NotContains(t, content, "Blocked sources:")
	require.NotContains(t, strings.ToLower(content), "impact note unavailable")
}

func TestBuildDeterministicWebResearchSummary_DownranksLandingAndSearchPages(t *testing.T) {
	content := buildDeterministicWebResearchSummary(
		[]toolCall{
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://coindesk.com",
					"title": "CoinDesk: Bitcoin, Ethereum, XRP, Crypto News and Price Data",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":  "https://coindesk.com",
					"mode": "text",
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          220,
					},
					"extracted": "News Video Prices Data & Indices Sponsored BTC $70,000 ETH $2,000 SOL $90",
				},
			},
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://www.google.com/search?q=defi+news+feb+2026",
					"title": "Google Search",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":  "https://www.google.com/search?q=defi+news+feb+2026",
					"mode": "text",
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          90,
					},
					"extracted": "Search results for DeFi news February 2026",
				},
			},
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://example.com/defi/l2-liquidity-rebound",
					"title": "Liquidity Rebound in L2 DeFi",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":  "https://example.com/defi/l2-liquidity-rebound",
					"mode": "metadata",
					"extracted": map[string]any{
						"title":       "Liquidity Rebound in L2 DeFi",
						"url":         "https://example.com/defi/l2-liquidity-rebound",
						"description": "Liquidity depth improved as stablecoin routing costs fell across major L2 pools.",
					},
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          18,
					},
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":       "https://example.com/defi/l2-liquidity-rebound",
					"mode":      "text",
					"extracted": "Liquidity depth improved across major DeFi pools as routing costs declined and stablecoin transfers accelerated. Market makers reported tighter spreads and stronger order-book resilience throughout February 2026, supporting broader risk appetite in lending and derivatives venues.",
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          120,
					},
				},
			},
		},
		webResearchRequirements{Enabled: true, MinimumItems: 1},
	)

	require.Contains(t, content, "Top stories:")
	require.Contains(t, content, "https://example.com/defi/l2-liquidity-rebound")
	require.NotContains(t, content, "Low-quality extracts:")
	require.NotContains(t, content, "homepage_not_article")
	require.NotContains(t, content, "search_results_page")
	require.NotContains(t, strings.ToLower(content), "impact note unavailable")
}

func TestBuildDeterministicWebResearchSummary_ClassifiesChallengePagesAsBlocked(t *testing.T) {
	content := buildDeterministicWebResearchSummary(
		[]toolCall{
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://www.bloomberg.com/search?query=real+world+assets+crypto+tokenization+2026",
					"title": "Bloomberg - Are you a robot?",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":  "https://www.bloomberg.com/search?query=real+world+assets+crypto+tokenization+2026",
					"mode": "metadata",
					"extracted": map[string]any{
						"title":       "Bloomberg - Are you a robot?",
						"url":         "https://www.bloomberg.com/search?query=real+world+assets+crypto+tokenization+2026",
						"description": "We've detected unusual activity from your computer network. Please click the box below to let us know you're not a robot.",
					},
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          32,
					},
				},
			},
		},
		webResearchRequirements{Enabled: true, MinimumItems: 1},
	)

	require.NotContains(t, content, "Blocked sources:")
	require.NotContains(t, content, "blocked_by_bot_protection")
	require.NotContains(t, content, "challenge_page_detected")
	require.NotContains(t, content, "Usable sources:\n1. [Bloomberg - Are you a robot?]")
}

func TestBuildDeterministicWebResearchSummary_DowngradesNotFoundPages(t *testing.T) {
	content := buildDeterministicWebResearchSummaryForRequest(
		[]toolCall{
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://cointelegraph.com/news/rwas-gatekeepers",
					"title": "Page Not Found | 404 | Cointelegraph",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":       "https://cointelegraph.com/news/rwas-gatekeepers",
					"mode":      "text",
					"extracted": "Could've sworn the page was around here somewhere.",
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          16,
					},
				},
			},
		},
		webResearchRequirements{Enabled: true, MinimumItems: 1},
		"Browse the web and give me RWA stories from February 2026",
	)

	require.NotContains(t, content, "Low-quality extracts:")
	require.NotContains(t, content, "not_found_page")
	require.NotContains(t, content, "Top stories:\n1. **Page Not Found | 404 | Cointelegraph**")
}

func TestCollectWebResearchEvidence_BuildsImpactFromBodyWhenMetadataDescriptionMissing(t *testing.T) {
	evidence := collectWebResearchEvidence([]toolCall{
		{
			ToolName: "browser.navigate",
			Input: map[string]any{
				"url":   "https://example.com/news/2026/02/12/stablecoin-flow-rotation",
				"title": "Stablecoin Flow Rotation",
			},
		},
		{
			ToolName: "browser.extract",
			Input: map[string]any{
				"url":  "https://example.com/news/2026/02/12/stablecoin-flow-rotation",
				"mode": "metadata",
				"extracted": map[string]any{
					"title":           "Stablecoin Flow Rotation",
					"url":             "https://example.com/news/2026/02/12/stablecoin-flow-rotation",
					"description":     "",
					"first_paragraph": "Cross-chain stablecoin flows rotated into Ethereum L2 pools and lifted settlement throughput across major venues.",
				},
				"diagnostics": map[string]any{
					"status":              "ok",
					"extractable_content": true,
					"word_count":          19,
				},
			},
		},
	})

	require.Len(t, evidence, 1)
	require.Equal(t, "https://example.com/news/2026/02/12/stablecoin-flow-rotation", evidence[0].URL)
	require.NotEmpty(t, evidence[0].Impact)
	require.Contains(t, strings.ToLower(evidence[0].Impact), "stablecoin")
}

func TestCollectWebResearchEvidence_DowngradesOpinionPages(t *testing.T) {
	evidence := collectWebResearchEvidence([]toolCall{
		{
			ToolName: "browser.navigate",
			Input: map[string]any{
				"url":   "https://cointelegraph.com/opinion/rwas-gatekeepers",
				"title": "Real-World Assets Don’t Need New Gatekeepers",
			},
		},
		{
			ToolName: "browser.extract",
			Input: map[string]any{
				"url":       "https://cointelegraph.com/opinion/rwas-gatekeepers",
				"mode":      "text",
				"extracted": "Institutions tokenize RWAs on closed infrastructure, reintroducing centralized control.",
				"diagnostics": map[string]any{
					"status":              "ok",
					"extractable_content": true,
					"word_count":          64,
				},
			},
		},
	})

	require.Len(t, evidence, 1)
	require.Equal(t, "no_extractable_content", evidence[0].ReasonCode)
	require.Equal(t, "opinion_page", evidence[0].ReasonDetail)
}

func TestSummarizeImpactText_PrefersSentenceOverRawPageBlob(t *testing.T) {
	raw := "Navigation Markets Prices. Cross-chain stablecoin flows rotated into Ethereum L2 pools and lifted settlement throughput across major venues. Sponsored BTC $70,000 ETH $2,000."
	summary := summarizeImpactText(raw)
	require.NotEmpty(t, summary)
	require.Contains(t, strings.ToLower(summary), "stablecoin")
	require.NotContains(t, strings.ToLower(summary), "markets prices")
}

func TestNonArticleReasonForURL_DoesNotMisclassifyNewsSlugArticles(t *testing.T) {
	require.Equal(t, "", nonArticleReasonForURL("https://cointelegraph.com/news/crypto-vc-funding-doubled-in-2025-as-rwa-tokenization-took-the-lead"))
	require.Equal(t, "section_index_page", nonArticleReasonForURL("https://cointelegraph.com/tags/rwa"))
	require.Equal(t, "legal_or_policy_page", nonArticleReasonForURL("https://cointelegraph.com/terms-and-privacy"))
	require.Equal(t, "legal_or_policy_page", nonArticleReasonForURL("https://www.coindesk.com/privacy"))
	require.Equal(t, "section_index_page", nonArticleReasonForURL("https://www.coindesk.com/author/jane-doe"))
	require.Equal(t, "section_index_page", nonArticleReasonForURL("https://duckduckgo.com/duckduckgo-help-pages/search-privacy/"))
	require.Equal(t, "section_index_page", nonArticleReasonForURL("https://cointelegraph.com/press-releases/example-rwa-launch"))
}

func TestCollectWebResearchEvidence_DowngradesLegalPolicyPages(t *testing.T) {
	evidence := collectWebResearchEvidence([]toolCall{
		{
			ToolName: "browser.navigate",
			Input: map[string]any{
				"url":   "https://www.coindesk.com/privacy",
				"title": "Privacy",
			},
		},
		{
			ToolName: "browser.extract",
			Input: map[string]any{
				"url":  "https://www.coindesk.com/privacy",
				"mode": "metadata",
				"extracted": map[string]any{
					"title":       "Privacy",
					"url":         "https://www.coindesk.com/privacy",
					"description": "Terms of service and privacy policy",
				},
				"diagnostics": map[string]any{
					"status":              "ok",
					"extractable_content": true,
					"word_count":          120,
				},
			},
		},
	})

	require.Len(t, evidence, 1)
	require.Equal(t, "no_extractable_content", evidence[0].ReasonCode)
	require.Equal(t, "legal_or_policy_page", evidence[0].ReasonDetail)
	require.False(t, evidence[0].extractable())
}

func TestSynthesizeImpactFromEvidence_CombinesNonMetadataSignals(t *testing.T) {
	evidence := []string{
		"Crypto News and Price Indexes",
		"Cross-chain stablecoin flows rotated into Ethereum L2 pools and lifted settlement throughput across major venues.",
		"Lending protocols repriced collateral factors after a volatility spike in governance-token markets.",
	}
	summary := synthesizeImpactFromEvidence("RWA and DeFi Market Update", evidence)
	require.NotEmpty(t, summary)
	lower := strings.ToLower(summary)
	require.Contains(t, lower, "stablecoin")
	require.Contains(t, lower, "repriced")
	require.NotContains(t, lower, "price indexes")
}

func TestGenerateAssistantReply_ToolFailureIncludesInvocationAndReason(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			callCount++
			if callCount == 1 {
				return "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.extract\",\"input\":{\"mode\":\"text\"}}]}\n```", nil
			}
			return "Done", nil
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"Extraction blocked","reason_code":"blocked_by_bot_protection"}`))
	}))
	defer toolServer.Close()

	cpEvents := make(chan map[string]any, 64)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		if strings.HasSuffix(r.URL.Path, "/events") {
			cpEvents <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
		return []store.Message{{Role: "user", Content: "Research the latest DeFi headlines"}}, nil
	}}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = cpServer.Client()

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-tool-failure-payload"})
	require.NoError(t, err)

	postedFailure := waitForPayloadType(t, cpEvents, "tool.failed")
	payload, _ := postedFailure["payload"].(map[string]any)
	require.NotEmpty(t, strings.TrimSpace(toString(payload["tool_invocation_id"])))
	require.Equal(t, "blocked_by_bot_protection", toString(payload["reason_code"]))
}

func TestParseToolRunnerErrorMessage_ParsesNestedBrowserWorkerJSON(t *testing.T) {
	body := []byte(`{"status":"failed","error":"{\"status\":\"failed\",\"error\":\"User tab mode could not connect to http://127.0.0.1:9222\",\"reason_code\":\"user_tab_mode_unavailable\"}"}`)

	message := parseToolRunnerErrorMessage(http.StatusBadGateway, body)

	require.Contains(t, message, "User tab mode could not connect to http://127.0.0.1:9222")
	require.Contains(t, message, "user_tab_mode_unavailable")
	require.NotContains(t, message, "{\"status\"")
}

func TestBuildToolExecutionFallback_UsesCompletionSummary(t *testing.T) {
	content := buildToolExecutionFallback([]toolCall{
		{
			ToolName: "process.exec",
			Input: map[string]any{
				"command": "npm",
				"args":    []string{"run", "dev"},
			},
		},
	})

	require.Contains(t, strings.ToLower(content), "completed this run")
	require.NotContains(t, strings.ToLower(content), "could not produce a final assistant response")
}

func TestMaybeGenerateRunTitle_AppendsEvent(t *testing.T) {
	storeStub := &stubStore{}
	var capturedEvent store.RunEvent
	storeStub.listEventsFunc = func(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
		return nil, nil
	}
	storeStub.nextSeqFunc = func(ctx context.Context, runID string) (int64, error) {
		return 5, nil
	}
	storeStub.appendEventFunc = func(ctx context.Context, event store.RunEvent) error {
		capturedEvent = event
		return nil
	}

	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")
	provider := stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
		require.Len(t, messages, 2)
		return "Landing Page Fixes", nil
	}}

	activities.maybeGenerateRunTitle(context.Background(), "run-1", provider, []store.Message{{Role: "user", Content: "Fix my landing page"}}, "Done")

	require.Equal(t, "run-1", capturedEvent.RunID)
	require.Equal(t, int64(5), capturedEvent.Seq)
	require.Equal(t, "run.title.updated", capturedEvent.Type)
	require.Equal(t, "Landing Page Fixes", capturedEvent.Payload["title"])
}

func TestParseToolCalls_InlineFenceAndBarePayload(t *testing.T) {
	inline := "```tool {\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"notes.txt\",\"content\":\"hello\"}}]} ```"
	inlineCalls, inlineStatus := parseToolCalls(inline)
	require.True(t, inlineStatus.sawToolBlock)
	require.Len(t, inlineCalls, 1)
	require.Equal(t, "editor.write", inlineCalls[0].ToolName)
	require.Equal(t, "notes.txt", inlineCalls[0].Input["path"])

	bare := "{\"tool_calls\":[{\"tool_name\":\"browser.navigate\",\"input\":{\"url\":\"https://example.com\"}}]}"
	bareCalls, bareStatus := parseToolCalls(bare)
	require.True(t, bareStatus.sawToolBlock)
	require.Len(t, bareCalls, 1)
	require.Equal(t, "browser.navigate", bareCalls[0].ToolName)
	require.Equal(t, "https://example.com", bareCalls[0].Input["url"])
}

func TestParseToolCalls_OpenAIStyleFunctionPayload(t *testing.T) {
	fenced := "```tool\n{\"tool_calls\":[{\"function\":{\"name\":\"editor.write\",\"arguments\":\"{\\\"path\\\":\\\"notes.txt\\\",\\\"content\\\":\\\"hello\\\"}\"}}]}\n```"
	fencedCalls, fencedStatus := parseToolCalls(fenced)
	require.True(t, fencedStatus.sawToolBlock)
	require.Len(t, fencedCalls, 1)
	require.Equal(t, "editor.write", fencedCalls[0].ToolName)
	require.Equal(t, "notes.txt", fencedCalls[0].Input["path"])
	require.Equal(t, "hello", fencedCalls[0].Input["content"])

	single := "```json\n{\"function\":{\"name\":\"browser.navigate\",\"arguments\":\"{\\\"url\\\":\\\"https://example.com\\\"}\"}}\n```"
	singleCalls, singleStatus := parseToolCalls(single)
	require.True(t, singleStatus.sawToolBlock)
	require.Len(t, singleCalls, 1)
	require.Equal(t, "browser.navigate", singleCalls[0].ToolName)
	require.Equal(t, "https://example.com", singleCalls[0].Input["url"])
}

func TestParseToolCalls_CanonicalizesBrowserAliases(t *testing.T) {
	searchAlias := "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.search\",\"input\":{\"query\":\"top DeFi news February 2026\"}}]}\n```"
	searchCalls, searchStatus := parseToolCalls(searchAlias)
	require.True(t, searchStatus.sawToolBlock)
	require.Len(t, searchCalls, 1)
	require.Equal(t, "browser.navigate", searchCalls[0].ToolName)
	require.Equal(t, "https://duckduckgo.com/?q=top+DeFi+news+February+2026", searchCalls[0].Input["url"])

	browseAlias := "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.browse\",\"input\":{\"target\":\"https://defillama.com\"}}]}\n```"
	browseCalls, browseStatus := parseToolCalls(browseAlias)
	require.True(t, browseStatus.sawToolBlock)
	require.Len(t, browseCalls, 1)
	require.Equal(t, "browser.navigate", browseCalls[0].ToolName)
	require.Equal(t, "https://defillama.com", browseCalls[0].Input["url"])

	extractAlias := "```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.extract_text\",\"input\":{\"selector\":\"body\"}}]}\n```"
	extractCalls, extractStatus := parseToolCalls(extractAlias)
	require.True(t, extractStatus.sawToolBlock)
	require.Len(t, extractCalls, 1)
	require.Equal(t, "browser.extract", extractCalls[0].ToolName)
	require.Equal(t, "text", extractCalls[0].Input["mode"])
}

func TestGenerateAssistantReply_CanonicalizesBrowserAliasToolNames(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	responses := []string{
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"browser.search\",\"input\":{\"query\":\"top defi news february 2026\"}}]}\n```",
		"Compiled the top DeFi headlines with links.",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return "", errors.New("too many calls")
			}
			value := responses[callCount]
			callCount++
			return value, nil
		}}, nil
	}

	toolCalls := make(chan map[string]any, 32)
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(body, &payload)
		toolCalls <- payload
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"url": "https://duckduckgo.com"}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]string, 32)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Find DeFi headlines"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-browser-alias"})
	require.NoError(t, err)

	executed := <-toolCalls
	require.Equal(t, "browser.navigate", executed["tool_name"])
	input, ok := executed["input"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, input["url"], "duckduckgo.com/?q=top+defi+news+february+2026")

	posted := <-cpMessages
	require.Equal(t, "Compiled the top DeFi headlines with links.", posted["content"])
}

func TestGenerateAssistantReply_MaxIterationsTriggersFinalSynthesis(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	responses := []string{
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"a.txt\",\"content\":\"a\"}}]}\n```",
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"b.txt\",\"content\":\"b\"}}]}\n```",
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"c.txt\",\"content\":\"c\"}}]}\n```",
		"```tool\n{\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{\"path\":\"d.txt\",\"content\":\"d\"}}]}\n```",
		"Final synthesis response",
	}
	callCount := 0
	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			if callCount >= len(responses) {
				return "", errors.New("too many calls")
			}
			value := responses[callCount]
			callCount++
			return value, nil
		}}, nil
	}

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{Status: "completed", Output: map[string]any{"ok": true}})
	}))
	defer toolServer.Close()

	cpMessages := make(chan map[string]any, 16)
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			payload := map[string]any{}
			_ = json.Unmarshal(body, &payload)
			cpMessages <- payload
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Create some files"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-synthesis"})
	require.NoError(t, err)

	posted := <-cpMessages
	content, _ := posted["content"].(string)
	require.Equal(t, "Final synthesis response", content)
}

func TestGenerateAssistantReply_PostCompletionCleansUpRunResources(t *testing.T) {
	originalProvider := newProvider
	defer func() { newProvider = originalProvider }()

	newProvider = func(cfg llm.Config) (llm.Provider, error) {
		return stubProvider{generate: func(ctx context.Context, messages []llm.Message) (string, error) {
			return "Done", nil
		}}, nil
	}

	cleanupCalled := make(chan string, 4)
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/processes/cleanup") {
			cleanupCalled <- r.URL.Path
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"completed","output":{"stopped":0},"artifacts":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer toolServer.Close()

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer cpServer.Close()

	storeStub := &stubStore{
		listMessagesFunc: func(ctx context.Context, runID string) ([]store.Message, error) {
			return []store.Message{{Role: "user", Content: "Say done"}}, nil
		},
	}

	activities := NewRunActivities(storeStub, llm.Config{Provider: "openai", OpenAIAPIKey: "key"}, nil, cpServer.URL, toolServer.URL)
	activities.httpClient = &http.Client{Timeout: time.Second}

	err := activities.GenerateAssistantReply(context.Background(), GenerateInput{RunID: "run-cleanup"})
	require.NoError(t, err)

	select {
	case path := <-cleanupCalled:
		require.Contains(t, path, "/runs/run-cleanup/processes/cleanup")
	case <-time.After(2 * time.Second):
		t.Fatal("expected run cleanup endpoint to be called")
	}
}

func TestIsRetryableLLMError_StatusPattern(t *testing.T) {
	require.True(t, isRetryableLLMError(errors.New("provider error code:500 upstream")))
	require.True(t, isRetryableLLMError(errors.New("http 503 service unavailable")))
	require.False(t, isRetryableLLMError(errors.New("validation failed: unsupported model")))
}

func TestClampConversationWindow_PreservesSystemAndRecent(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system-1"},
		{Role: "system", Content: "system-2"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}

	clamped := clampConversationWindow(messages, 2, 1000)
	require.Len(t, clamped, 4)
	require.Equal(t, "system", clamped[0].Role)
	require.Equal(t, "system", clamped[1].Role)
	require.Equal(t, "u2", clamped[2].Content)
	require.Equal(t, "a2", clamped[3].Content)
}

func TestPostMessage(t *testing.T) {
	storeStub := &stubStore{}
	activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/runs/run-1/messages", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		activities.controlPlane = server.URL
		activities.httpClient = server.Client()
		err := activities.postMessage(context.Background(), "run-1", "hi")
		require.NoError(t, err)
	})

	t.Run("sanitizes_research_noise", func(t *testing.T) {
		var payload map[string]string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/runs/run-1/messages", r.URL.Path)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		activities.controlPlane = server.URL
		activities.httpClient = server.Client()
		content := strings.Join([]string{
			"Top stories:",
			"1. **Story A**",
			"   - Source: [example.com](https://example.com/a)",
			"",
			"Low-quality extracts:",
			"1. [Search](https://example.com/search) — search_results_page",
		}, "\n")
		err := activities.postMessage(context.Background(), "run-1", content)
		require.NoError(t, err)
		require.Equal(t, "assistant", payload["role"])
		require.Contains(t, payload["content"], "Top stories:")
		require.NotContains(t, strings.ToLower(payload["content"]), "low-quality extracts:")
	})

	t.Run("status_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		activities.controlPlane = server.URL
		activities.httpClient = server.Client()
		err := activities.postMessage(context.Background(), "run-1", "hi")
		require.EqualError(t, err, "control plane message failed: 400 Bad Request")
	})

	t.Run("request_error", func(t *testing.T) {
		activities.controlPlane = "http://[::1]:namedport"
		err := activities.postMessage(context.Background(), "run-1", "hi")
		require.Error(t, err)
	})

	t.Run("transport_error", func(t *testing.T) {
		activities.controlPlane = "http://example.com"
		activities.httpClient = &http.Client{Transport: errorRoundTripper{err: errors.New("network")}}
		err := activities.postMessage(context.Background(), "run-1", "hi")
		require.ErrorContains(t, err, "network")
	})

	t.Run("marshal_error", func(t *testing.T) {
		originalMarshal := marshalJSON
		marshalJSON = func(v any) ([]byte, error) {
			return nil, errors.New("marshal")
		}
		defer func() { marshalJSON = originalMarshal }()

		activities.controlPlane = "http://example.com"
		err := activities.postMessage(context.Background(), "run-1", "hi")
		require.EqualError(t, err, "marshal")
	})
}

func TestExecuteToolCall_UserTabGuardrails(t *testing.T) {
	var capturedBody map[string]any
	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/tools/execute", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		_ = json.NewEncoder(w).Encode(toolRunnerResponse{
			Status: "completed",
			Output: map[string]any{"ok": true},
		})
	}))
	defer toolServer.Close()

	activities := NewRunActivities(&stubStore{}, llm.Config{}, nil, "http://example.com", toolServer.URL)
	activities.httpClient = toolServer.Client()

	output, err := activities.executeToolCall(
		context.Background(),
		"run-1",
		toolCall{
			ToolName: "browser.navigate",
			Input:    map[string]any{"url": "https://example.com"},
		},
		browserUserTabConfig{
			Enabled:            true,
			InteractionAllowed: true,
			DomainAllowlist:    []string{"example.com"},
			PreferredBrowser:   "brave",
			BrowserUserAgent:   "Mozilla/5.0 ... Brave/1.73.0",
		},
	)
	require.NoError(t, err)
	require.Equal(t, true, output["ok"])

	inputPayload, ok := capturedBody["input"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user_tab", inputPayload["_browser_mode"])

	guardrails, ok := inputPayload["_browser_guardrails"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, guardrails["interaction_allowed"])
	require.Equal(t, true, guardrails["create_tab_group"])
	require.Equal(t, []any{"example.com"}, guardrails["allowlist_domains"])
	require.Equal(t, "brave", guardrails["preferred_browser"])
	require.Equal(t, "Mozilla/5.0 ... Brave/1.73.0", guardrails["browser_user_agent"])
}

func TestResolveBrowserUserTabConfig_IncludesPreferredBrowserFromUserAgent(t *testing.T) {
	config := resolveBrowserUserTabConfig([]store.Message{
		{
			Role: "user",
			Metadata: map[string]any{
				"browser_mode":        "user_tab",
				"browser_interaction": "enabled",
				"browser_user_agent":  "Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Brave/1.73.100 Chrome/131.0.0.0 Safari/537.36",
			},
		},
	})

	require.True(t, config.Enabled)
	require.True(t, config.InteractionAllowed)
	require.Equal(t, "brave", config.PreferredBrowser)
	require.Contains(t, config.BrowserUserAgent, "Brave")
}

func TestFallbackResearchSeedURLsFromRequest(t *testing.T) {
	seeds := fallbackResearchSeedURLsFromRequest("Browse the web and give me the top 8 DeFi news items from February 2026 with sources")
	require.NotEmpty(t, seeds)
	require.LessOrEqual(t, len(seeds), maxAutoResearchSeedPages)
	joined := strings.Join(seeds, "\n")
	require.Contains(t, joined, "reuters.com/site-search")
	require.Contains(t, joined, "forbes.com/search")
	require.NotContains(t, joined, "coindesk.com/search")
}

func TestMergeResearchSeeds_PrioritizesPrimaryGroup(t *testing.T) {
	merged := mergeResearchSeeds(4,
		[]string{
			"https://www.reuters.com/site-search/?query=rwa",
			"https://www.coindesk.com/search?s=rwa",
		},
		[]string{
			"https://www.coindesk.com/search?s=rwa",
			"https://cointelegraph.com/search?query=rwa",
			"https://duckduckgo.com/?q=rwa",
		},
		1,
	)

	require.Len(t, merged, 4)
	require.Equal(t, "https://www.reuters.com/site-search/?query=rwa", merged[0])
	require.Equal(t, "https://www.coindesk.com/search?s=rwa", merged[1])
	require.Equal(t, "https://cointelegraph.com/search?query=rwa", merged[2])
	require.Equal(t, "https://duckduckgo.com/?q=rwa", merged[3])
}

func TestRankArticleLinkCandidates_AllowsCrossDomainFromSearchSeed(t *testing.T) {
	seedURL := "https://news.google.com/search?q=rwa+crypto+february+2026"
	candidates := []researchLinkCandidate{
		{
			URL:        "https://www.google.com/url?q=https%3A%2F%2Fwww.reuters.com%2Fbusiness%2Ffinance%2Ftokenized-rwa-markets-expand-2026-02-08%2F",
			AnchorText: "RWA tokenization flows accelerate in February",
		},
		{
			URL:        "https://policies.google.com/privacy",
			AnchorText: "Privacy Policy",
		},
	}

	ranked := rankArticleLinkCandidates(seedURL, candidates, map[string]struct{}{}, nil)
	require.NotEmpty(t, ranked)
	require.Equal(t, "https://www.reuters.com/business/finance/tokenized-rwa-markets-expand-2026-02-08/", ranked[0].URL)
}

func TestRankArticleLinkCandidates_DoesNotDropStrongArticleLinksWithoutKeywordMatch(t *testing.T) {
	seedURL := "https://www.coindesk.com/tag/real-world-assets/"
	candidates := []researchLinkCandidate{
		{
			URL:        "https://www.coindesk.com/markets/2026/02/06/china-offshore-issuance-framework-advances-tokenization/",
			AnchorText: "China offshore issuance framework advances tokenization",
		},
		{
			URL:        "https://www.coindesk.com/privacy",
			AnchorText: "Privacy Policy",
		},
	}

	ranked := rankArticleLinkCandidates(seedURL, candidates, map[string]struct{}{}, []string{"rwa", "crypto", "february", "2026"})
	require.NotEmpty(t, ranked)
	require.Equal(t, "https://www.coindesk.com/markets/2026/02/06/china-offshore-issuance-framework-advances-tokenization/", ranked[0].URL)
}

func TestRankArticleLinkCandidates_FiltersUtilityCrossDomainLinksForTopicalRequests(t *testing.T) {
	seedURL := "https://news.google.com/search?q=rwa+crypto+february+2026"
	candidates := []researchLinkCandidate{
		{
			URL:        "https://duckduckgo.com/duckduckgo-help-pages/search-privacy/",
			AnchorText: "DuckDuckGo Search Privacy",
		},
		{
			URL:        "https://cointelegraph.com/news/rwa-tokenization-accelerates-in-february-2026",
			AnchorText: "RWA tokenization accelerates in February 2026",
		},
	}

	ranked := rankArticleLinkCandidates(seedURL, candidates, map[string]struct{}{}, []string{"rwa", "crypto", "february", "2026"})
	require.Len(t, ranked, 1)
	require.Equal(t, "https://cointelegraph.com/news/rwa-tokenization-accelerates-in-february-2026", ranked[0].URL)
}

func TestRankArticleLinkCandidates_ExcludesSponsoredPages(t *testing.T) {
	seedURL := "https://cointelegraph.com/tags/rwa"
	candidates := []researchLinkCandidate{
		{
			URL:        "https://cointelegraph.com/sponsored/regulated-exchange-behind-9-of-europe-s-mica-wp-filings-is-building-a-compliance-native-l2",
			AnchorText: "Sponsored: Regulated exchange behind 9% of MiCA filings",
		},
		{
			URL:        "https://cointelegraph.com/news/multiliquid-metalayer-instant-redemption-backstop-rwas-solana",
			AnchorText: "Multiliquid launches metalayer for RWA redemption",
		},
	}

	ranked := rankArticleLinkCandidates(seedURL, candidates, map[string]struct{}{}, []string{"rwa", "crypto", "february", "2026"})
	require.Len(t, ranked, 1)
	require.Equal(t, "https://cointelegraph.com/news/multiliquid-metalayer-instant-redemption-backstop-rwas-solana", ranked[0].URL)
}

func TestEvidenceMatchesSpecificKeywords_UsesRWAAliases(t *testing.T) {
	item := webResearchEvidence{
		URL:          "https://example.com/news/2026/02/tokenization-accelerates",
		Title:        "Institutional tokenization accelerates in February 2026",
		Impact:       "Tokenized treasury issuance expanded across multiple markets.",
		EvidenceText: []string{"Real-world asset adoption continued to rise with broader tokenization pilots."},
	}

	require.True(t, evidenceMatchesSpecificKeywords(item, []string{"rwa"}))
}

func TestBuildInsufficientWebResearchFallback_UsesRequestForTopicalFiltering(t *testing.T) {
	fallback := buildInsufficientWebResearchFallback(
		[]toolCall{
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://duckduckgo.com/duckduckgo-help-pages/search-privacy/",
					"title": "DuckDuckGo Search Privacy Protection - DuckDuckGo Help Pages",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":       "https://duckduckgo.com/duckduckgo-help-pages/search-privacy/",
					"mode":      "text",
					"extracted": "How DuckDuckGo keeps search private and anonymous.",
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          80,
					},
				},
			},
			{
				ToolName: "browser.navigate",
				Input: map[string]any{
					"url":   "https://cointelegraph.com/news/rwa-tokenization-accelerates-in-february-2026",
					"title": "RWA tokenization accelerates in February 2026",
				},
			},
			{
				ToolName: "browser.extract",
				Input: map[string]any{
					"url":       "https://cointelegraph.com/news/rwa-tokenization-accelerates-in-february-2026",
					"mode":      "text",
					"extracted": "RWA tokenization activity accelerated in February 2026 as institutional on-chain treasury volumes increased and issuance frameworks expanded.",
					"diagnostics": map[string]any{
						"status":              "ok",
						"extractable_content": true,
						"word_count":          120,
					},
				},
			},
		},
		webResearchRequirements{Enabled: true, MinimumItems: 2},
		"Browse the web and give me the top current news stories surrounding RWAs and crypto February 2026 and a comprehensive summary",
		"I can keep researching.",
	)

	require.Contains(t, fallback, "Top stories:")
	require.Contains(t, fallback, "cointelegraph.com/news/rwa-tokenization-accelerates-in-february-2026")
	require.NotContains(t, fallback, "DuckDuckGo Search Privacy Protection - DuckDuckGo Help Pages\n   -")
}

func TestMetadataPointsToTarget_RejectsDifferentPathOnSameHost(t *testing.T) {
	metadataOutput := map[string]any{
		"url": "https://thedefiant.io/news/defi/world-liberty-financial-offloads-bitcoin-to-pay-debt",
	}
	require.False(
		t,
		metadataPointsToTarget(
			metadataOutput,
			"https://thedefiant.io/news/defi/tether-invests-usd100-million-in-anchorage-digital",
			"https://thedefiant.io/news/defi",
		),
	)
}

func TestResponseHasLowResearchQuality_FlagsUtilityLinks(t *testing.T) {
	content := "Usable sources:\n1. [DuckDuckGo Search Privacy](https://duckduckgo.com/duckduckgo-help-pages/search-privacy)"
	require.True(t, responseHasLowResearchQuality(content))
}

func TestShouldAutoGenerateResearchDoc(t *testing.T) {
	content := strings.Join([]string{
		"Here is the research summary based on extractable article sources.",
		"",
		"Top stories:",
		"1. **Story A**",
		"   - Source: [example.com](https://example.com/a)",
		"2. **Story B**",
		"   - Source: [example.com](https://example.com/b)",
		"3. **Story C**",
		"   - Source: [example.com](https://example.com/c)",
	}, "\n")
	require.True(t, shouldAutoGenerateResearchDoc(content))
	require.False(t, shouldAutoGenerateResearchDoc("Top stories:\n1. **Only one**\n   - Source: [x](https://example.com/x)"))
	require.False(t, shouldAutoGenerateResearchDoc("I could not extract enough article-grade source pages to produce a reliable top-stories summary yet."))
}

func TestSanitizeResearchUserResponse_RemovesDiagnosticsSections(t *testing.T) {
	content := strings.Join([]string{
		"I completed browser research and synthesized extractable sources.",
		"",
		"Overview: 4 extractable source(s), 2 blocked source(s), 24 low-quality extract(s).",
		"",
		"Top stories:",
		"1. **Story A**",
		"   - Source: [example.com](https://example.com/a)",
		"",
		"Low-quality extracts:",
		"1. [Search page](https://example.com/search) — search_results_page",
		"",
		"Blocked sources:",
		"1. [Blocked](https://example.com/blocked) — blocked_by_bot_protection",
	}, "\n")

	sanitized := sanitizeResearchUserResponse(content)
	require.Contains(t, sanitized, "Top stories:")
	require.NotContains(t, strings.ToLower(sanitized), "low-quality extracts:")
	require.NotContains(t, strings.ToLower(sanitized), "blocked sources:")
	require.NotContains(t, strings.ToLower(sanitized), "extractable source(s)")
}

func TestBuildResearchDocSections_StripsMarkdownAndKeepsStructure(t *testing.T) {
	content := strings.Join([]string{
		"Overview:",
		"Summary with [source](https://example.com/a) and **bold text**.",
		"",
		"Top stories:",
		"1. **Story A**",
		"   - Source: [example.com](https://example.com/a)",
	}, "\n")
	sections := buildResearchDocSections(content)
	require.NotEmpty(t, sections)
	first := sections[0]
	require.Equal(t, "Overview", first["heading"])
	firstContent, _ := first["content"].(string)
	require.Contains(t, firstContent, "source (https://example.com/a)")
	require.NotContains(t, firstContent, "**")
}

func TestPostEvent(t *testing.T) {
	store := &stubStore{}
	activities := NewRunActivities(store, llm.Config{}, nil, "http://example.com", "")

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/runs/run-1/events", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		activities.controlPlane = server.URL
		activities.httpClient = server.Client()
		err := activities.postEvent(context.Background(), "run-1", "run.failed", map[string]any{"ok": true})
		require.NoError(t, err)
	})

	t.Run("status_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		activities.controlPlane = server.URL
		activities.httpClient = server.Client()
		err := activities.postEvent(context.Background(), "run-1", "run.failed", map[string]any{"ok": true})
		require.EqualError(t, err, "control plane event failed: 400 Bad Request")
	})

	t.Run("request_error", func(t *testing.T) {
		activities.controlPlane = "http://[::1]:namedport"
		err := activities.postEvent(context.Background(), "run-1", "run.failed", map[string]any{"ok": true})
		require.Error(t, err)
	})

	t.Run("transport_error", func(t *testing.T) {
		activities.controlPlane = "http://example.com"
		activities.httpClient = &http.Client{Transport: errorRoundTripper{err: errors.New("network")}}
		err := activities.postEvent(context.Background(), "run-1", "run.failed", map[string]any{"ok": true})
		require.ErrorContains(t, err, "network")
	})

	t.Run("marshal_error", func(t *testing.T) {
		activities.controlPlane = "http://example.com"
		payload := map[string]any{"bad": make(chan int)}
		err := activities.postEvent(context.Background(), "run-1", "run.failed", payload)
		require.Error(t, err)
	})
}

func TestHandleRunFailure(t *testing.T) {
	t.Run("posts run.failed event to control plane", func(t *testing.T) {
		storeStub := &stubStore{}
		activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://example.com", "")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/runs/run-1/events", r.URL.Path)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.Equal(t, "run.failed", payload["type"])
			bodyPayload, ok := payload["payload"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "boom", bodyPayload["error"])
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		activities.controlPlane = server.URL
		activities.httpClient = server.Client()

		err := activities.HandleRunFailure(context.Background(), RunFailureInput{RunID: "run-1", Error: "boom"})
		require.NoError(t, err)
	})

	t.Run("falls back to local event append when post fails", func(t *testing.T) {
		var appended store.RunEvent
		storeStub := &stubStore{
			nextSeqFunc: func(ctx context.Context, runID string) (int64, error) {
				return 7, nil
			},
			appendEventFunc: func(ctx context.Context, event store.RunEvent) error {
				appended = event
				return nil
			},
		}
		activities := NewRunActivities(storeStub, llm.Config{}, nil, "http://[::1]:namedport", "")
		err := activities.HandleRunFailure(context.Background(), RunFailureInput{RunID: "run-2", Error: "upstream failed"})
		require.NoError(t, err)
		require.Equal(t, int64(7), appended.Seq)
		require.Equal(t, "run.failed", appended.Type)
		require.Equal(t, "llm", appended.Source)
		require.Equal(t, "upstream failed", appended.Payload["error"])
	})

	t.Run("requires run id", func(t *testing.T) {
		activities := NewRunActivities(&stubStore{}, llm.Config{}, nil, "http://example.com", "")
		err := activities.HandleRunFailure(context.Background(), RunFailureInput{})
		require.EqualError(t, err, "run_id required")
	})
}
