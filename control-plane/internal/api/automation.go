package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type automationExecuteRequest struct {
	Prompt                 string         `json:"prompt"`
	Goal                   string         `json:"goal"`
	PolicyProfile          string         `json:"policy_profile"`
	ModelRoute             string         `json:"model_route"`
	Tags                   []string       `json:"tags"`
	Metadata               map[string]any `json:"metadata"`
	TimeoutMS              int            `json:"timeout_ms"`
	PollIntervalMS         int            `json:"poll_interval_ms"`
	WaitForCompletion      *bool          `json:"wait_for_completion"`
	BrowserMode            string         `json:"browser_mode"`
	BrowserInteraction     string         `json:"browser_interaction"`
	BrowserDomainAllowlist []string       `json:"browser_domain_allowlist"`
	BrowserPreferred       string         `json:"browser_preferred_browser"`
	BrowserUserAgent       string         `json:"browser_user_agent"`
}

type automationSourceDiagnostic struct {
	URL                string `json:"url,omitempty"`
	Title              string `json:"title,omitempty"`
	Status             string `json:"status,omitempty"`
	ReasonCode         string `json:"reason_code,omitempty"`
	ReasonDetail       string `json:"reason_detail,omitempty"`
	ExtractableContent bool   `json:"extractable_content,omitempty"`
	WordCount          int    `json:"word_count,omitempty"`
}

type automationDiagnostics struct {
	UsableSources    int                          `json:"usable_sources"`
	BlockedSources   int                          `json:"blocked_sources"`
	LowQuality       int                          `json:"low_quality_sources"`
	Sources          []automationSourceDiagnostic `json:"sources"`
	TerminalEvent    string                       `json:"terminal_event,omitempty"`
	TerminalReason   string                       `json:"terminal_reason,omitempty"`
	TotalEvents      int                          `json:"total_events"`
	AssistantMessage int                          `json:"assistant_messages"`
}

type automationExecuteResponse struct {
	RunID            string                `json:"run_id"`
	Status           string                `json:"status"`
	Phase            string                `json:"phase"`
	CompletionReason string                `json:"completion_reason,omitempty"`
	TimedOut         bool                  `json:"timed_out"`
	FinalResponse    string                `json:"final_response,omitempty"`
	Diagnostics      automationDiagnostics `json:"diagnostics"`
}

const (
	defaultAutomationTimeout      = 3 * time.Minute
	defaultAutomationPollInterval = 500 * time.Millisecond
)

func (s *Server) executeAutomationRun(w http.ResponseWriter, r *http.Request) {
	if !s.ensureLLMConfigured(w, r.Context()) {
		return
	}

	req := automationExecuteRequest{}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(req.Goal)
	}
	if prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	runID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	policyProfile := strings.TrimSpace(req.PolicyProfile)
	if policyProfile == "" {
		policyProfile = "default"
	}

	run := store.Run{
		ID:            runID,
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
		_ = s.workflows.StartRun(r.Context(), runID)
	}

	seq, _ := s.store.NextSeq(r.Context(), runID)
	startedEvent := store.RunEvent{
		RunID:     runID,
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
	_ = s.store.AppendEvent(r.Context(), startedEvent)
	s.broker.Publish(toEvent(startedEvent))

	metadata := cloneMetadataMap(req.Metadata)
	if mode := strings.TrimSpace(req.BrowserMode); mode != "" {
		metadata["browser_mode"] = mode
	}
	if interaction := strings.TrimSpace(req.BrowserInteraction); interaction != "" {
		metadata["browser_interaction"] = interaction
	}
	if preferred := strings.TrimSpace(req.BrowserPreferred); preferred != "" {
		metadata["browser_preferred_browser"] = preferred
	}
	if userAgent := strings.TrimSpace(req.BrowserUserAgent); userAgent != "" {
		metadata["browser_user_agent"] = userAgent
	}
	if len(req.BrowserDomainAllowlist) > 0 {
		trimmed := make([]string, 0, len(req.BrowserDomainAllowlist))
		for _, item := range req.BrowserDomainAllowlist {
			if value := strings.TrimSpace(item); value != "" {
				trimmed = append(trimmed, value)
			}
		}
		if len(trimmed) > 0 {
			metadata["browser_domain_allowlist"] = strings.Join(trimmed, ",")
		}
	}

	msg := store.Message{
		ID:        uuid.New().String(),
		RunID:     runID,
		Role:      "user",
		Content:   prompt,
		Sequence:  time.Now().UnixNano(),
		CreatedAt: now,
		Metadata:  metadata,
	}
	if err := s.store.AddMessage(r.Context(), msg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.indexMessageMemory(r.Context(), msg)
	if s.workflows != nil {
		_ = s.workflows.SignalMessage(r.Context(), runID, prompt)
	}

	seq, _ = s.store.NextSeq(r.Context(), runID)
	messageEvent := store.RunEvent{
		RunID:     runID,
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

	waitForCompletion := true
	if req.WaitForCompletion != nil {
		waitForCompletion = *req.WaitForCompletion
	}
	if !waitForCompletion {
		writeJSONStatus(w, automationExecuteResponse{
			RunID:  runID,
			Status: "running",
			Phase:  "planning",
			Diagnostics: automationDiagnostics{
				Sources:     []automationSourceDiagnostic{},
				TotalEvents: 2,
			},
		}, http.StatusAccepted)
		return
	}

	timeout := normalizeDuration(req.TimeoutMS, defaultAutomationTimeout, 5*time.Second, 30*time.Minute)
	poll := normalizeDuration(req.PollIntervalMS, defaultAutomationPollInterval, 100*time.Millisecond, 5*time.Second)
	waitCtx, cancel := contextWithTimeoutOrRequest(r.Context(), timeout)
	defer cancel()

	var (
		eventsList []store.RunEvent
		terminal   *store.RunEvent
	)
	for {
		currentEvents, err := s.store.ListEvents(waitCtx, runID, 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		eventsList = currentEvents
		terminal = latestTerminalEvent(eventsList)
		if terminal != nil {
			break
		}
		select {
		case <-waitCtx.Done():
			break
		case <-time.After(poll):
		}
		if waitCtx.Err() != nil {
			break
		}
	}

	messages, err := s.store.ListMessages(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	finalResponse, assistantMessages := latestAssistantMessage(messages)
	status, phase, completionReason := deriveAutomationStatusFromTerminalEvent(terminal)
	timedOut := terminal == nil
	if timedOut {
		status = "running"
		phase = "executing"
		completionReason = "timeout_waiting_for_terminal_event"
	}

	diagnostics := buildAutomationDiagnostics(eventsList, assistantMessages)
	diagnostics.TerminalEvent = terminalEventType(terminal)
	diagnostics.TerminalReason = completionReason

	log.Printf("automation.execute run_id=%s status=%s reason=%s timed_out=%t", runID, status, completionReason, timedOut)
	if strings.TrimSpace(finalResponse) != "" {
		log.Printf("automation.final_response run_id=%s\n%s", runID, finalResponse)
	}

	writeJSONStatus(w, automationExecuteResponse{
		RunID:            runID,
		Status:           status,
		Phase:            phase,
		CompletionReason: completionReason,
		TimedOut:         timedOut,
		FinalResponse:    finalResponse,
		Diagnostics:      diagnostics,
	}, http.StatusOK)
}

func cloneMetadataMap(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return map[string]any{}
	}
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}

func normalizeDuration(rawMS int, fallback time.Duration, min time.Duration, max time.Duration) time.Duration {
	value := fallback
	if rawMS > 0 {
		value = time.Duration(rawMS) * time.Millisecond
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func contextWithTimeoutOrRequest(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func latestTerminalEvent(eventsList []store.RunEvent) *store.RunEvent {
	for i := len(eventsList) - 1; i >= 0; i-- {
		normalized := strings.TrimSpace(strings.ToLower(eventsList[i].Type))
		switch normalized {
		case "run.completed", "run.partial", "run.failed", "run.cancelled":
			event := eventsList[i]
			return &event
		}
	}
	return nil
}

func terminalEventType(event *store.RunEvent) string {
	if event == nil {
		return ""
	}
	return strings.TrimSpace(event.Type)
}

func deriveAutomationStatusFromTerminalEvent(event *store.RunEvent) (string, string, string) {
	if event == nil {
		return "", "", ""
	}
	status := strings.TrimSpace(toStringValue(event.Payload["status"]))
	phase := strings.TrimSpace(toStringValue(event.Payload["phase"]))
	reason := strings.TrimSpace(toStringValue(event.Payload["completion_reason"]))

	switch strings.TrimSpace(strings.ToLower(event.Type)) {
	case "run.completed":
		if status == "" {
			status = "completed"
		}
		if phase == "" {
			phase = "completed"
		}
	case "run.partial":
		if status == "" {
			status = "partial"
		}
		if phase == "" {
			phase = "completed"
		}
	case "run.failed":
		if status == "" {
			status = "failed"
		}
		if phase == "" {
			phase = "failed"
		}
	case "run.cancelled":
		if status == "" {
			status = "cancelled"
		}
		if phase == "" {
			phase = "cancelled"
		}
	}

	if status == "" {
		status = "completed"
	}
	if phase == "" {
		phase = "completed"
	}
	return status, phase, reason
}

func latestAssistantMessage(messages []store.Message) (string, int) {
	assistantCount := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.TrimSpace(strings.ToLower(messages[i].Role)) != "assistant" {
			continue
		}
		assistantCount++
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.TrimSpace(strings.ToLower(messages[i].Role)) != "assistant" {
			continue
		}
		content := strings.TrimSpace(messages[i].Content)
		if content != "" {
			return content, assistantCount
		}
	}
	return "", assistantCount
}

func toStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return strings.TrimSpace(toJSONSafeString(typed))
	}
}

func toJSONSafeString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func toIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}

func toBoolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.TrimSpace(strings.ToLower(typed)) {
		case "true", "1", "yes", "y":
			return true
		}
	}
	return false
}

func buildAutomationDiagnostics(eventsList []store.RunEvent, assistantMessages int) automationDiagnostics {
	diagnostics := automationDiagnostics{
		Sources:          []automationSourceDiagnostic{},
		TotalEvents:      len(eventsList),
		AssistantMessage: assistantMessages,
	}
	seen := map[string]struct{}{}
	addSource := func(candidate automationSourceDiagnostic) {
		parts := make([]string, 0, 5)
		for _, value := range []string{candidate.URL, candidate.Title, candidate.ReasonCode, candidate.Status, candidate.ReasonDetail} {
			trimmed := strings.TrimSpace(strings.ToLower(value))
			if trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		key := strings.Join(parts, "|")
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		diagnostics.Sources = append(diagnostics.Sources, candidate)
	}

	for _, event := range eventsList {
		eventType := strings.TrimSpace(strings.ToLower(event.Type))
		if eventType == "tool.completed" {
			toolName := strings.TrimSpace(strings.ToLower(toStringValue(event.Payload["tool_name"])))
			if toolName != "browser.extract" {
				continue
			}
			output, _ := event.Payload["output"].(map[string]any)
			diagnostic := automationSourceDiagnostic{
				URL:   strings.TrimSpace(toStringValue(output["url"])),
				Title: strings.TrimSpace(toStringValue(output["title"])),
			}
			diagPayload, _ := output["diagnostics"].(map[string]any)
			if diagPayload != nil {
				diagnostic.Status = strings.TrimSpace(toStringValue(diagPayload["status"]))
				diagnostic.ReasonCode = strings.TrimSpace(toStringValue(diagPayload["reason_code"]))
				diagnostic.ReasonDetail = strings.TrimSpace(toStringValue(diagPayload["reason_detail"]))
				diagnostic.ExtractableContent = toBoolValue(diagPayload["extractable_content"])
				diagnostic.WordCount = toIntValue(diagPayload["word_count"])
			}
			if diagnostic.ReasonCode == "" {
				diagnostic.ReasonCode = strings.TrimSpace(toStringValue(output["reason_code"]))
			}
			addSource(diagnostic)
			continue
		}

		if eventType == "browser.extract" {
			diagnostic := automationSourceDiagnostic{
				URL:   strings.TrimSpace(toStringValue(event.Payload["url"])),
				Title: strings.TrimSpace(toStringValue(event.Payload["title"])),
			}
			diagPayload, _ := event.Payload["diagnostics"].(map[string]any)
			if diagPayload != nil {
				diagnostic.Status = strings.TrimSpace(toStringValue(diagPayload["status"]))
				diagnostic.ReasonCode = strings.TrimSpace(toStringValue(diagPayload["reason_code"]))
				diagnostic.ReasonDetail = strings.TrimSpace(toStringValue(diagPayload["reason_detail"]))
				diagnostic.ExtractableContent = toBoolValue(diagPayload["extractable_content"])
				diagnostic.WordCount = toIntValue(diagPayload["word_count"])
			}
			if diagnostic.ReasonCode == "" {
				diagnostic.ReasonCode = strings.TrimSpace(toStringValue(event.Payload["reason_code"]))
			}
			addSource(diagnostic)
		}
	}

	for _, source := range diagnostics.Sources {
		reasonCode := strings.TrimSpace(strings.ToLower(source.ReasonCode))
		status := strings.TrimSpace(strings.ToLower(source.Status))
		switch reasonCode {
		case "blocked_by_bot_protection", "consent_wall", "login_wall":
			diagnostics.BlockedSources++
		case "no_extractable_content":
			diagnostics.LowQuality++
		default:
			if source.ExtractableContent || status == "ok" {
				diagnostics.UsableSources++
			} else {
				diagnostics.LowQuality++
			}
		}
	}

	return diagnostics
}
