# Workflows and Events

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Workflows: `control-plane/internal/workflows/`
- Activities: `control-plane/internal/workflows/activities.go`
- Event broker: `control-plane/internal/events/`
- API events: `control-plane/internal/api/server.go`

---

## Overview

Gavryn Local uses **Temporal** for workflow orchestration and **Server-Sent Events (SSE)** for real-time communication. This combination provides durable execution with immediate UI feedback.

---

## Temporal Workflows

### Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                     TEMPORAL SERVER                          │
│                     (localhost:7233)                         │
│                                                              │
│  ┌─────────────────┐    ┌─────────────────────────────┐     │
│  │   WORKFLOWS     │    │          TASK QUEUE          │     │
│  │                 │    │                              │     │
│  │ RunWorkflow     │───▶│ gavryn-runs-{port}          │     │
│  │                 │    │ (isolated per dev instance)  │     │
│  └─────────────────┘    └─────────────────────────────┘     │
│           │                                                  │
│           │ Executes                                         │
│           ▼                                                  │
│  ┌─────────────────┐                                         │
│  │    ACTIVITIES   │                                         │
│  │                 │                                         │
│  │ GenerateAssistant│                                        │
│  │ Reply            │                                        │
│  │                 │                                         │
│  └─────────────────┘                                         │
└──────────────────────────────────────────────────────────────┘
```

### RunWorkflow

The main workflow that handles conversation runs.

**File**: `control-plane/internal/workflows/run_workflow.go`

```go
func RunWorkflow(ctx workflow.Context, input RunInput) (RunResult, error)
```

**Behavior**:
- Runs indefinitely until cancelled
- Listens for message signals
- Executes `GenerateAssistantReply` activity for each message
- Handles graceful shutdown on cancellation

**Input**:
```go
type RunInput struct {
    RunID   string
    Message string
}
```

**Signals**:
- `MessageSignalName` - Triggered when user sends message

### Workflow Service

**File**: `control-plane/internal/workflows/service.go`

Provides workflow management operations:

| Method | Description |
|--------|-------------|
| `StartRun(ctx, runID)` | Start new workflow instance |
| `SignalMessage(ctx, runID, message)` | Send message to running workflow |
| `CancelRun(ctx, runID)` | Cancel workflow execution |

### Task Queue Isolation

**Recent Change (Feb 2026)**: Task queues are now isolated per control-plane port.

```go
// In config
TemporalTaskQueue: getEnv("TEMPORAL_TASK_QUEUE", "gavryn-runs")

// In dev.sh - dynamic assignment
TEMPORAL_TASK_QUEUE="gavryn-runs-${CONTROL_PLANE_PORT}"
```

This prevents cross-talk between multiple dev instances.

---

## Activities

### GenerateAssistantReply

**File**: `control-plane/internal/workflows/activities.go`

The main activity that generates AI responses.

```go
func (a *RunActivities) GenerateAssistantReply(ctx context.Context, input GenerateInput) error
```

**Process**:
1. Load conversation history from store
2. Resolve LLM configuration (per-message overrides supported)
3. Build system prompt (personality + context)
4. Build memory prompt (if memory enabled)
5. Call LLM provider
6. Stream response via events
7. Save assistant message to store

**Prompt Assembly**:

```
System Prompt (assembled from):
├── Personality settings (if configured)
├── Current date/time context
├── Available tools list
└── Gavryn identity

Memory Prompt (if enabled):
├── Relevant past conversations (vector search)
└── Recent context (FTS)

Full Context:
├── System prompt
├── Memory prompt (optional)
├── User/assistant message history
└── Current user message
```

### Activity Configuration

**Timeouts**:
```go
activityOptions := workflow.ActivityOptions{
    StartToCloseTimeout: 5 * time.Minute,
    RetryPolicy: &temporal.RetryPolicy{
        MaximumAttempts: 1,  // No automatic retry
    },
}
```

**Note**: Activities do not retry on failure. Errors are logged and emitted as events.

---

## Event System

### Event Broker

**File**: `control-plane/internal/events/broker.go`

In-memory pub/sub for SSE streaming:

```go
type Broker interface {
    Publish(event RunEvent)
    Subscribe(ctx context.Context, runID string) <-chan RunEvent
}
```

**Characteristics**:
- In-memory (not persisted)
- Per-run subscriptions
- Automatic cleanup on client disconnect

### Event Types

**Core Events**:

| Type | Source | Description |
|------|--------|-------------|
| `assistant_message_delta` | Activity | Streaming response chunk |
| `assistant_message_complete` | Activity | Response finished |
| `tool_started` | Worker | Tool execution began |
| `tool_completed` | Worker | Tool execution finished |
| `tool_failed` | Worker | Tool execution error |
| `artifact_created` | Worker | New artifact generated |
| `run_failed` | Any | Error during execution |
| `run_completed` | Workflow | Run finished |

**Event Structure**:

```go
type RunEvent struct {
    RunID     string         // UUID of the run
    Seq       int64          // Sequence number
    Type      string         // Event type
    Timestamp string         // ISO 8601 timestamp
    Source    string         // Component name (e.g., "tool_runner")
    Payload   map[string]any // Event-specific data
}
```

### Event Sequences

Each run has a monotonically increasing sequence counter:

```sql
CREATE TABLE run_event_sequences (
  run_id UUID PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
  last_seq BIGINT NOT NULL DEFAULT 0
);
```

**Usage**:
- Ensures event ordering
- Supports replay from specific point
- Used for deduplication

### Event Ingestion

Workers emit events via HTTP POST:

```http
POST /runs/{id}/events
Content-Type: application/json

{
  "type": "tool_completed",
  "source": "tool_runner",
  "payload": { ... }
}
```

Control plane:
1. Receives event via API
2. Assigns next sequence number
3. Persists to database
4. Publishes to broker
5. SSE clients receive update

---

## Server-Sent Events (SSE)

### Endpoint

```http
GET /runs/{id}/events
Accept: text/event-stream
```

**Protocol**: Server-Sent Events (text/event-stream)

### Event Stream Format

```
event: message
data: {"type": "assistant_message_delta", "payload": {"delta": "Hello"}, "timestamp": "2026-01-01T12:00:00Z", "seq": 1}

event: message
data: {"type": "assistant_message_delta", "payload": {"delta": " world"}, "timestamp": "2026-01-01T12:00:01Z", "seq": 2}

```

### Client Implementation

**JavaScript**:
```javascript
const eventSource = new EventSource(`/runs/${runId}/events`);

eventSource.onmessage = (event) => {
  const data = JSON.parse(event.data);
  
  switch (data.type) {
    case 'assistant_message_delta':
      appendToMessage(data.payload.delta);
      break;
    case 'tool_started':
      showToolIndicator(data.payload.tool_name);
      break;
    case 'tool_completed':
      displayToolResult(data.payload.result);
      break;
    case 'run_failed':
      showError(data.payload.error);
      break;
  }
};

eventSource.onerror = (error) => {
  console.error('SSE error:', error);
  // Auto-reconnect happens automatically
};

// Cleanup on component unmount
window.addEventListener('beforeunload', () => {
  eventSource.close();
});
```

### Reconnection Behavior

- SSE automatically reconnects on connection loss
- Client receives events from last acknowledged sequence
- Duplicate events are handled client-side

---

## Event Flow Examples

### Simple Chat Flow

```
User sends message
    │
    ▼
POST /runs/{id}/messages ──▶ Control Plane
    │                            │
    │                            ▼
    │                    Signal Workflow (Temporal)
    │                            │
    │                            ▼
    │                    Activity: GenerateAssistantReply
    │                            │
    │                            ▼
    │                    Call LLM Provider
    │                            │
    │                            ▼
    │◀───────────────── Streaming Response
    │                            │
    │                    Emit SSE Events (per chunk)
    │                            │
    ▼                            ▼
Frontend ◀─────────────────── SSE Stream
    │
    ▼
Display streaming message
```

### Tool Execution Flow

```
Assistant decides to use tool
    │
    ▼
LLM Response includes tool call
    │
    ▼
Activity parses tool request
    │
    ▼
Emit: tool_started event
    │
    ▼
POST /execute to Tool Runner
    │
    ▼
Tool Runner executes tool
    │
    ▼
Emit: tool_completed (or tool_failed)
    │
    ▼
Continue LLM conversation with tool result
```

---

## Error Handling

### Workflow Errors

Workflows catch activity errors and emit events:

```go
if err != nil {
    logger.Error("llm activity failed", "error", err)
    // Event emitted via activity, not workflow
}
```

### Activity Errors

Activities post error events before returning:

```go
if err != nil {
    a.postEvent(ctx, input.RunID, "run_failed", map[string]any{
        "error": err.Error(),
    })
    return err
}
```

### Client Error Handling

Frontend should handle:
- Connection drops (auto-reconnect)
- `run_failed` events (display error UI)
- Malformed events (log and ignore)

---

## Event Persistence

Events are stored in Postgres for:
- Audit trails
- Replay capabilities
- Debugging

**Table**: `run_events`

```sql
CREATE TABLE run_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  seq BIGINT NOT NULL,
  type TEXT NOT NULL,
  timestamp TIMESTAMPTZ NOT NULL,
  source TEXT NOT NULL,
  payload JSONB DEFAULT '{}'::jsonb,
  message_id UUID,
  tool_invocation_id UUID,
  artifact_id UUID
);
```

**Note**: SSE broker uses in-memory pub/sub. Persistence is for record-keeping only.

---

## Testing Events

### Unit Tests

```go
// Test event emission
func TestActivityEmitsEvents(t *testing.T) {
    mockStore := &mockStore{}
    activities := NewRunActivities(mockStore, ...)
    
    // Execute activity
    err := activities.GenerateAssistantReply(ctx, input)
    
    // Verify events were posted
    // (via mock store or event inspection)
}
```

### Integration Tests

```bash
# Run smoke tests
cd scripts
./smoke.sh

# Tests SSE endpoint connectivity
```

### Manual Testing

```bash
# Create a run and stream events
curl -N http://localhost:8080/runs/{id}/events
```

The `-N` flag disables buffering for immediate output.

---

## Performance Considerations

### Event Throughput

- Current design: ~100 events/second per run
- Bottleneck: LLM provider API latency
- SSE overhead: Minimal (text protocol)

### Memory Usage

- Broker stores events in memory until consumed
- Long-running runs may accumulate events
- Cleanup happens on client disconnect

### Optimization Tips

1. **Batch small events**: Combine related updates
2. **Throttle UI updates**: Debounce rapid events
3. **Limit history**: Don't load all historical events on reconnect

---

## Troubleshooting

### Events Not Received

**Check**:
1. SSE connection established (Chrome DevTools → Network → EventStream)
2. Workflow is running: `docker compose exec temporal tctl workflow list`
3. Events being published: Check control plane logs

### Duplicate Events

- Normal during reconnection
- Client should deduplicate by sequence number
- Database stores all events (no deduplication)

### Event Lag

If events appear delayed:
1. Check LLM provider latency
2. Verify network connectivity
3. Monitor control plane CPU usage

---

## Future Enhancements

Potential improvements:

1. **Event Replay**: Load historical events on reconnect
2. **Event Filtering**: Subscribe to specific event types
3. **WebSocket Alternative**: Bidirectional communication
4. **Event Persistence Optimization**: Time-series storage
5. **Cross-Run Events**: Global notifications

---

## See Also

- [Architecture](./architecture.md) - System architecture
- [API Reference](./api-reference.md) - HTTP endpoints
- [Data Model](./data-model.md) - Database schema
- Temporal Documentation: https://docs.temporal.io/
