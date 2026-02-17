package api

import (
	"context"
	"strings"
	"time"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/events"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func (s *Server) upsertProcessesFromEvent(ctx context.Context, event store.RunEvent) error {
	processes := extractProcessesFromEvent(event)
	for _, process := range processes {
		if err := s.store.UpsertRunProcess(ctx, process); err != nil {
			return err
		}
	}
	return nil
}

func extractProcessesFromEvent(event store.RunEvent) []store.RunProcess {
	if event.Payload == nil {
		return nil
	}
	if events.NormalizeType(event.Type) != "tool.completed" {
		return nil
	}
	toolName := strings.TrimSpace(readString(event.Payload, "tool_name"))
	if toolName == "" || !strings.HasPrefix(toolName, "process.") {
		return nil
	}

	outputRaw, ok := event.Payload["output"]
	if !ok {
		return nil
	}
	outputMap, ok := outputRaw.(map[string]any)
	if !ok {
		return nil
	}

	if toolName == "process.list" {
		processesRaw, ok := outputMap["processes"].([]any)
		if !ok {
			return nil
		}
		results := make([]store.RunProcess, 0, len(processesRaw))
		for _, item := range processesRaw {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			process := processFromOutput(event.RunID, itemMap, event.Timestamp, toolName)
			if process.ProcessID == "" {
				continue
			}
			results = append(results, process)
		}
		return results
	}

	process := processFromOutput(event.RunID, outputMap, event.Timestamp, toolName)
	if process.ProcessID == "" {
		return nil
	}
	return []store.RunProcess{process}
}

func processFromOutput(runID string, output map[string]any, eventTimestamp string, toolName string) store.RunProcess {
	processID := strings.TrimSpace(readString(output, "process_id"))
	if processID == "" {
		return store.RunProcess{}
	}
	process := store.RunProcess{
		RunID:       runID,
		ProcessID:   processID,
		Command:     strings.TrimSpace(readString(output, "command")),
		Cwd:         strings.TrimSpace(readString(output, "cwd")),
		Status:      strings.TrimSpace(readString(output, "status")),
		PID:         int(readInt64(output, "pid")),
		StartedAt:   strings.TrimSpace(readString(output, "started_at")),
		EndedAt:     strings.TrimSpace(readString(output, "ended_at")),
		Signal:      strings.TrimSpace(readString(output, "signal")),
		PreviewURLs: readStringArray(output, "preview_urls"),
		Args:        readStringArray(output, "args"),
		Metadata: map[string]any{
			"tool_name": toolName,
		},
	}
	if process.StartedAt == "" {
		process.StartedAt = eventTimestampOrNow(eventTimestamp)
	}
	if status := strings.ToLower(process.Status); status == "exited" || status == "stopped" || status == "failed" {
		if process.EndedAt == "" {
			process.EndedAt = eventTimestampOrNow(eventTimestamp)
		}
	}
	if exitCode, ok := readOptionalInt(output, "exit_code"); ok {
		process.ExitCode = &exitCode
	}
	if process.Status == "" {
		process.Status = "running"
	}
	return process
}

func readOptionalInt(payload map[string]any, key string) (int, bool) {
	if payload == nil {
		return 0, false
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	default:
		return 0, false
	}
}

func readStringArray(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		values = append(values, trimmed)
	}
	return values
}

func eventTimestampOrNow(value string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}
