# Architecture

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Control plane: `control-plane/internal/`
- Frontend: `frontend/src/`
- Workers: `workers/*/src/`
- Database: `infra/migrations/`

---

## System Overview

Gavryn Local follows a **distributed service architecture** with clear separation of concerns:

1. **Control Plane**: Go-based API server and Temporal workflow orchestrator
2. **Frontend**: Vite + React single-page application
3. **Workers**: Node.js microservices for specialized tasks
4. **Data Layer**: Postgres with pgvector for storage and search

---

## Component Architecture

### Control Plane (Go)

The control plane is the central nervous system of Gavryn Local, handling:

- HTTP API requests
- Server-Sent Events (SSE) for real-time updates
- Temporal workflow orchestration
- LLM provider abstraction
- Data persistence

```
┌─────────────────────────────────────────────────────────────────────┐
│                     CONTROL PLANE (Go)                               │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────┐  │
│  │     API      │  │   Workflow   │  │    Store     │  │   LLM    │  │
│  │   Handlers   │  │   Service    │  │  Interface   │  │Providers │  │
│  │              │  │              │  │              │  │          │  │
│  │ server.go    │  │ service.go   │  │ store.go     │  │ codex.go │  │
│  │ routes       │  │ start/signal │  │ postgres.go   │  │ openai.go│  │
│  │ validation   │  │ cancel runs  │  │ memory.go    │  │opencode.go│  │
│  └──────────────┘  └──────────────┘  └──────────────┘  └──────────┘  │
│         │                 │                  │               │      │
│         └─────────────────┴──────────────────┴───────────────┘      │
│                              │                                       │
│                    ┌─────────┴─────────┐                            │
│                    │   Event Broker    │                            │
│                    │    (SSE)          │                            │
│                    │  events/broker.go │                            │
│                    └───────────────────┘                            │
└─────────────────────────────────────────────────────────────────────┘
```

#### Key Packages

| Package | Path | Responsibility |
|---------|------|----------------|
| `api` | `internal/api/` | HTTP handlers, routing, SSE endpoints |
| `workflows` | `internal/workflows/` | Temporal workflow definitions, activities |
| `store` | `internal/store/` | Storage interface + Postgres implementation |
| `llm` | `internal/llm/` | LLM provider adapters (OpenAI, Codex, etc.) |
| `config` | `internal/config/` | Environment configuration loading |
| `events` | `internal/events/` | SSE event broker |
| `secrets` | `internal/secrets/` | API key encryption/decryption |
| `skills` | `internal/skills/` | Skill materialization to filesystem |
| `personality` | `internal/personality/` | System prompt assembly |

### Frontend (React + Vite)

```
┌─────────────────────────────────────────────────────────────┐
│                    FRONTEND (React + Vite)                   │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                   App.tsx                             │  │
│  │         (Main UI state + routing)                     │  │
│  └──────────────────────────────────────────────────────┘  │
│                           │                                  │
│         ┌─────────────────┼─────────────────┐                │
│         ▼                 ▼                 ▼                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐    │
│  │  Chat View  │  │ Settings    │  │ Skills/Context  │    │
│  │  Messages   │  │ Wizard      │  │ Management      │    │
│  │  Composer   │  │ Config      │  │                 │    │
│  └─────────────┘  └─────────────┘  └─────────────────┘    │
│         │                 │                                  │
│         └─────────────────┘                                  │
│                   │                                          │
│         ┌────────┴────────┐                                 │
│         ▼                 ▼                                 │
│  ┌─────────────┐  ┌─────────────┐                        │
│  │  Components │  │     Lib     │                          │
│  │  (shadcn/ui)│  │  (events,  │                          │
│  │  Button     │  │   utils)    │                          │
│  │  Card       │  │             │                          │
│  └─────────────┘  └─────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

#### Key Files

| File | Path | Purpose |
|------|------|---------|
| `App.tsx` | `frontend/src/App.tsx` | Main UI component, state management |
| `events.ts` | `frontend/src/lib/events.ts` | SSE client for real-time updates |
| `utils.ts` | `frontend/src/lib/utils.ts` | Shared utilities (cn helper) |

### Workers (Node.js)

Workers are independent Node.js services that handle specific task types:

#### Browser Worker

```
┌────────────────────────────────────────────────────┐
│              BROWSER WORKER (Node.js)              │
│                   Port 8082                          │
│                                                      │
│  ┌──────────────────────────────────────────────┐  │
│  │         Express Server                         │  │
│  │                                              │  │
│  │  POST /execute → Tool Handler                │  │
│  │       │                                      │  │
│  │       ▼                                      │  │
│  │  ┌──────────────┐    ┌──────────────────┐ │  │
│  │  │ Playwright   │    │   Artifacts        │ │  │
│  │  │ Browser      │───▶│   Storage          │ │  │
│  │  │ Session      │    │   (screenshots)    │ │  │
│  │  └──────────────┘    └──────────────────┘ │  │
│  │                                              │  │
│  │  Tools: navigate, click, type, scroll,     │  │
│  │         extract, evaluate, pdf, snapshot     │  │
│  └──────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────┘
```

**Responsibilities**:
- Browser automation via Playwright
- Screenshot/PDF capture
- Session management per run
- Event emission to control plane

#### Tool Runner

```
┌────────────────────────────────────────────────────┐
│            TOOL RUNNER (Node.js)                   │
│                  Port 8081                         │
│                                                      │
│  ┌──────────────────────────────────────────────┐  │
│  │         Express Server                         │  │
│  │                                              │  │
│  │  POST /execute → Tool Router                 │  │
│  │       │                                      │  │
│  │       ├───▶ Browser Worker (proxy)            │  │
│  │       │                                     │  │
│  │       └───▶ Document Tools                  │  │
│  │             ├── PPTX (pptxgenjs)            │  │
│  │             ├── DOCX (docx)                 │  │
│  │             ├── PDF (pdf-lib)               │  │
│  │             └── CSV (papaparse)             │  │
│  │                                              │  │
│  │  Allowlist filtering for security            │  │
│  └──────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────┘
```

**Responsibilities**:
- Tool allowlist enforcement
- Document generation (PPTX, DOCX, PDF, CSV)
- Browser tool proxying
- Artifact storage and serving

---

## Data Flow

### Chat Flow

```
User ──▶ Frontend ──▶ Control Plane ──▶ Temporal ──▶ Activities
  │        │               │               │            │
  │        │               │               │            ▼
  │        │               │               │      LLM Provider
  │        │               │               │            │
  │        │               │               │            ▼
  │        │               │               │      Response
  │        │               │               │            │
  │        │               │               ▼            │
  │        │               │         SSE Events ◀─────┘
  │        │               │               │
  │        │◀──────────────┴───────────────┘
  │        ▼
  │    Display
  │
  └──▶ (User sees streaming response)
```

### Event Flow

1. **User sends message** → `POST /runs/:id/messages`
2. **Control plane signals workflow** → Temporal signal
3. **Activity executes** → LLM provider call
4. **Events emitted** → SSE broadcast
5. **Frontend receives** → Real-time UI update

---

## Database Schema

### Core Tables

| Table | Purpose | Key Columns |
|-------|---------|-------------|
| `runs` | Conversation runs | id, status, title, created_at, updated_at |
| `messages` | Chat messages | id, run_id, role, content, sequence |
| `tool_invocations` | Tool executions | id, run_id, tool_name, status, input/output |
| `artifacts` | Generated files | id, run_id, type, uri, content_type |
| `run_events` | Event log | id, run_id, seq, type, timestamp, payload |

### Configuration Tables

| Table | Purpose |
|-------|---------|
| `llm_settings` | LLM provider configuration |
| `memory_settings` | Memory system enable/disable |
| `personality_settings` | Custom system prompts |

### Feature Tables

| Table | Purpose |
|-------|---------|
| `skills` | Skill metadata |
| `skill_files` | Skill file contents |
| `context_nodes` | Context folders and files |
| `memory_entries` | Vector + FTS memory storage |

See [Data Model](./data-model.md) for complete schema details.

---

## Communication Patterns

### HTTP REST API

Standard REST endpoints for CRUD operations:

- `POST /runs` - Create new run
- `POST /runs/:id/messages` - Send message
- `GET /runs` - List runs (with metadata)
- `GET /settings/llm` - Get LLM settings
- `POST /skills` - Create skill

### Server-Sent Events (SSE)

Real-time event streaming for UI updates:

```
GET /runs/:id/events

stream: {
  "type": "assistant_message_delta",
  "payload": { "delta": "Hello..." }
}
stream: {
  "type": "tool_started",
  "payload": { "tool_name": "browser.navigate" }
}
stream: {
  "type": "tool_completed",
  "payload": { "result": {...} }
}
```

Event types include:
- `assistant_message_delta` - Streaming LLM response chunks
- `tool_started` - Tool execution beginning
- `tool_completed` - Tool execution finished
- `run_failed` - Error during execution
- `artifact_created` - New artifact generated

### Temporal Workflows

Long-running orchestration for conversation handling:

```go
// Workflow receives messages via signals
func RunWorkflow(ctx workflow.Context, input RunInput) (RunResult, error) {
    messageCh := workflow.GetSignalChannel(ctx, MessageSignalName)
    
    for {
        selector := workflow.NewSelector(ctx)
        selector.AddReceive(messageCh, func(c workflow.ReceiveChannel, more bool) {
            var msg string
            c.Receive(ctx, &msg)
            // Execute activity to generate reply
            workflow.ExecuteActivity(ctx, "GenerateAssistantReply", ...)
        })
        selector.Select(ctx)
    }
}
```

---

## Recent Improvements (Feb 2026)

### Task Queue Isolation

**Problem**: Multiple dev runs consuming from shared queue causing cross-talk.

**Solution**: Configurable `TEMPORAL_TASK_QUEUE` per control-plane port:

```bash
TEMPORAL_TASK_QUEUE=gavryn-runs-8084
```

**Files**:
- `control-plane/internal/config/config.go`
- `control-plane/internal/workflows/service.go`
- `scripts/dev.sh`

### Dev Shutdown Reliability

**Problem**: Orphan worker processes after Ctrl+C.

**Solution**: Job control with process-group-aware cleanup:

- Tracks child process groups and PID trees
- Sends INT → TERM → KILL sequence
- Shutdown flag prevents restart loops

**Files**:
- `scripts/dev.sh`

### History Rehydration

**Problem**: Chat history not reloading on page refresh.

**Solution**: Backend `GET /runs` endpoint with frontend rehydration:

- New `ListRuns` store methods (Postgres + memory)
- Frontend fetches run list on mount
- "Back to tasks" button in Settings

**Files**:
- `control-plane/internal/api/runs.go`
- `control-plane/internal/store/store.go`
- `frontend/src/App.tsx`

### DB Connection Resilience

**Problem**: Stale Postgres port references after Docker restart.

**Solution**: Dynamic port re-reading in dev script:

- Re-reads actual Docker mapped port after startup
- Updates `POSTGRES_PORT` and `POSTGRES_URL`
- Prevents localhost:5434-style mismatches

**Files**:
- `scripts/dev.sh`
- `control-plane/internal/config/config.go`

### Sidebar UX

**Problem**: Sidebar not flush to left edge.

**Solution**: Full-width flex layout with collapsible sidebar:

- `w-80` expanded → `w-[80px]` collapsed
- Smooth CSS transitions
- Toggle button in sidebar

**Files**:
- `frontend/src/App.tsx`

---

## Scaling Considerations

### Single-Node Architecture

Gavryn Local is designed for single-machine deployment:

- **Control plane**: Single instance (can run multiple for dev)
- **Workers**: Multiple instances possible (load balancing not implemented)
- **Database**: Single Postgres instance
- **Temporal**: Single Temporal server

### Resource Requirements

| Service | CPU | Memory | Notes |
|---------|-----|--------|-------|
| Control plane | 0.5 cores | 256MB | Lightweight Go process |
| Frontend (dev) | 0.5 cores | 512MB | Vite dev server |
| Browser worker | 1 core | 1GB | Playwright + Chromium |
| Tool runner | 0.5 cores | 256MB | Document generation |
| Postgres | 1 core | 1GB | With pgvector extension |
| Temporal | 1 core | 1GB | Includes UI |

---

## Security Model

### Authentication

- **LLM Providers**: API keys stored encrypted in Postgres
- **Encryption Key**: `LLM_SECRETS_KEY` (32-byte base64, required)
- **Local Access**: No auth required for local development

### Tool Allowlist

Workers enforce tool allowlisting:

```javascript
const allowlist = [
  "browser.navigate",
  "browser.click",
  "browser.type",
  // ...
];
```

Unauthorized tools return HTTP 403.

### Data Isolation

- Browser sessions are per-run (isolated contexts)
- File system access limited to artifacts directory
- No external network calls except to configured LLM providers

---

## Deployment Patterns

### Development (`make dev`)

```bash
# Starts:
# - Docker (Postgres + Temporal)
# - Control plane (Go)
# - Worker (Go)
# - Tool runner (Node)
# - Browser worker (Node)
# - Frontend (Vite)
```

### Docker Compose Only

```bash
make up  # Just Postgres + Temporal
# Then run services manually
```

### Production Considerations

Not officially supported as a production deployment model. For production use:

- Consider gavryn-cloud (SaaS offering)
- Build custom deployment with proper secrets management
- Use external Postgres and Temporal services
- Implement authentication/authorization

---

## Monitoring & Observability

### Health Endpoints

All services expose `/health`:

```bash
curl http://localhost:8080/health   # Control plane
curl http://localhost:8081/health   # Tool runner
curl http://localhost:8082/health   # Browser worker
```

### Logs

| Service | Log Location |
|---------|--------------|
| Control plane | Stdout (colored) |
| Workers | Stdout |
| Docker | `docker compose logs` |

### Smoke Tests

```bash
make smoke  # Comprehensive health check
```

Validates:
- Docker containers running
- Ports accessible
- Health endpoints responding
- Database connectivity
- LLM connectivity (if configured)

---

## Technology Stack

### Backend

| Component | Technology | Version |
|-----------|------------|---------|
| Language | Go | 1.22+ |
| Web Framework | Chi | v5 |
| Database | Postgres | 16 + pgvector |
| Workflow Engine | Temporal | 1.24 |
| SSE | Go channels | - |
| Testing | testify | - |

### Frontend

| Component | Technology | Version |
|-----------|------------|---------|
| Framework | React | 18 |
| Build Tool | Vite | 5.x |
| Styling | Tailwind CSS | 3.x |
| Components | shadcn/ui | - |
| Icons | Lucide React | - |
| Testing | Vitest | 2.x |
| E2E | Playwright | 1.49 |

### Workers

| Component | Technology | Version |
|-----------|------------|---------|
| Runtime | Node.js | 18+ |
| Framework | Express | 4.x |
| Browser | Playwright | - |
| Documents | pptxgenjs, docx, pdf-lib, papaparse | - |

### Infrastructure

| Component | Technology |
|-----------|------------|
| Container Runtime | Docker |
| Database | Postgres 16 (pgvector/pgvector) |
| Workflow Engine | Temporal |
| Migration Tool | psql |

---

## Architecture Decisions

### Why Temporal?

Temporal provides:
- Durable execution (survives process restarts)
- Built-in retry and timeout handling
- Signal-based message passing
- Visibility into workflow state

### Why SSE over WebSockets?

Server-Sent Events were chosen because:
- Simpler protocol (HTTP-based)
- Automatic reconnection
- Unidirectional (server → client) matches our use case
- Better compatibility with proxies/load balancers

### Why pgvector?

Postgres with pgvector provides:
- Single database for all storage needs
- Vector similarity search for memory
- Full-text search (tsvector) for hybrid queries
- ACID compliance for data integrity

---

## Future Architecture Directions

### Potential Enhancements

1. **Multi-tenant Support**: Namespace isolation for shared deployments
2. **Plugin System**: Dynamic skill loading without restart
3. **Distributed Workers**: Worker pools across multiple machines
4. **Streaming LLM Responses**: Token-by-token streaming (currently chunk-based)
5. **Advanced Memory**: Conversation summarization, entity extraction

---

## See Also

- [Data Model](./data-model.md) - Complete database schema
- [Workflows](./workflows.md) - Temporal workflow details
- [API Reference](./api-reference.md) - HTTP endpoints
- [Configuration](./configuration.md) - Environment variables
