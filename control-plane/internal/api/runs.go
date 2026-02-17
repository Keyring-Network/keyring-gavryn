package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type runSummaryResponse struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	Phase            string   `json:"phase"`
	CompletionReason string   `json:"completion_reason,omitempty"`
	ResumedFrom      string   `json:"resumed_from,omitempty"`
	CheckpointSeq    int64    `json:"checkpoint_seq"`
	PolicyProfile    string   `json:"policy_profile,omitempty"`
	ModelRoute       string   `json:"model_route,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	Title            string   `json:"title"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	MessageCount     int64    `json:"message_count"`
}

type listRunsResponse struct {
	Runs []runSummaryResponse `json:"runs"`
}

type runStepResponse struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Source      string         `json:"source"`
	Seq         int64          `json:"seq"`
	StartedAt   string         `json:"started_at,omitempty"`
	CompletedAt string         `json:"completed_at,omitempty"`
	Error       string         `json:"error,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
}

type listRunStepsResponse struct {
	Steps []runStepResponse `json:"steps"`
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListRuns(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := listRunsResponse{Runs: make([]runSummaryResponse, 0, len(runs))}
	for _, run := range runs {
		response.Runs = append(response.Runs, runSummaryResponse{
			ID:               run.ID,
			Status:           run.Status,
			Phase:            run.Phase,
			CompletionReason: run.CompletionReason,
			ResumedFrom:      run.ResumedFrom,
			CheckpointSeq:    run.CheckpointSeq,
			PolicyProfile:    run.PolicyProfile,
			ModelRoute:       run.ModelRoute,
			Tags:             run.Tags,
			Title:            run.Title,
			CreatedAt:        run.CreatedAt,
			UpdatedAt:        run.UpdatedAt,
			MessageCount:     run.MessageCount,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	if runID == "" {
		http.Error(w, "run id required", http.StatusBadRequest)
		return
	}
	runs, err := s.store.ListRuns(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, run := range runs {
		if run.ID != runID {
			continue
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(runSummaryResponse{
			ID:               run.ID,
			Status:           run.Status,
			Phase:            run.Phase,
			CompletionReason: run.CompletionReason,
			ResumedFrom:      run.ResumedFrom,
			CheckpointSeq:    run.CheckpointSeq,
			PolicyProfile:    run.PolicyProfile,
			ModelRoute:       run.ModelRoute,
			Tags:             run.Tags,
			Title:            run.Title,
			CreatedAt:        run.CreatedAt,
			UpdatedAt:        run.UpdatedAt,
			MessageCount:     run.MessageCount,
		})
		return
	}
	http.Error(w, "run not found", http.StatusNotFound)
}

func (s *Server) listRunSteps(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	if runID == "" {
		http.Error(w, "run id required", http.StatusBadRequest)
		return
	}
	steps, err := s.store.ListRunSteps(r.Context(), runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := make([]runStepResponse, 0, len(steps))
	for _, step := range steps {
		response = append(response, runStepResponse{
			ID:          step.ID,
			Kind:        step.Kind,
			Name:        step.Name,
			Status:      step.Status,
			Source:      step.Source,
			Seq:         step.Seq,
			StartedAt:   step.StartedAt,
			CompletedAt: step.CompletedAt,
			Error:       step.Error,
			Diagnostics: step.Diagnostics,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(listRunStepsResponse{Steps: response})
}

func (s *Server) deleteRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	if runID == "" {
		http.Error(w, "run id required", http.StatusBadRequest)
		return
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(r.Context(), 10*time.Second)
	_, _ = s.stopAllWorkspaceProcesses(cleanupCtx, runID)
	cleanupCancel()
	if s.workflows != nil {
		_ = s.workflows.CancelRun(r.Context(), runID)
	}
	if err := s.store.DeleteRun(r.Context(), runID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type resumeRunRequest struct {
	Message string `json:"message"`
}

func (s *Server) resumeRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	if runID == "" {
		http.Error(w, "run id required", http.StatusBadRequest)
		return
	}
	if !s.ensureLLMConfigured(w, r.Context()) {
		return
	}

	runs, err := s.store.ListRuns(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var summary *runSummaryResponse
	for _, run := range runs {
		if run.ID != runID {
			continue
		}
		summary = &runSummaryResponse{
			ID:               run.ID,
			Status:           run.Status,
			Phase:            run.Phase,
			CompletionReason: run.CompletionReason,
			ResumedFrom:      run.ResumedFrom,
			CheckpointSeq:    run.CheckpointSeq,
			PolicyProfile:    run.PolicyProfile,
			ModelRoute:       run.ModelRoute,
			Tags:             run.Tags,
			Title:            run.Title,
			CreatedAt:        run.CreatedAt,
			UpdatedAt:        run.UpdatedAt,
			MessageCount:     run.MessageCount,
		}
		break
	}
	if summary == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	if strings.EqualFold(summary.Status, "running") {
		http.Error(w, "run is already running", http.StatusConflict)
		return
	}

	req := resumeRunRequest{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	messageContent := strings.TrimSpace(req.Message)
	if messageContent == "" {
		messageContent = "Continue from the latest checkpoint and complete the task."
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	message := store.Message{
		ID:        uuid.New().String(),
		RunID:     runID,
		Role:      "user",
		Content:   messageContent,
		Sequence:  time.Now().UnixNano(),
		CreatedAt: now,
		Metadata: map[string]any{
			"resume": true,
		},
	}
	if err := s.store.AddMessage(r.Context(), message); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.indexMessageMemory(r.Context(), message)

	if s.workflows != nil {
		if err := s.workflows.ResumeRun(r.Context(), runID, messageContent); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}

	seq, _ := s.store.NextSeq(r.Context(), runID)
	event := store.RunEvent{
		RunID:     runID,
		Seq:       seq,
		Type:      "run.resumed",
		Timestamp: now,
		Source:    "control_plane",
		TraceID:   uuid.New().String(),
		Payload: map[string]any{
			"message_id": message.ID,
			"status":     "running",
			"phase":      "planning",
		},
	}
	_ = s.store.AppendEvent(r.Context(), event)
	_ = s.upsertArtifactsFromEvent(r.Context(), event)
	s.broker.Publish(toEvent(event))

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"run_id": runID,
		"status": "running",
	})
}
