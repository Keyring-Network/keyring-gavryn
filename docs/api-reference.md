# API Reference

**Last Reviewed**: 2026-02-07  
**Contract Mode**: Single canonical contract (no compatibility aliases)

## Base URLs
- Control plane: `http://localhost:8080`
- Tool runner: `http://localhost:8081`
- Browser worker: `http://localhost:8082`

## Control Plane HTTP Contract

### Canonical endpoint inventory

<!-- CONTROL_PLANE_ROUTES_START -->
```http
POST /runs
GET /runs
GET /runs/{id}
DELETE /runs/{id}
POST /runs/{id}/messages
POST /runs/{id}/resume
POST /runs/{id}/cancel
POST /runs/{id}/events
GET /runs/{id}/events
GET /runs/{id}/steps
GET /runs/{id}/workspace
GET /runs/{id}/workspace/tree
GET /runs/{id}/workspace/file
PUT /runs/{id}/workspace/file
DELETE /runs/{id}/workspace/file
GET /runs/{id}/workspace/stat
POST /runs/{id}/processes/exec
GET /runs/{id}/processes
POST /runs/{id}/processes/start
GET /runs/{id}/processes/{pid}
GET /runs/{id}/processes/{pid}/logs
POST /runs/{id}/processes/{pid}/stop
GET /runs/{id}/artifacts
POST /automation/execute
GET /settings/llm
POST /settings/llm
POST /settings/llm/test
POST /settings/llm/models
GET /settings/memory
POST /settings/memory
GET /settings/personality
POST /settings/personality
GET /skills
POST /skills
PUT /skills/{id}
DELETE /skills/{id}
GET /skills/{id}/files
POST /skills/{id}/files
DELETE /skills/{id}/files
GET /context
POST /context/folders
POST /context/files
GET /context/files/{id}
DELETE /context/{id}
GET /health
GET /ready
```
<!-- CONTROL_PLANE_ROUTES_END -->

### Selected payload contracts

#### `POST /runs`
Creates a run and accepts execution controls:

```json
{
  "goal": "create a nextjs marketing site",
  "policy_profile": "default",
  "model_route": "opencode-zen:kimi-k2.5,openai:gpt-4.1-mini",
  "tags": ["marketing", "website"],
  "metadata": {"intent": "build"}
}
```

#### `GET /runs/{id}`
Returns canonical run state fields, including phase and resume metadata.

```json
{
  "id": "uuid",
  "status": "running|completed|partial|failed|cancelled",
  "phase": "planning|executing|validating|terminal",
  "completion_reason": "success|partial|llm_unavailable|cancelled|error",
  "resumed_from": "uuid-or-empty",
  "checkpoint_seq": 123,
  "policy_profile": "default",
  "model_route": "opencode-zen:kimi-k2.5",
  "created_at": "RFC3339Nano",
  "updated_at": "RFC3339Nano"
}
```

#### `GET /runs/{id}/artifacts`
Supports filtering and pagination via `query`, `category`, `content_type`, `label`, `page`, `page_size`.

```json
{
  "artifacts": [
    {
      "id": "artifact-id",
      "type": "file|screenshot|preview|...",
      "category": "document|image|code|process|browser",
      "uri": "http://...",
      "content_type": "text/plain",
      "size_bytes": 1024,
      "checksum": "sha256:...",
      "labels": ["preview"],
      "retention_class": "default",
      "created_at": "RFC3339Nano"
    }
  ],
  "page": 1,
  "page_size": 50,
  "total": 1,
  "total_pages": 1
}
```

#### `POST /automation/execute`
Runs a single automation prompt and optionally waits for completion, returning the final assistant output plus research diagnostics.

```json
{
  "prompt": "Browse the web and give me the top 8 DeFi news items from February 2026 with source links and a comprehensive summary",
  "wait_for_completion": true,
  "timeout_ms": 180000,
  "poll_interval_ms": 1200,
  "metadata": {
    "llm_model": "kimi-k2.5",
    "browser_mode": "user_tab",
    "browser_interaction": "allow"
  }
}
```

Typical response fields:

```json
{
  "run_id": "uuid",
  "status": "completed|partial|failed|running",
  "phase": "planning|executing|validating|terminal",
  "final_response": "assistant markdown",
  "diagnostics": {
    "usable_sources_count": 4,
    "low_quality_sources_count": 2,
    "blocked_sources_count": 1,
    "sources": []
  }
}
```

## SSE Event Envelope

`GET /runs/{id}/events?after_seq=N`

```json
{
  "run_id": "uuid",
  "seq": 123,
  "ts": "RFC3339Nano",
  "type": "step.started",
  "source": "control_plane|worker|browser_worker|tool_runner",
  "trace_id": "uuid",
  "payload": {}
}
```

Rules:
- Dot-notation event types only (`step.started`, `tool.completed`, `policy.denied`)
- Strictly monotonic `seq`
- Replay from cursor (`after_seq`)

## Tool Runner Contract

### Canonical endpoints

```http
POST /tools/execute
GET /tools/capabilities
GET /health
GET /ready
```

### Execute request envelope

```json
{
  "contract_version": "tool_contract_v2",
  "run_id": "uuid",
  "invocation_id": "uuid",
  "idempotency_key": "uuid-or-stable-key",
  "tool_name": "editor.write|process.start|browser.navigate|...",
  "input": {},
  "timeout_ms": 30000,
  "policy_context": {
    "profile": "default"
  }
}
```

### Execute response envelope

```json
{
  "status": "completed|failed|denied",
  "output": {},
  "artifacts": [],
  "error": "optional error text"
}
```

## Browser Worker Contract

### Canonical endpoints

```http
POST /tools/execute
POST /cancel
GET /health
GET /ready
```
