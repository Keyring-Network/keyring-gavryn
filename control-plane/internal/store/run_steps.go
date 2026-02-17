package store

import (
	"fmt"
	"strings"
)

func BuildRunStepFromEvent(event RunEvent) (RunStep, bool) {
	eventType := normalizeEventType(event.Type)
	switch eventType {
	case "step.planned":
		stepID := firstString(event.Payload, "step_id")
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", event.Seq)
		}
		name := firstString(event.Payload, "name")
		if name == "" {
			name = stepID
		}
		step := RunStep{
			RunID:             event.RunID,
			ID:                stepID,
			ParentStepID:      firstString(event.Payload, "parent_step_id"),
			Name:              name,
			Kind:              "step",
			Status:            "pending",
			PlanID:            firstString(event.Payload, "plan_id"),
			Attempt:           firstInt(event.Payload, "attempt"),
			PolicyDecision:    firstString(event.Payload, "policy_decision"),
			Source:            event.Source,
			Seq:               event.Seq,
			Dependencies:      readStringSlice(event.Payload, "dependencies"),
			ExpectedArtifacts: readStringSlice(event.Payload, "expected_artifacts"),
		}
		step.Diagnostics = buildDiagnostics(event, step)
		return step, true
	case "step.started":
		stepID := firstString(event.Payload, "step_id")
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", event.Seq)
		}
		name := firstString(event.Payload, "name")
		if name == "" {
			name = stepID
		}
		step := RunStep{
			RunID:             event.RunID,
			ID:                stepID,
			ParentStepID:      firstString(event.Payload, "parent_step_id"),
			Name:              name,
			Kind:              "step",
			Status:            "running",
			PlanID:            firstString(event.Payload, "plan_id"),
			Attempt:           firstInt(event.Payload, "attempt"),
			PolicyDecision:    firstString(event.Payload, "policy_decision"),
			Source:            event.Source,
			Seq:               event.Seq,
			StartedAt:         event.Timestamp,
			Dependencies:      readStringSlice(event.Payload, "dependencies"),
			ExpectedArtifacts: readStringSlice(event.Payload, "expected_artifacts"),
		}
		step.Diagnostics = buildDiagnostics(event, step)
		return step, true
	case "step.completed", "step.failed":
		stepID := firstString(event.Payload, "step_id")
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", event.Seq)
		}
		name := firstString(event.Payload, "name")
		if name == "" {
			name = stepID
		}
		status := "completed"
		if eventType == "step.failed" {
			status = "failed"
		}
		step := RunStep{
			RunID:             event.RunID,
			ID:                stepID,
			ParentStepID:      firstString(event.Payload, "parent_step_id"),
			Name:              name,
			Kind:              "step",
			Status:            status,
			PlanID:            firstString(event.Payload, "plan_id"),
			Attempt:           firstInt(event.Payload, "attempt"),
			PolicyDecision:    firstString(event.Payload, "policy_decision"),
			Source:            event.Source,
			Seq:               event.Seq,
			CompletedAt:       event.Timestamp,
			Error:             firstString(event.Payload, "error"),
			Dependencies:      readStringSlice(event.Payload, "dependencies"),
			ExpectedArtifacts: readStringSlice(event.Payload, "expected_artifacts"),
		}
		step.Diagnostics = buildDiagnostics(event, step)
		return step, true
	case "tool.started", "tool.completed", "tool.failed":
		toolID := firstString(event.Payload, "invocation_id", "tool_call_id")
		if toolID == "" {
			toolID = fmt.Sprintf("tool-%d", event.Seq)
		}
		toolName := firstString(event.Payload, "tool_name")
		if toolName == "" {
			toolName = eventType
		}
		status := "running"
		switch eventType {
		case "tool.completed":
			status = "completed"
		case "tool.failed":
			status = "failed"
		}
		step := RunStep{
			RunID:          event.RunID,
			ID:             toolID,
			Name:           toolName,
			Kind:           "tool",
			Status:         status,
			PlanID:         firstString(event.Payload, "plan_id"),
			Attempt:        firstInt(event.Payload, "attempt"),
			PolicyDecision: firstString(event.Payload, "policy_decision"),
			Source:         event.Source,
			Seq:            event.Seq,
			Error:          firstString(event.Payload, "error"),
			Diagnostics:    map[string]any{},
			Dependencies:   readStringSlice(event.Payload, "dependencies"),
		}
		if status == "running" {
			step.StartedAt = event.Timestamp
		} else {
			step.CompletedAt = event.Timestamp
		}
		step.Diagnostics = buildDiagnostics(event, step)
		return step, true
	default:
		return RunStep{}, false
	}
}

func MergeRunStep(existing RunStep, incoming RunStep) RunStep {
	merged := existing

	if merged.RunID == "" {
		merged.RunID = incoming.RunID
	}
	if merged.ID == "" {
		merged.ID = incoming.ID
	}
	if merged.ParentStepID == "" {
		merged.ParentStepID = incoming.ParentStepID
	}
	if merged.Name == "" {
		merged.Name = incoming.Name
	}
	if merged.Kind == "" {
		merged.Kind = incoming.Kind
	}
	if merged.Source == "" {
		merged.Source = incoming.Source
	}
	if incoming.Source != "" {
		merged.Source = incoming.Source
	}
	if incoming.Status != "" {
		merged.Status = incoming.Status
	}
	if merged.PlanID == "" {
		merged.PlanID = incoming.PlanID
	}
	if incoming.Attempt > 0 {
		merged.Attempt = incoming.Attempt
	}
	if incoming.PolicyDecision != "" {
		merged.PolicyDecision = incoming.PolicyDecision
	}
	if merged.Seq == 0 || (incoming.Seq > 0 && incoming.Seq < merged.Seq) {
		merged.Seq = incoming.Seq
	}
	if merged.StartedAt == "" && incoming.StartedAt != "" {
		merged.StartedAt = incoming.StartedAt
	}
	if incoming.CompletedAt != "" {
		merged.CompletedAt = incoming.CompletedAt
	}
	if incoming.Error != "" {
		merged.Error = incoming.Error
	}
	if len(merged.Dependencies) == 0 && len(incoming.Dependencies) > 0 {
		merged.Dependencies = append([]string{}, incoming.Dependencies...)
	}
	if len(merged.ExpectedArtifacts) == 0 && len(incoming.ExpectedArtifacts) > 0 {
		merged.ExpectedArtifacts = append([]string{}, incoming.ExpectedArtifacts...)
	}
	if merged.Diagnostics == nil {
		merged.Diagnostics = map[string]any{}
	}
	for key, value := range incoming.Diagnostics {
		merged.Diagnostics[key] = value
	}
	if merged.Name == "" {
		merged.Name = merged.ID
	}
	if merged.Kind == "" {
		merged.Kind = "step"
	}
	if merged.Status == "" {
		merged.Status = "running"
	}
	return merged
}

func normalizeEventType(eventType string) string {
	normalized := strings.TrimSpace(strings.ToLower(eventType))
	switch normalized {
	case "step_started":
		return "step.started"
	case "step.planned", "step_planned":
		return "step.planned"
	case "step_completed":
		return "step.completed"
	case "step_failed":
		return "step.failed"
	case "tool_started":
		return "tool.started"
	case "tool_output":
		return "tool.completed"
	case "tool_error":
		return "tool.failed"
	default:
		return strings.ReplaceAll(normalized, "_", ".")
	}
}

func firstString(payload map[string]any, keys ...string) string {
	if payload == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func firstInt(payload map[string]any, keys ...string) int {
	if payload == nil {
		return 0
	}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		}
	}
	return 0
}

func readStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	results := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		results = append(results, trimmed)
	}
	if len(results) == 0 {
		return nil
	}
	return results
}

func buildDiagnostics(event RunEvent, step RunStep) map[string]any {
	diagnostics := map[string]any{}
	for key, value := range event.Payload {
		diagnostics[key] = value
	}
	diagnostics["source"] = event.Source
	diagnostics["seq"] = event.Seq
	diagnostics["kind"] = step.Kind
	if step.Error != "" {
		diagnostics["error"] = step.Error
	}
	if event.TraceID != "" {
		diagnostics["trace_id"] = event.TraceID
	}
	return diagnostics
}
