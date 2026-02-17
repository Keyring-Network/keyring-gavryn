package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

var weekdayIndex = map[string]time.Weekday{
	"sun": time.Sunday,
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
}

type automationSchedule struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prompt     string   `json:"prompt"`
	Model      string   `json:"model"`
	Days       []string `json:"days"`
	TimeOfDay  string   `json:"time"`
	Timezone   string   `json:"timezone"`
	Enabled    bool     `json:"enabled"`
	NextRunAt  string   `json:"next_run_at,omitempty"`
	LastRunAt  string   `json:"last_run_at,omitempty"`
	InProgress bool     `json:"in_progress"`
	Unread     int      `json:"unread_count"`
	LastStatus string   `json:"last_status,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

type automationInboxEntry struct {
	ID               string                `json:"id"`
	AutomationID     string                `json:"automation_id"`
	RunID            string                `json:"run_id,omitempty"`
	Status           string                `json:"status"`
	Phase            string                `json:"phase,omitempty"`
	CompletionReason string                `json:"completion_reason,omitempty"`
	FinalResponse    string                `json:"final_response,omitempty"`
	TimedOut         bool                  `json:"timed_out"`
	Error            string                `json:"error,omitempty"`
	Unread           bool                  `json:"unread"`
	Trigger          string                `json:"trigger"`
	StartedAt        string                `json:"started_at"`
	CompletedAt      string                `json:"completed_at,omitempty"`
	Diagnostics      automationDiagnostics `json:"diagnostics"`
}

type automationUpsertRequest struct {
	Name      string   `json:"name"`
	Prompt    string   `json:"prompt"`
	Model     string   `json:"model"`
	Days      []string `json:"days"`
	TimeOfDay string   `json:"time"`
	Timezone  string   `json:"timezone"`
	Enabled   *bool    `json:"enabled"`
}

type automationsListResponse struct {
	Automations []automationSchedule `json:"automations"`
	UnreadCount int                  `json:"unread_count"`
}

type automationDetailResponse struct {
	Automation  automationSchedule     `json:"automation"`
	Inbox       []automationInboxEntry `json:"inbox"`
	UnreadCount int                    `json:"unread_count"`
}

type automationQueueResponse struct {
	Queued bool   `json:"queued"`
	Error  string `json:"error,omitempty"`
}

type automationProcessResponse struct {
	Queued int `json:"queued"`
}

func normalizeDays(days []string) []string {
	if len(days) == 0 {
		return []string{"mon", "tue", "wed", "thu", "fri"}
	}
	seen := map[string]struct{}{}
	ordered := make([]string, 0, len(days))
	for _, raw := range days {
		day := strings.TrimSpace(strings.ToLower(raw))
		if _, ok := weekdayIndex[day]; !ok {
			continue
		}
		if _, exists := seen[day]; exists {
			continue
		}
		seen[day] = struct{}{}
		ordered = append(ordered, day)
	}
	if len(ordered) == 0 {
		return []string{"mon", "tue", "wed", "thu", "fri"}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return weekdayIndex[ordered[i]] < weekdayIndex[ordered[j]]
	})
	return ordered
}

func normalizeTimezone(value string) string {
	tz := strings.TrimSpace(value)
	if tz == "" {
		return "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}
	return tz
}

func normalizeTimeOfDay(value string) (string, error) {
	timePart := strings.TrimSpace(value)
	if timePart == "" {
		timePart = "09:00"
	}
	parsed, err := time.Parse("15:04", timePart)
	if err != nil {
		return "", errors.New("time must use HH:MM format")
	}
	return parsed.Format("15:04"), nil
}

func computeNextRun(days []string, timeOfDay string, timezone string, from time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(normalizeTimezone(timezone))
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse("15:04", timeOfDay)
	if err != nil {
		return time.Time{}, err
	}
	hour := parsed.Hour()
	minute := parsed.Minute()
	allowed := map[time.Weekday]struct{}{}
	for _, day := range normalizeDays(days) {
		allowed[weekdayIndex[day]] = struct{}{}
	}
	nowLocal := from.In(loc)
	for i := 0; i < 14; i++ {
		candidateDay := nowLocal.AddDate(0, 0, i)
		candidate := time.Date(candidateDay.Year(), candidateDay.Month(), candidateDay.Day(), hour, minute, 0, 0, loc)
		if _, ok := allowed[candidate.Weekday()]; !ok {
			continue
		}
		if candidate.After(nowLocal) {
			return candidate.UTC(), nil
		}
	}
	return time.Time{}, errors.New("failed to compute next run")
}

func toScheduleRecord(value store.Automation) automationSchedule {
	return automationSchedule{
		ID:         value.ID,
		Name:       value.Name,
		Prompt:     value.Prompt,
		Model:      value.Model,
		Days:       append([]string(nil), value.Days...),
		TimeOfDay:  value.TimeOfDay,
		Timezone:   value.Timezone,
		Enabled:    value.Enabled,
		NextRunAt:  value.NextRunAt,
		LastRunAt:  value.LastRunAt,
		InProgress: value.InProgress,
		CreatedAt:  value.CreatedAt,
		UpdatedAt:  value.UpdatedAt,
	}
}

func toStoreAutomation(value automationSchedule) store.Automation {
	return store.Automation{
		ID:         value.ID,
		Name:       value.Name,
		Prompt:     value.Prompt,
		Model:      value.Model,
		Days:       append([]string(nil), value.Days...),
		TimeOfDay:  value.TimeOfDay,
		Timezone:   value.Timezone,
		Enabled:    value.Enabled,
		NextRunAt:  value.NextRunAt,
		LastRunAt:  value.LastRunAt,
		InProgress: value.InProgress,
		CreatedAt:  value.CreatedAt,
		UpdatedAt:  value.UpdatedAt,
	}
}

func toInboxRecord(value store.AutomationInboxEntry) automationInboxEntry {
	return automationInboxEntry{
		ID:               value.ID,
		AutomationID:     value.AutomationID,
		RunID:            value.RunID,
		Status:           value.Status,
		Phase:            value.Phase,
		CompletionReason: value.CompletionReason,
		FinalResponse:    value.FinalResponse,
		TimedOut:         value.TimedOut,
		Error:            value.Error,
		Unread:           value.Unread,
		Trigger:          value.Trigger,
		StartedAt:        value.StartedAt,
		CompletedAt:      value.CompletedAt,
		Diagnostics:      decodeAutomationDiagnostics(value.Diagnostics),
	}
}

func toStoreInboxEntry(value automationInboxEntry) store.AutomationInboxEntry {
	return store.AutomationInboxEntry{
		ID:               value.ID,
		AutomationID:     value.AutomationID,
		RunID:            value.RunID,
		Status:           value.Status,
		Phase:            value.Phase,
		CompletionReason: value.CompletionReason,
		FinalResponse:    value.FinalResponse,
		TimedOut:         value.TimedOut,
		Error:            value.Error,
		Unread:           value.Unread,
		Trigger:          value.Trigger,
		StartedAt:        value.StartedAt,
		CompletedAt:      value.CompletedAt,
		Diagnostics:      encodeAutomationDiagnostics(value.Diagnostics),
	}
}

func decodeAutomationDiagnostics(value map[string]any) automationDiagnostics {
	if len(value) == 0 {
		return automationDiagnostics{Sources: []automationSourceDiagnostic{}}
	}
	diag := automationDiagnostics{}
	encoded, err := json.Marshal(value)
	if err != nil {
		return automationDiagnostics{Sources: []automationSourceDiagnostic{}}
	}
	if err := json.Unmarshal(encoded, &diag); err != nil {
		return automationDiagnostics{Sources: []automationSourceDiagnostic{}}
	}
	if diag.Sources == nil {
		diag.Sources = []automationSourceDiagnostic{}
	}
	return diag
}

func encodeAutomationDiagnostics(value automationDiagnostics) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"sources": []any{}}
	}
	payload := map[string]any{}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return map[string]any{"sources": []any{}}
	}
	return payload
}

func findInboxEntry(entries []store.AutomationInboxEntry, entryID string) *store.AutomationInboxEntry {
	for idx := range entries {
		if entries[idx].ID == entryID {
			copy := entries[idx]
			return &copy
		}
	}
	return nil
}

func summarizeInbox(entries []store.AutomationInboxEntry) (int, string) {
	unread := 0
	lastStatus := ""
	for idx, entry := range entries {
		if entry.Unread {
			unread++
		}
		if idx == 0 {
			lastStatus = strings.TrimSpace(entry.Status)
		}
	}
	return unread, lastStatus
}

func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	items, err := s.store.ListAutomations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]automationSchedule, 0, len(items))
	totalUnread := 0
	for _, item := range items {
		entries, inboxErr := s.store.ListAutomationInbox(r.Context(), item.ID)
		if inboxErr != nil {
			http.Error(w, inboxErr.Error(), http.StatusInternalServerError)
			return
		}
		record := toScheduleRecord(item)
		unread, lastStatus := summarizeInbox(entries)
		record.Unread = unread
		record.LastStatus = lastStatus
		totalUnread += unread
		if record.Enabled {
			if next, nextErr := computeNextRun(record.Days, record.TimeOfDay, record.Timezone, now); nextErr == nil {
				record.NextRunAt = next.Format(time.RFC3339)
			}
		} else {
			record.NextRunAt = ""
		}
		result = append(result, record)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].UpdatedAt > result[j].UpdatedAt
	})
	writeJSONStatus(w, automationsListResponse{Automations: result, UnreadCount: totalUnread}, http.StatusOK)
}

func (s *Server) createAutomation(w http.ResponseWriter, r *http.Request) {
	req := automationUpsertRequest{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	prompt := strings.TrimSpace(req.Prompt)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}
	timeOfDay, err := normalizeTimeOfDay(req.TimeOfDay)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now().UTC()
	schedule := automationSchedule{
		ID:        uuid.NewString(),
		Name:      name,
		Prompt:    prompt,
		Model:     strings.TrimSpace(req.Model),
		Days:      normalizeDays(req.Days),
		TimeOfDay: timeOfDay,
		Timezone:  normalizeTimezone(req.Timezone),
		Enabled:   enabled,
		CreatedAt: now.Format(time.RFC3339Nano),
		UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if enabled {
		if next, nextErr := computeNextRun(schedule.Days, schedule.TimeOfDay, schedule.Timezone, now); nextErr == nil {
			schedule.NextRunAt = next.Format(time.RFC3339Nano)
		}
	}
	if err := s.store.CreateAutomation(r.Context(), toStoreAutomation(schedule)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, schedule, http.StatusCreated)
}

func (s *Server) updateAutomation(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "automation id is required", http.StatusBadRequest)
		return
	}
	req := automationUpsertRequest{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	current, err := s.store.GetAutomation(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if current == nil {
		http.Error(w, "automation not found", http.StatusNotFound)
		return
	}
	updated := toScheduleRecord(*current)
	if value := strings.TrimSpace(req.Name); value != "" {
		updated.Name = value
	}
	if value := strings.TrimSpace(req.Prompt); value != "" {
		updated.Prompt = value
	}
	if value := strings.TrimSpace(req.Model); value != "" {
		updated.Model = value
	}
	if len(req.Days) > 0 {
		updated.Days = normalizeDays(req.Days)
	}
	if strings.TrimSpace(req.TimeOfDay) != "" {
		timeOfDay, err := normalizeTimeOfDay(req.TimeOfDay)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updated.TimeOfDay = timeOfDay
	}
	if strings.TrimSpace(req.Timezone) != "" {
		updated.Timezone = normalizeTimezone(req.Timezone)
	}
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
	}
	updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if updated.Enabled {
		if next, err := computeNextRun(updated.Days, updated.TimeOfDay, updated.Timezone, time.Now().UTC()); err == nil {
			updated.NextRunAt = next.Format(time.RFC3339Nano)
		}
	} else {
		updated.NextRunAt = ""
	}
	if err := s.store.UpdateAutomation(r.Context(), toStoreAutomation(updated)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries, err := s.store.ListAutomationInbox(r.Context(), id)
	if err == nil {
		updated.Unread, updated.LastStatus = summarizeInbox(entries)
	}
	writeJSONStatus(w, updated, http.StatusOK)
}

func (s *Server) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "automation id is required", http.StatusBadRequest)
		return
	}
	current, err := s.store.GetAutomation(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if current == nil {
		http.Error(w, "automation not found", http.StatusNotFound)
		return
	}
	if err := s.store.DeleteAutomation(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, map[string]any{"deleted": true}, http.StatusOK)
}

func (s *Server) getAutomationInbox(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "automation id is required", http.StatusBadRequest)
		return
	}
	automation, err := s.store.GetAutomation(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if automation == nil {
		http.Error(w, "automation not found", http.StatusNotFound)
		return
	}
	entries, err := s.store.ListAutomationInbox(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	record := toScheduleRecord(*automation)
	record.Unread, record.LastStatus = summarizeInbox(entries)
	if record.Enabled {
		if next, nextErr := computeNextRun(record.Days, record.TimeOfDay, record.Timezone, time.Now().UTC()); nextErr == nil {
			record.NextRunAt = next.Format(time.RFC3339)
		}
	}
	mapped := make([]automationInboxEntry, 0, len(entries))
	for _, entry := range entries {
		mapped = append(mapped, toInboxRecord(entry))
	}
	writeJSONStatus(w, automationDetailResponse{Automation: record, Inbox: mapped, UnreadCount: record.Unread}, http.StatusOK)
}

func (s *Server) markAutomationInboxRead(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	entryID := strings.TrimSpace(chi.URLParam(r, "entryID"))
	if id == "" || entryID == "" {
		http.Error(w, "automation id and entry id are required", http.StatusBadRequest)
		return
	}
	if err := s.store.MarkAutomationInboxEntryRead(r.Context(), id, entryID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, map[string]any{"ok": true}, http.StatusOK)
}

func (s *Server) markAutomationInboxReadAll(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "automation id is required", http.StatusBadRequest)
		return
	}
	if err := s.store.MarkAutomationInboxReadAll(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, map[string]any{"ok": true}, http.StatusOK)
}

func (s *Server) processDueAutomations(w http.ResponseWriter, r *http.Request) {
	queued := s.queueDueAutomations("schedule")
	writeJSONStatus(w, automationProcessResponse{Queued: queued}, http.StatusOK)
}

func (s *Server) runAutomationNow(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "automation id is required", http.StatusBadRequest)
		return
	}
	queued, reason := s.queueAutomationExecution(id, "manual")
	if !queued {
		writeJSONStatus(w, automationQueueResponse{Queued: false, Error: reason}, http.StatusConflict)
		return
	}
	writeJSONStatus(w, automationQueueResponse{Queued: true}, http.StatusAccepted)
}

func (s *Server) queueDueAutomations(trigger string) int {
	now := time.Now().UTC()
	items, err := s.store.ListAutomations(context.Background())
	if err != nil {
		return 0
	}
	candidates := make([]string, 0)
	for _, schedule := range items {
		if !schedule.Enabled || schedule.InProgress {
			continue
		}
		next, nextErr := computeNextRun(schedule.Days, schedule.TimeOfDay, schedule.Timezone, now.Add(-1*time.Second))
		if nextErr != nil {
			continue
		}
		schedule.NextRunAt = next.Format(time.RFC3339Nano)
		schedule.UpdatedAt = now.Format(time.RFC3339Nano)
		_ = s.store.UpdateAutomation(context.Background(), schedule)
		if !next.After(now) {
			candidates = append(candidates, schedule.ID)
		}
	}
	queued := 0
	for _, id := range candidates {
		ok, _ := s.queueAutomationExecution(id, trigger)
		if ok {
			queued++
		}
	}
	return queued
}

func (s *Server) queueAutomationExecution(id string, trigger string) (bool, string) {
	s.automationMu.Lock()
	defer s.automationMu.Unlock()

	schedule, err := s.store.GetAutomation(context.Background(), id)
	if err != nil {
		return false, "failed to load automation"
	}
	if schedule == nil {
		return false, "automation not found"
	}
	if schedule.InProgress {
		return false, "automation is already running"
	}
	if !schedule.Enabled {
		return false, "automation is disabled"
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	entry := store.AutomationInboxEntry{
		ID:           uuid.NewString(),
		AutomationID: id,
		Status:       "queued",
		Trigger:      trigger,
		Unread:       false,
		StartedAt:    now,
		CreatedAt:    now,
		UpdatedAt:    now,
		Diagnostics: map[string]any{
			"sources": []any{},
		},
	}
	schedule.InProgress = true
	schedule.UpdatedAt = now
	if err := s.store.UpdateAutomation(context.Background(), *schedule); err != nil {
		return false, "failed to update automation state"
	}
	if err := s.store.CreateAutomationInboxEntry(context.Background(), entry); err != nil {
		schedule.InProgress = false
		schedule.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		_ = s.store.UpdateAutomation(context.Background(), *schedule)
		return false, "failed to queue automation"
	}
	go s.executeAutomationJob(*schedule, entry.ID)
	return true, ""
}

func (s *Server) executeAutomationJob(schedule store.Automation, entryID string) {
	s.automationMu.Lock()
	entries, _ := s.store.ListAutomationInbox(context.Background(), schedule.ID)
	entry := findInboxEntry(entries, entryID)
	if entry != nil {
		entry.Status = "running"
		entry.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
		entry.UpdatedAt = entry.StartedAt
		_ = s.store.UpdateAutomationInboxEntry(context.Background(), *entry)
	}
	s.automationMu.Unlock()

	wait := true
	req := automationExecuteRequest{
		Prompt:            schedule.Prompt,
		ModelRoute:        strings.TrimSpace(schedule.Model),
		WaitForCompletion: &wait,
		Metadata: map[string]any{
			"automation_id":   schedule.ID,
			"automation_name": schedule.Name,
		},
	}
	if strings.TrimSpace(schedule.Model) != "" {
		req.Metadata["llm_model"] = strings.TrimSpace(schedule.Model)
	}

	response, err := s.invokeAutomationExecute(req)
	completedAt := time.Now().UTC()

	var (
		notifySchedule store.Automation
		notifyEntry    store.AutomationInboxEntry
		shouldNotify   bool
	)

	s.automationMu.Lock()
	current, loadErr := s.store.GetAutomation(context.Background(), schedule.ID)
	if loadErr != nil || current == nil {
		s.automationMu.Unlock()
		return
	}
	entries, _ = s.store.ListAutomationInbox(context.Background(), schedule.ID)
	inboxEntry := findInboxEntry(entries, entryID)
	if inboxEntry != nil {
		if err != nil {
			inboxEntry.Status = "failed"
			inboxEntry.Error = err.Error()
			inboxEntry.FinalResponse = ""
			inboxEntry.TimedOut = false
			inboxEntry.Diagnostics = map[string]any{"sources": []any{}}
		} else {
			inboxEntry.RunID = response.RunID
			inboxEntry.Status = response.Status
			inboxEntry.Phase = response.Phase
			inboxEntry.CompletionReason = response.CompletionReason
			inboxEntry.FinalResponse = response.FinalResponse
			inboxEntry.TimedOut = response.TimedOut
			inboxEntry.Diagnostics = encodeAutomationDiagnostics(response.Diagnostics)
		}
		inboxEntry.CompletedAt = completedAt.Format(time.RFC3339Nano)
		inboxEntry.Unread = true
		inboxEntry.UpdatedAt = inboxEntry.CompletedAt
		_ = s.store.UpdateAutomationInboxEntry(context.Background(), *inboxEntry)
		notifyEntry = *inboxEntry
		shouldNotify = true
	}

	current.InProgress = false
	current.LastRunAt = completedAt.Format(time.RFC3339Nano)
	current.UpdatedAt = completedAt.Format(time.RFC3339Nano)
	if current.Enabled {
		if next, nextErr := computeNextRun(current.Days, current.TimeOfDay, current.Timezone, completedAt); nextErr == nil {
			current.NextRunAt = next.Format(time.RFC3339Nano)
		}
	} else {
		current.NextRunAt = ""
	}
	_ = s.store.UpdateAutomation(context.Background(), *current)
	notifySchedule = *current
	s.automationMu.Unlock()

	if shouldNotify {
		if notifyErr := s.notifyDiscordAutomationCompletion(notifySchedule, notifyEntry); notifyErr != nil {
			log.Printf("automation notification failed automation_id=%s entry_id=%s err=%v", notifySchedule.ID, notifyEntry.ID, notifyErr)
		}
	}
}

func (s *Server) invokeAutomationExecute(req automationExecuteRequest) (automationExecuteResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return automationExecuteResponse{}, err
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/automation/execute", bytes.NewReader(payload))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.executeAutomationRun(recorder, httpReq)

	result := recorder.Result()
	defer result.Body.Close()
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		raw := strings.TrimSpace(recorder.Body.String())
		if raw == "" {
			raw = "automation execution failed"
		}
		return automationExecuteResponse{}, fmt.Errorf("automation execute failed (%d): %s", result.StatusCode, raw)
	}
	output := automationExecuteResponse{}
	if err := json.NewDecoder(result.Body).Decode(&output); err != nil {
		return automationExecuteResponse{}, err
	}
	return output, nil
}
