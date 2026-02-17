package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type MemoryStore struct {
	mu          sync.RWMutex
	runs        map[string]store.Run
	events      map[string][]store.RunEvent
	runSteps    map[string]map[string]store.RunStep
	processes   map[string]map[string]store.RunProcess
	messages    map[string][]store.Message
	seq         map[string]int64
	settings    *store.LLMSettings
	skills      map[string]store.Skill
	files       map[string]map[string]store.SkillFile
	context     map[string]store.ContextNode
	memory      *store.MemorySettings
	personality *store.PersonalitySettings
	entries     []store.MemoryEntry
	entryIndex  map[string]store.MemoryEntry
	artifacts   map[string]map[string]store.Artifact
	automations map[string]store.Automation
	inbox       map[string][]store.AutomationInboxEntry
}

func New() *MemoryStore {
	return &MemoryStore{
		runs:        map[string]store.Run{},
		events:      map[string][]store.RunEvent{},
		runSteps:    map[string]map[string]store.RunStep{},
		processes:   map[string]map[string]store.RunProcess{},
		messages:    map[string][]store.Message{},
		seq:         map[string]int64{},
		skills:      map[string]store.Skill{},
		files:       map[string]map[string]store.SkillFile{},
		context:     map[string]store.ContextNode{},
		entryIndex:  map[string]store.MemoryEntry{},
		artifacts:   map[string]map[string]store.Artifact{},
		automations: map[string]store.Automation{},
		inbox:       map[string][]store.AutomationInboxEntry{},
	}
}

func (m *MemoryStore) CreateRun(ctx context.Context, run store.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(run.Phase) == "" {
		run.Phase = "planning"
	}
	if strings.TrimSpace(run.PolicyProfile) == "" {
		run.PolicyProfile = "default"
	}
	m.runs[run.ID] = run
	return nil
}

func (m *MemoryStore) DeleteRun(ctx context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runs, runID)
	delete(m.events, runID)
	delete(m.runSteps, runID)
	delete(m.processes, runID)
	delete(m.messages, runID)
	delete(m.seq, runID)
	delete(m.artifacts, runID)
	return nil
}

func (m *MemoryStore) ListAutomations(ctx context.Context) ([]store.Automation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	results := make([]store.Automation, 0, len(m.automations))
	for _, automation := range m.automations {
		cloned := automation
		cloned.Days = append([]string{}, automation.Days...)
		results = append(results, cloned)
	}
	sort.Slice(results, func(i, j int) bool {
		return parseTime(results[i].UpdatedAt).After(parseTime(results[j].UpdatedAt))
	})
	return results, nil
}

func (m *MemoryStore) GetAutomation(ctx context.Context, automationID string) (*store.Automation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	automation, ok := m.automations[automationID]
	if !ok {
		return nil, nil
	}
	cloned := automation
	cloned.Days = append([]string{}, automation.Days...)
	return &cloned, nil
}

func (m *MemoryStore) CreateAutomation(ctx context.Context, automation store.Automation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cloned := automation
	cloned.Days = append([]string{}, automation.Days...)
	m.automations[automation.ID] = cloned
	if _, ok := m.inbox[automation.ID]; !ok {
		m.inbox[automation.ID] = []store.AutomationInboxEntry{}
	}
	return nil
}

func (m *MemoryStore) UpdateAutomation(ctx context.Context, automation store.Automation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.automations[automation.ID]; !ok {
		return nil
	}
	cloned := automation
	cloned.Days = append([]string{}, automation.Days...)
	m.automations[automation.ID] = cloned
	return nil
}

func (m *MemoryStore) DeleteAutomation(ctx context.Context, automationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.automations, automationID)
	delete(m.inbox, automationID)
	return nil
}

func (m *MemoryStore) ListAutomationInbox(ctx context.Context, automationID string) ([]store.AutomationInboxEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entries := m.inbox[automationID]
	if entries == nil {
		return []store.AutomationInboxEntry{}, nil
	}
	cloned := make([]store.AutomationInboxEntry, 0, len(entries))
	for _, entry := range entries {
		copy := entry
		copy.Diagnostics = cloneMap(entry.Diagnostics)
		cloned = append(cloned, copy)
	}
	sort.Slice(cloned, func(i, j int) bool {
		return parseTime(cloned[i].StartedAt).After(parseTime(cloned[j].StartedAt))
	})
	return cloned, nil
}

func (m *MemoryStore) CreateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := entry
	copy.Diagnostics = cloneMap(entry.Diagnostics)
	m.inbox[entry.AutomationID] = append([]store.AutomationInboxEntry{copy}, m.inbox[entry.AutomationID]...)
	return nil
}

func (m *MemoryStore) UpdateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.inbox[entry.AutomationID]
	for idx := range entries {
		if entries[idx].ID != entry.ID {
			continue
		}
		copy := entry
		copy.Diagnostics = cloneMap(entry.Diagnostics)
		entries[idx] = copy
		m.inbox[entry.AutomationID] = entries
		return nil
	}
	return nil
}

func (m *MemoryStore) MarkAutomationInboxEntryRead(ctx context.Context, automationID string, entryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.inbox[automationID]
	for idx := range entries {
		if entries[idx].ID == entryID {
			entries[idx].Unread = false
		}
	}
	m.inbox[automationID] = entries
	return nil
}

func (m *MemoryStore) MarkAutomationInboxReadAll(ctx context.Context, automationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.inbox[automationID]
	for idx := range entries {
		entries[idx].Unread = false
	}
	m.inbox[automationID] = entries
	return nil
}

func (m *MemoryStore) ListRuns(ctx context.Context) ([]store.RunSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	results := make([]store.RunSummary, 0, len(m.runs))
	for _, run := range m.runs {
		summary := store.RunSummary{
			ID:               run.ID,
			Status:           run.Status,
			Phase:            run.Phase,
			CompletionReason: run.CompletionReason,
			ResumedFrom:      run.ResumedFrom,
			CheckpointSeq:    run.CheckpointSeq,
			PolicyProfile:    run.PolicyProfile,
			ModelRoute:       run.ModelRoute,
			Tags:             append([]string{}, run.Tags...),
			CreatedAt:        run.CreatedAt,
			UpdatedAt:        run.UpdatedAt,
		}
		if events := m.events[run.ID]; len(events) > 0 {
			for i := len(events) - 1; i >= 0; i-- {
				if events[i].Type != "run.title.updated" {
					continue
				}
				if title := readString(events[i].Payload, "title"); title != "" {
					summary.Title = title
					break
				}
			}
		}
		if messages := m.messages[run.ID]; len(messages) > 0 {
			summary.MessageCount = int64(len(messages))
			if strings.TrimSpace(summary.Title) == "" {
				for _, msg := range messages {
					if msg.Role == "user" {
						summary.Title = msg.Content
						break
					}
				}
			}
		}
		if events := m.events[run.ID]; len(events) > 0 {
			last := events[len(events)-1]
			if last.Timestamp != "" {
				summary.UpdatedAt = last.Timestamp
			}
			if status := statusFromEvent(last.Type); status != "" {
				summary.Status = status
			}
		}
		results = append(results, summary)
	}
	sort.Slice(results, func(i, j int) bool {
		left := parseTime(results[i].UpdatedAt)
		right := parseTime(results[j].UpdatedAt)
		return left.After(right)
	})
	return results, nil
}

func (m *MemoryStore) AddMessage(ctx context.Context, msg store.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[msg.RunID] = append(m.messages[msg.RunID], msg)
	return nil
}

func (m *MemoryStore) ListMessages(ctx context.Context, runID string) ([]store.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	messages := m.messages[runID]
	return append([]store.Message{}, messages...), nil
}

func (m *MemoryStore) GetLLMSettings(ctx context.Context) (*store.LLMSettings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.settings == nil {
		return nil, nil
	}
	copy := *m.settings
	return &copy, nil
}

func (m *MemoryStore) UpsertLLMSettings(ctx context.Context, settings store.LLMSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := settings
	m.settings = &copy
	return nil
}

func (m *MemoryStore) ListSkills(ctx context.Context) ([]store.Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	results := make([]store.Skill, 0, len(m.skills))
	for _, skill := range m.skills {
		results = append(results, skill)
	}
	return results, nil
}

func (m *MemoryStore) GetSkill(ctx context.Context, skillID string) (*store.Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	skill, ok := m.skills[skillID]
	if !ok {
		return nil, nil
	}
	copy := skill
	return &copy, nil
}

func (m *MemoryStore) CreateSkill(ctx context.Context, skill store.Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills[skill.ID] = skill
	return nil
}

func (m *MemoryStore) UpdateSkill(ctx context.Context, skill store.Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skills[skill.ID] = skill
	return nil
}

func (m *MemoryStore) DeleteSkill(ctx context.Context, skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.skills, skillID)
	delete(m.files, skillID)
	return nil
}

func (m *MemoryStore) ListSkillFiles(ctx context.Context, skillID string) ([]store.SkillFile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	files := m.files[skillID]
	if files == nil {
		return []store.SkillFile{}, nil
	}
	results := make([]store.SkillFile, 0, len(files))
	for _, file := range files {
		results = append(results, file)
	}
	return results, nil
}

func (m *MemoryStore) UpsertSkillFile(ctx context.Context, file store.SkillFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files[file.SkillID] == nil {
		m.files[file.SkillID] = map[string]store.SkillFile{}
	}
	m.files[file.SkillID][file.Path] = file
	return nil
}

func (m *MemoryStore) DeleteSkillFile(ctx context.Context, skillID string, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	files := m.files[skillID]
	if files == nil {
		return nil
	}
	delete(files, path)
	return nil
}

func (m *MemoryStore) ListContextNodes(ctx context.Context) ([]store.ContextNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	results := make([]store.ContextNode, 0, len(m.context))
	for _, node := range m.context {
		results = append(results, node)
	}
	return results, nil
}

func (m *MemoryStore) GetContextFile(ctx context.Context, nodeID string) (*store.ContextNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node, ok := m.context[nodeID]
	if !ok {
		return nil, nil
	}
	copy := node
	return &copy, nil
}

func (m *MemoryStore) CreateContextFolder(ctx context.Context, node store.ContextNode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.context[node.ID] = node
	return nil
}

func (m *MemoryStore) CreateContextFile(ctx context.Context, node store.ContextNode) error {
	return m.CreateContextFolder(ctx, node)
}

func (m *MemoryStore) DeleteContextNode(ctx context.Context, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteContextSubtree(nodeID)
	return nil
}

func (m *MemoryStore) GetMemorySettings(ctx context.Context) (*store.MemorySettings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.memory == nil {
		return nil, nil
	}
	copy := *m.memory
	return &copy, nil
}

func (m *MemoryStore) UpsertMemorySettings(ctx context.Context, settings store.MemorySettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := settings
	m.memory = &copy
	return nil
}

func (m *MemoryStore) UpsertMemoryEntry(ctx context.Context, entry store.MemoryEntry) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fingerprint := ""
	if entry.Metadata != nil {
		if value, ok := entry.Metadata["fingerprint"]; ok {
			if key, ok := value.(string); ok {
				fingerprint = strings.TrimSpace(key)
			}
		}
	}
	if fingerprint != "" {
		if _, exists := m.entryIndex[fingerprint]; exists {
			return false, nil
		}
		m.entryIndex[fingerprint] = entry
	}
	m.entries = append(m.entries, entry)
	return true, nil
}

func (m *MemoryStore) GetPersonalitySettings(ctx context.Context) (*store.PersonalitySettings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.personality == nil {
		return nil, nil
	}
	copy := *m.personality
	return &copy, nil
}

func (m *MemoryStore) UpsertPersonalitySettings(ctx context.Context, settings store.PersonalitySettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := settings
	m.personality = &copy
	return nil
}

func (m *MemoryStore) SearchMemory(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if query == "" || limit <= 0 {
		return []store.MemoryEntry{}, nil
	}
	results := []store.MemoryEntry{}
	for _, entry := range m.entries {
		if len(results) >= limit {
			break
		}
		if strings.Contains(strings.ToLower(entry.Content), strings.ToLower(query)) {
			results = append(results, entry)
		}
	}
	return results, nil
}

func (m *MemoryStore) SearchMemoryWithEmbedding(ctx context.Context, query string, embedding []float32, limit int) ([]store.MemoryEntry, error) {
	return m.SearchMemory(ctx, query, limit)
}

func (m *MemoryStore) deleteContextSubtree(nodeID string) {
	for id, node := range m.context {
		if node.ParentID == nodeID {
			m.deleteContextSubtree(id)
		}
	}
	delete(m.context, nodeID)
}

func (m *MemoryStore) AppendEvent(ctx context.Context, event store.RunEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	event.Type = normalizeEventType(event.Type)
	m.events[event.RunID] = append(m.events[event.RunID], event)
	m.applyRunStepLocked(event)
	m.applyRunStateLocked(event)
	return nil
}

func normalizeEventType(eventType string) string {
	normalized := strings.TrimSpace(strings.ToLower(eventType))
	if normalized == "" {
		return ""
	}
	return strings.ReplaceAll(normalized, "_", ".")
}

func (m *MemoryStore) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := m.events[runID]
	if afterSeq <= 0 {
		return append([]store.RunEvent{}, events...), nil
	}
	filtered := []store.RunEvent{}
	for _, event := range events {
		if event.Seq > afterSeq {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

func (m *MemoryStore) NextSeq(ctx context.Context, runID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq[runID] += 1
	return m.seq[runID], nil
}

func (m *MemoryStore) ListRunSteps(ctx context.Context, runID string) ([]store.RunStep, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stepsByID := m.runSteps[runID]
	if len(stepsByID) == 0 {
		return []store.RunStep{}, nil
	}

	steps := make([]store.RunStep, 0, len(stepsByID))
	for _, step := range stepsByID {
		steps = append(steps, step)
	}
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].Seq == steps[j].Seq {
			return steps[i].ID < steps[j].ID
		}
		if steps[i].Seq == 0 {
			return false
		}
		if steps[j].Seq == 0 {
			return true
		}
		return steps[i].Seq < steps[j].Seq
	})
	return steps, nil
}

func (m *MemoryStore) UpsertRunProcess(ctx context.Context, process store.RunProcess) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if process.ProcessID == "" {
		return fmt.Errorf("process id required")
	}
	if m.processes[process.RunID] == nil {
		m.processes[process.RunID] = map[string]store.RunProcess{}
	}
	existing, ok := m.processes[process.RunID][process.ProcessID]
	if !ok {
		if process.Metadata == nil {
			process.Metadata = map[string]any{}
		}
		m.processes[process.RunID][process.ProcessID] = process
		return nil
	}
	if existing.Command == "" {
		existing.Command = process.Command
	}
	if len(existing.Args) == 0 && len(process.Args) > 0 {
		existing.Args = append([]string{}, process.Args...)
	}
	if existing.Cwd == "" {
		existing.Cwd = process.Cwd
	}
	if process.Status != "" {
		existing.Status = process.Status
	}
	if process.PID > 0 {
		existing.PID = process.PID
	}
	if existing.StartedAt == "" && process.StartedAt != "" {
		existing.StartedAt = process.StartedAt
	}
	if process.EndedAt != "" {
		existing.EndedAt = process.EndedAt
	}
	if process.ExitCode != nil {
		code := *process.ExitCode
		existing.ExitCode = &code
	}
	if process.Signal != "" {
		existing.Signal = process.Signal
	}
	if len(process.PreviewURLs) > 0 {
		existing.PreviewURLs = dedupeStrings(append(existing.PreviewURLs, process.PreviewURLs...))
	}
	if existing.Metadata == nil {
		existing.Metadata = map[string]any{}
	}
	for key, value := range process.Metadata {
		existing.Metadata[key] = value
	}
	m.processes[process.RunID][process.ProcessID] = existing
	return nil
}

func (m *MemoryStore) GetRunProcess(ctx context.Context, runID string, processID string) (*store.RunProcess, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byRun := m.processes[runID]
	if byRun == nil {
		return nil, nil
	}
	process, ok := byRun[processID]
	if !ok {
		return nil, nil
	}
	copy := process
	return &copy, nil
}

func (m *MemoryStore) ListRunProcesses(ctx context.Context, runID string) ([]store.RunProcess, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byRun := m.processes[runID]
	if byRun == nil {
		return []store.RunProcess{}, nil
	}
	processes := make([]store.RunProcess, 0, len(byRun))
	for _, process := range byRun {
		processes = append(processes, process)
	}
	sort.Slice(processes, func(i, j int) bool {
		left := parseTime(processes[i].StartedAt)
		right := parseTime(processes[j].StartedAt)
		if left.Equal(right) {
			return processes[i].ProcessID < processes[j].ProcessID
		}
		return left.After(right)
	})
	return processes, nil
}

func (m *MemoryStore) UpsertArtifact(ctx context.Context, artifact store.Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if artifact.ID == "" {
		return fmt.Errorf("artifact id required")
	}
	if m.artifacts[artifact.RunID] == nil {
		m.artifacts[artifact.RunID] = map[string]store.Artifact{}
	}
	m.artifacts[artifact.RunID][artifact.ID] = artifact
	return nil
}

func (m *MemoryStore) ListArtifacts(ctx context.Context, runID string) ([]store.Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	artifacts := m.artifacts[runID]
	if artifacts == nil {
		return []store.Artifact{}, nil
	}
	results := make([]store.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		results = append(results, artifact)
	}
	sort.Slice(results, func(i, j int) bool {
		left := parseTime(results[i].CreatedAt)
		right := parseTime(results[j].CreatedAt)
		if left.Equal(right) {
			return results[i].ID < results[j].ID
		}
		return left.Before(right)
	})
	return results, nil
}

func statusFromEvent(eventType string) string {
	switch eventType {
	case "run.completed":
		return "completed"
	case "run.failed":
		return "failed"
	case "run.cancelled":
		return "cancelled"
	case "run.partial":
		return "partial"
	case "run.started":
		return "running"
	default:
		return ""
	}
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func readString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	raw, ok := metadata[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (m *MemoryStore) applyRunStepLocked(event store.RunEvent) {
	step, ok := store.BuildRunStepFromEvent(event)
	if !ok {
		return
	}
	if m.runSteps[event.RunID] == nil {
		m.runSteps[event.RunID] = map[string]store.RunStep{}
	}
	existing, exists := m.runSteps[event.RunID][step.ID]
	if !exists {
		m.runSteps[event.RunID][step.ID] = step
		return
	}
	m.runSteps[event.RunID][step.ID] = store.MergeRunStep(existing, step)
}

func (m *MemoryStore) applyRunStateLocked(event store.RunEvent) {
	run, ok := m.runs[event.RunID]
	if !ok {
		return
	}
	eventType := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(event.Type)), "_", ".")
	switch eventType {
	case "run.started":
		run.Status = "running"
		run.Phase = "planning"
	case "run.phase.changed":
		if phase := readString(event.Payload, "phase"); phase != "" {
			run.Phase = phase
		}
	case "run.completed":
		run.Status = "completed"
		run.Phase = "completed"
		run.CompletionReason = readString(event.Payload, "completion_reason")
	case "run.partial":
		run.Status = "partial"
		run.Phase = "completed"
		run.CompletionReason = readString(event.Payload, "completion_reason")
	case "run.failed":
		run.Status = "failed"
		run.Phase = "failed"
		if reason := readString(event.Payload, "completion_reason"); reason != "" {
			run.CompletionReason = reason
		} else {
			run.CompletionReason = "activity_error"
		}
	case "run.cancelled":
		run.Status = "cancelled"
		run.Phase = "cancelled"
		run.CompletionReason = "user_cancelled"
	case "run.resumed":
		run.Status = "running"
		run.Phase = "planning"
		if resumedFrom := readString(event.Payload, "resumed_from"); resumedFrom != "" {
			run.ResumedFrom = resumedFrom
		}
	}
	if event.Seq > run.CheckpointSeq {
		run.CheckpointSeq = event.Seq
	}
	if strings.TrimSpace(event.Timestamp) != "" {
		run.UpdatedAt = event.Timestamp
	}
	m.runs[event.RunID] = run
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
