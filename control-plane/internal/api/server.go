package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/events"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type Server struct {
	store        store.Store
	broker       Broker
	workflows    WorkflowService
	cfg          config.Config
	httpClient   *http.Client
	automationMu sync.Mutex
}

type Broker interface {
	Publish(event events.RunEvent)
	Subscribe(ctx context.Context, runID string) <-chan events.RunEvent
}

type WorkflowService interface {
	StartRun(ctx context.Context, runID string) error
	SignalMessage(ctx context.Context, runID string, message string) error
	ResumeRun(ctx context.Context, runID string, message string) error
	CancelRun(ctx context.Context, runID string) error
}

func NewServer(store store.Store, broker Broker, workflows WorkflowService, cfg config.Config) *Server {
	return &Server{
		store:      store,
		broker:     broker,
		workflows:  workflows,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(quietRequestLogger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Post("/runs", s.createRun)
	r.Get("/runs", s.listRuns)
	r.Get("/runs/{id}", s.getRun)
	r.Delete("/runs/{id}", s.deleteRun)
	r.Post("/runs/{id}/messages", s.addMessage)
	r.Post("/runs/{id}/resume", s.resumeRun)
	r.Post("/runs/{id}/cancel", s.cancelRun)
	r.Post("/runs/{id}/events", s.ingestEvent)
	r.Get("/runs/{id}/events", s.streamEvents)
	r.Post("/automation/execute", s.executeAutomationRun)
	r.Get("/automations", s.listAutomations)
	r.Post("/automations", s.createAutomation)
	r.Put("/automations/{id}", s.updateAutomation)
	r.Delete("/automations/{id}", s.deleteAutomation)
	r.Get("/automations/{id}/inbox", s.getAutomationInbox)
	r.Post("/automations/{id}/inbox/{entryID}/read", s.markAutomationInboxRead)
	r.Post("/automations/{id}/inbox/read-all", s.markAutomationInboxReadAll)
	r.Post("/automations/process-due", s.processDueAutomations)
	r.Post("/automations/{id}/run", s.runAutomationNow)
	r.Get("/runs/{id}/steps", s.listRunSteps)
	r.Get("/runs/{id}/workspace", s.listWorkspace)
	r.Get("/runs/{id}/workspace/tree", s.listWorkspaceTree)
	r.Get("/runs/{id}/workspace/file", s.readWorkspaceFile)
	r.Put("/runs/{id}/workspace/file", s.writeWorkspaceFile)
	r.Delete("/runs/{id}/workspace/file", s.deleteWorkspaceFile)
	r.Get("/runs/{id}/workspace/stat", s.statWorkspaceFile)
	r.Post("/runs/{id}/processes/exec", s.execWorkspaceProcess)
	r.Get("/runs/{id}/processes", s.listWorkspaceProcesses)
	r.Post("/runs/{id}/processes/start", s.startWorkspaceProcess)
	r.Get("/runs/{id}/processes/{pid}", s.getWorkspaceProcess)
	r.Get("/runs/{id}/processes/{pid}/logs", s.getWorkspaceProcessLogs)
	r.Post("/runs/{id}/processes/{pid}/stop", s.stopWorkspaceProcess)
	r.Get("/runs/{id}/artifacts", s.listArtifacts)
	r.Get("/settings/llm", s.getLLMSettings)
	r.Post("/settings/llm", s.updateLLMSettings)
	r.Post("/settings/llm/test", s.testLLMSettings)
	r.Post("/settings/llm/models", s.listLLMModels)
	r.Get("/settings/memory", s.getMemorySettings)
	r.Post("/settings/memory", s.updateMemorySettings)
	r.Get("/settings/personality", s.getPersonalitySettings)
	r.Post("/settings/personality", s.updatePersonalitySettings)
	r.Get("/skills", s.listSkills)
	r.Post("/skills", s.createSkill)
	r.Put("/skills/{id}", s.updateSkill)
	r.Delete("/skills/{id}", s.deleteSkill)
	r.Get("/skills/{id}/files", s.listSkillFiles)
	r.Post("/skills/{id}/files", s.upsertSkillFiles)
	r.Delete("/skills/{id}/files", s.deleteSkillFiles)
	r.Get("/context", s.listContextNodes)
	r.Post("/context/folders", s.createContextFolder)
	r.Post("/context/files", s.uploadContextFile)
	r.Get("/context/files/{id}", s.getContextFile)
	r.Delete("/context/{id}", s.deleteContextNode)
	r.Get("/health", s.health)
	r.Get("/ready", s.ready)

	return r
}

func quietRequestLogger(next http.Handler) http.Handler {
	logged := middleware.Logger(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldSuppressRequestLog(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		logged.ServeHTTP(w, r)
	})
}

func shouldSuppressRequestLog(method string, path string) bool {
	cleanPath := strings.TrimSpace(path)
	if method == http.MethodPost && strings.HasSuffix(cleanPath, "/events") {
		return true
	}
	if method == http.MethodGet && strings.HasSuffix(cleanPath, "/events") {
		return true
	}
	if method == http.MethodGet && (cleanPath == "/runs" || strings.HasPrefix(cleanPath, "/settings/")) {
		return true
	}
	if method == http.MethodOptions && (strings.HasSuffix(cleanPath, "/messages") || strings.HasPrefix(cleanPath, "/settings/")) {
		return true
	}
	return false
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type subsystemStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type readinessResponse struct {
	Status     string                     `json:"status"`
	Subsystems map[string]subsystemStatus `json:"subsystems"`
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	subsystems := map[string]subsystemStatus{}
	overall := http.StatusOK

	if _, err := s.store.ListRuns(ctx); err != nil {
		subsystems["store"] = subsystemStatus{Status: "error", Error: err.Error()}
		overall = http.StatusServiceUnavailable
	} else {
		subsystems["store"] = subsystemStatus{Status: "ok"}
	}

	toolRunnerURL := strings.TrimSpace(s.cfg.ToolRunnerURL)
	if toolRunnerURL == "" {
		subsystems["tool_runner"] = subsystemStatus{Status: "skipped"}
	} else {
		baseURL := strings.TrimRight(toolRunnerURL, "/")
		resp, err := s.probeHTTP(ctx, baseURL+"/ready")
		if err == nil && resp != nil && resp.StatusCode == http.StatusNotFound {
			resp, err = s.probeHTTP(ctx, baseURL+"/health")
		}
		if err != nil {
			subsystems["tool_runner"] = subsystemStatus{Status: "error", Error: err.Error()}
			overall = http.StatusServiceUnavailable
		} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			subsystems["tool_runner"] = subsystemStatus{Status: "error", Error: fmt.Sprintf("health status %d", resp.StatusCode)}
			overall = http.StatusServiceUnavailable
		} else {
			subsystems["tool_runner"] = subsystemStatus{Status: "ok"}
		}
	}

	status := "ok"
	if overall != http.StatusOK {
		status = "degraded"
	}
	writeJSONStatus(w, readinessResponse{Status: status, Subsystems: subsystems}, overall)
}

func writeJSONStatus(w http.ResponseWriter, value any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) probeHTTP(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, resp.Body.Close()
}

type createRunRequest struct {
	Goal          string         `json:"goal"`
	PolicyProfile string         `json:"policy_profile"`
	ModelRoute    string         `json:"model_route"`
	Tags          []string       `json:"tags"`
	Metadata      map[string]any `json:"metadata"`
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	if !s.ensureLLMConfigured(w, r.Context()) {
		return
	}
	req := createRunRequest{}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
	}
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	policyProfile := strings.TrimSpace(req.PolicyProfile)
	if policyProfile == "" {
		policyProfile = "default"
	}
	run := store.Run{
		ID:            id,
		Status:        "running",
		Phase:         "planning",
		PolicyProfile: policyProfile,
		ModelRoute:    strings.TrimSpace(req.ModelRoute),
		Tags:          req.Tags,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.CreateRun(r.Context(), run); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if s.workflows != nil {
		_ = s.workflows.StartRun(r.Context(), id)
	}

	seq, _ := s.store.NextSeq(r.Context(), id)
	event := store.RunEvent{
		RunID:     id,
		Seq:       seq,
		Type:      "run.started",
		Timestamp: now,
		Source:    "control_plane",
		TraceID:   uuid.New().String(),
		Payload: map[string]any{
			"status":         "running",
			"phase":          "planning",
			"policy_profile": policyProfile,
			"model_route":    run.ModelRoute,
			"tags":           run.Tags,
		},
	}
	_ = s.store.AppendEvent(r.Context(), event)
	_ = s.upsertArtifactsFromEvent(r.Context(), event)
	_ = s.upsertProcessesFromEvent(r.Context(), event)
	s.broker.Publish(toEvent(event))

	goal := strings.TrimSpace(req.Goal)
	if goal != "" {
		msg := store.Message{
			ID:        uuid.New().String(),
			RunID:     id,
			Role:      "user",
			Content:   goal,
			Sequence:  time.Now().UnixNano(),
			CreatedAt: now,
			Metadata:  req.Metadata,
		}
		if err := s.store.AddMessage(r.Context(), msg); err == nil {
			s.indexMessageMemory(r.Context(), msg)
			if s.workflows != nil {
				_ = s.workflows.SignalMessage(r.Context(), id, goal)
			}
			seq, _ = s.store.NextSeq(r.Context(), id)
			messageEvent := store.RunEvent{
				RunID:     id,
				Seq:       seq,
				Type:      "message.added",
				Timestamp: now,
				Source:    "control_plane",
				TraceID:   uuid.New().String(),
				Payload: map[string]any{
					"message_id": msg.ID,
					"role":       msg.Role,
					"content":    msg.Content,
				},
			}
			_ = s.store.AppendEvent(r.Context(), messageEvent)
			s.broker.Publish(toEvent(messageEvent))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"run_id":         id,
		"status":         "running",
		"phase":          "planning",
		"policy_profile": policyProfile,
	})
}

type addMessageRequest struct {
	Role     string         `json:"role"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
}

func (s *Server) addMessage(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	var req addMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Role == "user" {
		if !s.ensureLLMConfigured(w, r.Context()) {
			return
		}
	}

	msg := store.Message{
		ID:        uuid.New().String(),
		RunID:     runID,
		Role:      req.Role,
		Content:   req.Content,
		Sequence:  time.Now().UnixNano(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Metadata:  req.Metadata,
	}
	if err := s.store.AddMessage(r.Context(), msg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.indexMessageMemory(r.Context(), msg)

	if s.workflows != nil && req.Role == "user" {
		_ = s.workflows.SignalMessage(r.Context(), runID, req.Content)
	}

	seq, _ := s.store.NextSeq(r.Context(), runID)
	event := store.RunEvent{
		RunID:     runID,
		Seq:       seq,
		Type:      "message.added",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "control_plane",
		TraceID:   uuid.New().String(),
		Payload:   map[string]any{"message_id": msg.ID, "role": msg.Role, "content": msg.Content},
	}
	_ = s.store.AppendEvent(r.Context(), event)
	_ = s.upsertArtifactsFromEvent(r.Context(), event)
	_ = s.upsertProcessesFromEvent(r.Context(), event)
	s.broker.Publish(toEvent(event))

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	cleanupCtx, cleanupCancel := context.WithTimeout(r.Context(), 10*time.Second)
	stoppedCount, cleanupErr := s.stopAllWorkspaceProcesses(cleanupCtx, runID)
	cleanupCancel()
	if s.workflows != nil {
		_ = s.workflows.CancelRun(r.Context(), runID)
	}
	if stoppedCount > 0 || cleanupErr != nil {
		seq, _ := s.store.NextSeq(r.Context(), runID)
		cleanupPayload := map[string]any{
			"stopped_processes": stoppedCount,
			"phase":             "cleanup",
		}
		if cleanupErr != nil {
			cleanupPayload["error"] = cleanupErr.Error()
		}
		cleanupEvent := store.RunEvent{
			RunID:     runID,
			Seq:       seq,
			Type:      "run.cleanup.processes",
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Source:    "control_plane",
			TraceID:   uuid.New().String(),
			Payload:   cleanupPayload,
		}
		_ = s.store.AppendEvent(r.Context(), cleanupEvent)
		_ = s.upsertArtifactsFromEvent(r.Context(), cleanupEvent)
		_ = s.upsertProcessesFromEvent(r.Context(), cleanupEvent)
		s.broker.Publish(toEvent(cleanupEvent))
	}
	seq, _ := s.store.NextSeq(r.Context(), runID)
	event := store.RunEvent{
		RunID:     runID,
		Seq:       seq,
		Type:      "run.cancelled",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "control_plane",
		TraceID:   uuid.New().String(),
		Payload:   map[string]any{"reason": "user_requested"},
	}
	_ = s.store.AppendEvent(r.Context(), event)
	_ = s.upsertArtifactsFromEvent(r.Context(), event)
	_ = s.upsertProcessesFromEvent(r.Context(), event)
	s.broker.Publish(toEvent(event))
	w.WriteHeader(http.StatusAccepted)
}

type ingestEventRequest struct {
	Type      string         `json:"type"`
	Source    string         `json:"source"`
	Timestamp string         `json:"timestamp"`
	TraceID   string         `json:"trace_id"`
	Payload   map[string]any `json:"payload"`
}

func (s *Server) ingestEvent(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	var req ingestEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		http.Error(w, "event type required", http.StatusBadRequest)
		return
	}
	if strings.Contains(req.Type, "_") {
		http.Error(w, "event type must use dot notation", http.StatusBadRequest)
		return
	}

	timestamp := req.Timestamp
	if timestamp == "" {
		timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	seq, _ := s.store.NextSeq(r.Context(), runID)
	event := store.RunEvent{
		RunID:     runID,
		Seq:       seq,
		Type:      events.NormalizeType(req.Type),
		Timestamp: timestamp,
		Source:    req.Source,
		TraceID:   strings.TrimSpace(req.TraceID),
		Payload:   req.Payload,
	}
	if event.TraceID == "" {
		event.TraceID = uuid.New().String()
	}
	if isTransientPayload(req.Payload) {
		s.broker.Publish(toEvent(event))
		w.WriteHeader(http.StatusAccepted)
		return
	}
	_ = s.store.AppendEvent(r.Context(), event)
	_ = s.upsertArtifactsFromEvent(r.Context(), event)
	_ = s.upsertProcessesFromEvent(r.Context(), event)
	s.broker.Publish(toEvent(event))

	w.WriteHeader(http.StatusAccepted)
}

func isTransientPayload(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if value, ok := payload["transient"]; ok {
		if flag, ok := value.(bool); ok {
			return flag
		}
	}
	return false
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	afterSeq := parseAfterSeq(runID, r)
	stored, err := s.store.ListEvents(ctx, runID, afterSeq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, event := range stored {
		sendSSE(w, toEvent(event))
		flusher.Flush()
	}

	eventsChan := s.broker.Subscribe(ctx, runID)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case event, ok := <-eventsChan:
			if !ok {
				return
			}
			sendSSE(w, event)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

func sendSSE(w http.ResponseWriter, event events.RunEvent) {
	payload, _ := json.Marshal(event)
	fmt.Fprintf(w, "id: %s:%d\n", event.RunID, event.Seq)
	fmt.Fprint(w, "event: run_event\n")
	fmt.Fprintf(w, "data: %s\n\n", payload)
}

func toEvent(event store.RunEvent) events.RunEvent {
	return events.RunEvent{
		RunID:   event.RunID,
		Seq:     event.Seq,
		Type:    events.NormalizeType(event.Type),
		Ts:      event.Timestamp,
		Source:  event.Source,
		TraceID: event.TraceID,
		Payload: event.Payload,
	}
}

func parseAfterSeq(runID string, r *http.Request) int64 {
	afterParam := strings.TrimSpace(r.URL.Query().Get("after_seq"))
	if afterParam != "" {
		if parsed, err := strconv.ParseInt(afterParam, 10, 64); err == nil {
			return parsed
		}
	}
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		return 0
	}
	parts := strings.Split(lastEventID, ":")
	if len(parts) != 2 {
		return 0
	}
	if parts[0] != runID {
		return 0
	}
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

func (s *Server) ensureLLMConfigured(w http.ResponseWriter, ctx context.Context) bool {
	settings, err := s.store.GetLLMSettings(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if settings == nil {
		http.Error(w, "LLM setup required", http.StatusPreconditionFailed)
		return false
	}
	return true
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Start(ctx context.Context, addr string) error {
	server := &http.Server{
		Addr:    addr,
		Handler: s.Router(),
	}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	return server.ListenAndServe()
}
