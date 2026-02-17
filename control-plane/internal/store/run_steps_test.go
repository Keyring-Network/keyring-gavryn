package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildRunStepFromEvent_StepLifecycle(t *testing.T) {
	planned, ok := BuildRunStepFromEvent(RunEvent{
		RunID:  "run-1",
		Seq:    0,
		Type:   "step.planned",
		Source: "llm",
		Payload: map[string]any{
			"step_id": "assistant_reply",
			"name":    "Generate assistant reply",
		},
	})
	require.True(t, ok)
	require.Equal(t, "pending", planned.Status)

	started, ok := BuildRunStepFromEvent(RunEvent{
		RunID:     "run-1",
		Seq:       1,
		Type:      "step.started",
		Timestamp: "2026-02-07T00:00:00Z",
		Source:    "llm",
		Payload: map[string]any{
			"step_id": "assistant_reply",
			"name":    "Generate assistant reply",
		},
	})
	require.True(t, ok)
	started = MergeRunStep(planned, started)
	require.Equal(t, "assistant_reply", started.ID)
	require.Equal(t, "running", started.Status)
	require.Equal(t, "step", started.Kind)

	failed, ok := BuildRunStepFromEvent(RunEvent{
		RunID:     "run-1",
		Seq:       2,
		Type:      "step.failed",
		Timestamp: "2026-02-07T00:00:01Z",
		Source:    "llm",
		Payload: map[string]any{
			"step_id": "assistant_reply",
			"error":   "timeout",
		},
	})
	require.True(t, ok)
	merged := MergeRunStep(started, failed)
	require.Equal(t, "failed", merged.Status)
	require.Equal(t, "timeout", merged.Error)
	require.Equal(t, int64(1), merged.Seq)
	require.Equal(t, "2026-02-07T00:00:00Z", merged.StartedAt)
	require.Equal(t, "2026-02-07T00:00:01Z", merged.CompletedAt)
}

func TestBuildRunStepFromEvent_ToolAliases(t *testing.T) {
	step, ok := BuildRunStepFromEvent(RunEvent{
		RunID:     "run-1",
		Seq:       3,
		Type:      "tool.completed",
		Timestamp: "2026-02-07T00:00:02Z",
		Source:    "tool_runner",
		Payload: map[string]any{
			"invocation_id": "tool-1",
			"tool_name":     "editor.write",
		},
	})
	require.True(t, ok)
	require.Equal(t, "tool-1", step.ID)
	require.Equal(t, "tool", step.Kind)
	require.Equal(t, "completed", step.Status)
	require.Equal(t, "2026-02-07T00:00:02Z", step.CompletedAt)
}

func TestBuildRunStepFromEvent_IgnoresNonStepEvents(t *testing.T) {
	_, ok := BuildRunStepFromEvent(RunEvent{Type: "run.started"})
	require.False(t, ok)
}
