# Data Model

**Last Reviewed**: 2026-02-06  
**Source of Truth Paths**:
- Store interface: `control-plane/internal/store/store.go`
- Migrations: `infra/migrations/`
- Postgres implementation: `control-plane/internal/store/postgres/`

---

## Overview

Gavryn Local uses **PostgreSQL** with **pgvector** extension for data persistence. The storage layer is abstracted through a `Store` interface with Postgres implementation.

---

## Schema Overview

### Entity Relationships

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│     runs        │────▶│    messages      │     │ run_event_seq   │
│ (conversation)  │     │ (chat history)   │     │ (sequences)     │
└─────────────────┘     └──────────────────┘     └─────────────────┘
         │
         │ One-to-Many
         ▼
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│tool_invocations │     │    artifacts     │     │   run_events    │
│ (tool calls)    │     │ (files/output)   │     │ (event log)     │
└─────────────────┘     └──────────────────┘     └─────────────────┘

┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   llm_settings  │     │  memory_settings │     │personality_set  │
│ (AI config)     │     │ (memory toggle)  │     │ (custom prompt) │
└─────────────────┘     └──────────────────┘     └─────────────────┘

┌─────────────────┐     ┌──────────────────┐
│     skills      │────▶│   skill_files    │
│ (skill metadata)│     │ (skill content)  │
└─────────────────┘     └──────────────────┘

┌─────────────────┐
│  context_nodes  │
│ (files/folders) │
└─────────────────┘

┌─────────────────┐
│ memory_entries  │
│ (vector + FTS)  │
└─────────────────┘
```

---

## Core Tables

### runs

Stores conversation runs (chat sessions).

```sql
CREATE TABLE runs (
  id UUID PRIMARY KEY,
  status TEXT NOT NULL,           -- 'running', 'completed', 'cancelled'
  title TEXT,                   -- Optional run title
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  metadata JSONB DEFAULT '{}'::jsonb
);
```

**Go Type**:
```go
type Run struct {
    ID        string
    Status    string
    CreatedAt string
    UpdatedAt string
}

type RunSummary struct {
    ID           string
    Status       string
    Title        string
    CreatedAt    string
    UpdatedAt    string
    MessageCount int64
}
```

**Note**: `RunSummary` used for list operations (added Feb 2026 for history rehydration).

### messages

Chat messages within a run.

```sql
CREATE TABLE messages (
  id UUID PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  role TEXT NOT NULL,           -- 'user', 'assistant', 'system'
  content TEXT NOT NULL,
  sequence BIGINT NOT NULL,     -- Ordering within run
  created_at TIMESTAMPTZ NOT NULL,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX messages_run_id_idx ON messages(run_id);
```

**Go Type**:
```go
type Message struct {
    ID        string
    RunID     string
    Role      string
    Content   string
    Sequence  int64
    CreatedAt string
    Metadata  map[string]any
}
```

**Access Pattern**:
- Load by `run_id` ordered by `sequence ASC`
- Cascade delete with run

### tool_invocations

Records of tool executions.

```sql
CREATE TABLE tool_invocations (
  id UUID PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,      -- e.g., 'browser.navigate'
  status TEXT NOT NULL,         -- 'pending', 'running', 'completed', 'failed'
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  input_json JSONB DEFAULT '{}'::jsonb,
  output_json JSONB DEFAULT '{}'::jsonb,
  error TEXT,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX tool_invocations_run_id_idx ON tool_invocations(run_id);
```

**Lifecycle**:
1. `pending` - Created when LLM requests tool
2. `running` - Worker starts execution
3. `completed`/`failed` - Worker finishes

### artifacts

Generated files (screenshots, documents, etc.).

```sql
CREATE TABLE artifacts (
  id UUID PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  type TEXT NOT NULL,           -- 'screenshot', 'pdf', 'pptx', etc.
  uri TEXT NOT NULL,            -- HTTP URL to access file
  content_type TEXT,            -- MIME type
  size_bytes BIGINT,
  created_at TIMESTAMPTZ NOT NULL,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX artifacts_run_id_idx ON artifacts(run_id);
```

**Storage**: Files stored on filesystem at `workers/{browser,tool-runner}/artifacts/{run_id}/`.

### run_events

Event log for audit and replay.

```sql
CREATE TABLE run_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  seq BIGINT NOT NULL,          -- Sequence number per run
  type TEXT NOT NULL,           -- Event type
  timestamp TIMESTAMPTZ NOT NULL,
  source TEXT NOT NULL,         -- Component emitting event
  payload JSONB DEFAULT '{}'::jsonb,
  message_id UUID,              -- Optional: related message
  tool_invocation_id UUID,      -- Optional: related tool call
  artifact_id UUID             -- Optional: related artifact
);

CREATE UNIQUE INDEX run_events_run_seq_idx ON run_events(run_id, seq);
```

**Sequence Management**:
- Monotonically increasing per run
- Tracked in `run_event_sequences` table
- Used for SSE ordering and deduplication

### run_event_sequences

Per-run sequence counters.

```sql
CREATE TABLE run_event_sequences (
  run_id UUID PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
  last_seq BIGINT NOT NULL DEFAULT 0
);
```

---

## Configuration Tables

### llm_settings

LLM provider configuration (single row).

```sql
CREATE TABLE llm_settings (
  id INT PRIMARY KEY DEFAULT 1,
  mode TEXT NOT NULL DEFAULT 'remote',
  provider TEXT NOT NULL DEFAULT 'codex',
  model TEXT NOT NULL DEFAULT 'gpt-5.2-codex',
  base_url TEXT,
  api_key_enc TEXT,             -- Encrypted API key
  codex_auth_path TEXT,
  codex_home TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
```

**Go Type**:
```go
type LLMSettings struct {
    Mode          string
    Provider      string
    Model         string
    BaseURL       string
    APIKeyEnc     string
    CodexAuthPath string
    CodexHome     string
    CreatedAt     string
    UpdatedAt     string
}
```

**Security**: `api_key_enc` is AES-256-GCM encrypted using `LLM_SECRETS_KEY`.

### memory_settings

Memory system toggle (single row).

```sql
CREATE TABLE memory_settings (
  id INT PRIMARY KEY DEFAULT 1,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
```

### personality_settings

Custom system prompt (single row).

```sql
CREATE TABLE personality_settings (
  id INT PRIMARY KEY DEFAULT 1,
  content TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
```

---

## Feature Tables

### skills

Skill metadata.

```sql
CREATE TABLE skills (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
```

**Filesystem Sync**: Skills materialize to `~/.config/opencode/skills/{skill-name}/`.

### skill_files

Skill file contents.

```sql
CREATE TABLE skill_files (
  id UUID PRIMARY KEY,
  skill_id UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  content BYTEA,                -- Binary content
  content_type TEXT,
  size_bytes BIGINT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(skill_id, path)
);
```

**Structure**:
```
skills/
└── {skill-name}/
    ├── SKILL.md              (required)
    ├── references/           (optional)
    └── scripts/              (optional)
```

### context_nodes

Context folders and files.

```sql
CREATE TABLE context_nodes (
  id UUID PRIMARY KEY,
  parent_id UUID REFERENCES context_nodes(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  node_type TEXT NOT NULL,      -- 'folder', 'file'
  content BYTEA,
  content_type TEXT,
  size_bytes BIGINT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
```

**Hierarchy**: Self-referencing `parent_id` for tree structure.

### memory_entries

Vector + FTS memory storage (requires pgvector extension).

```sql
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE memory_entries (
  id UUID PRIMARY KEY,
  content TEXT NOT NULL,
  metadata JSONB DEFAULT '{}'::jsonb,
  embedding vector(1536),      -- OpenAI embedding dimension
  tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX memory_entries_tsv_idx ON memory_entries USING GIN (tsv);
CREATE INDEX memory_entries_embedding_idx ON memory_entries 
  USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
```

**Hybrid Search**: Combines vector similarity + full-text search.

---

## Migration Files

### File Organization

| File | Tables Created | Purpose |
|------|----------------|---------|
| `001_init.sql` | runs, messages, tool_invocations, artifacts, run_events, run_event_sequences | Core schema |
| `002_llm_settings.sql` | llm_settings | LLM configuration |
| `003_skills.sql` | skills, skill_files | Skills system |
| `004_context.sql` | context_nodes | Context management |
| `005_memory.sql` | memory_settings, memory_entries | Memory system |
| `006_personality.sql` | personality_settings | Custom prompts |

### Migration Execution

```bash
# Automatic on make dev
for migration in infra/migrations/*.sql; do
  psql "$POSTGRES_URL" -f "$migration"
done
```

**Note**: Migrations are idempotent (use `CREATE TABLE IF NOT EXISTS`).

---

## Store Interface

### Interface Definition

**File**: `control-plane/internal/store/store.go`

```go
type Store interface {
    // Runs
    ListRuns(ctx context.Context) ([]RunSummary, error)
    CreateRun(ctx context.Context, run Run) error
    
    // Messages
    AddMessage(ctx context.Context, msg Message) error
    ListMessages(ctx context.Context, runID string) ([]Message, error)
    
    // Settings
    GetLLMSettings(ctx context.Context) (*LLMSettings, error)
    UpsertLLMSettings(ctx context.Context, settings LLMSettings) error
    GetMemorySettings(ctx context.Context) (*MemorySettings, error)
    UpsertMemorySettings(ctx context.Context, settings MemorySettings) error
    GetPersonalitySettings(ctx context.Context) (*PersonalitySettings, error)
    UpsertPersonalitySettings(ctx context.Context, settings PersonalitySettings) error
    
    // Skills
    ListSkills(ctx context.Context) ([]Skill, error)
    GetSkill(ctx context.Context, skillID string) (*Skill, error)
    CreateSkill(ctx context.Context, skill Skill) error
    UpdateSkill(ctx context.Context, skill Skill) error
    DeleteSkill(ctx context.Context, skillID string) error
    ListSkillFiles(ctx context.Context, skillID string) ([]SkillFile, error)
    UpsertSkillFile(ctx context.Context, file SkillFile) error
    DeleteSkillFile(ctx context.Context, skillID string, path string) error
    
    // Context
    ListContextNodes(ctx context.Context) ([]ContextNode, error)
    GetContextFile(ctx context.Context, nodeID string) (*ContextNode, error)
    CreateContextFolder(ctx context.Context, node ContextNode) error
    CreateContextFile(ctx context.Context, node ContextNode) error
    DeleteContextNode(ctx context.Context, nodeID string) error
    
    // Memory
    SearchMemory(ctx context.Context, query string, limit int) ([]MemoryEntry, error)
    
    // Events
    AppendEvent(ctx context.Context, event RunEvent) error
    ListEvents(ctx context.Context, runID string, afterSeq int64) ([]RunEvent, error)
    NextSeq(ctx context.Context, runID string) (int64, error)
}
```

### Implementations

| Implementation | Path | Use Case |
|----------------|------|----------|
| Postgres | `internal/store/postgres/` | Production, development |
| Memory | `internal/store/memory/` | Testing |

---

## Data Flow Patterns

### Chat Flow

```
1. CreateRun()
   → INSERT INTO runs
   → INSERT INTO run_event_sequences (last_seq=0)

2. AddMessage()
   → INSERT INTO messages
   → GetNextSeq()
   → INSERT INTO run_events (user message)

3. Workflow Activity
   → ListMessages(runID)
   → Call LLM
   → For each chunk:
     → GetNextSeq()
     → INSERT INTO run_events (delta)
     → Publish to SSE
   → INSERT INTO messages (assistant response)
```

### Tool Execution Flow

```
1. LLM requests tool
   → INSERT INTO tool_invocations (status='pending')

2. Worker receives request
   → UPDATE tool_invocations SET status='running'
   → Execute tool
   → Emit event (via HTTP to control plane)
   → UPDATE tool_invocations SET status='completed', output_json=...

3. If artifact created:
   → INSERT INTO artifacts
   → Emit artifact_created event
```

---

## Query Examples

### List Recent Runs

```sql
SELECT 
  r.id,
  r.status,
  r.title,
  r.created_at,
  COUNT(m.id) as message_count
FROM runs r
LEFT JOIN messages m ON r.id = m.run_id
GROUP BY r.id, r.status, r.title, r.created_at
ORDER BY r.updated_at DESC
LIMIT 10;
```

### Search Memory (Hybrid)

```sql
-- Vector similarity (top 10)
SELECT content, embedding <=> query_embedding AS distance
FROM memory_entries
ORDER BY embedding <=> query_embedding
LIMIT 10;

-- Full-text search
SELECT content, ts_rank_cd(tsv, query) AS rank
FROM memory_entries, to_tsquery('english', 'search terms') query
WHERE tsv @@ query
ORDER BY rank DESC
LIMIT 10;

-- Combined (hybrid)
WITH vector_results AS (...),
     text_results AS (...)
SELECT * FROM (
  SELECT content, distance, 0 as rank FROM vector_results
  UNION ALL
  SELECT content, 0 as distance, rank FROM text_results
) combined
ORDER BY (distance * 0.5 + (1.0 - rank) * 0.5)
LIMIT 10;
```

### Get Run Timeline

```sql
SELECT 
  e.type,
  e.timestamp,
  e.source,
  e.payload
FROM run_events e
WHERE e.run_id = 'uuid'
ORDER BY e.seq ASC;
```

---

## Performance Considerations

### Indexes

All foreign keys and frequently queried columns are indexed:

- `messages_run_id_idx` - Fast message retrieval
- `tool_invocations_run_id_idx` - Tool history lookup
- `artifacts_run_id_idx` - Artifact listing
- `run_events_run_seq_idx` - Unique constraint + event ordering
- `memory_entries_tsv_idx` - FTS queries
- `memory_entries_embedding_idx` - Vector similarity (ivfflat)

### Partitioning

Not currently implemented. For high-volume deployments, consider:
- Partitioning `run_events` by `run_id` hash
- Archiving old runs

### Connection Pooling

pgx (Go Postgres driver) manages connection pooling automatically.

---

## Backup and Recovery

### Full Backup

```bash
# Dump all data
docker compose exec postgres pg_dump -U gavryn gavryn > backup.sql

# Or with compression
docker compose exec postgres pg_dump -U gavryn gavryn | gzip > backup.sql.gz
```

### Restore

```bash
# Restore
docker compose exec -T postgres psql -U gavryn gavryn < backup.sql

# Or with compression
gunzip < backup.sql.gz | docker compose exec -T postgres psql -U gavryn gavryn
```

### Selective Export

```bash
# Export specific runs and related data
docker compose exec postgres pg_dump -U gavryn gavryn \
  --table=runs \
  --table=messages \
  --table=run_events \
  --where="run_id IN ('uuid1', 'uuid2')" > partial_backup.sql
```

---

## Data Retention

### Automatic Cleanup

Not currently implemented. For maintenance:

```sql
-- Delete old completed runs (example: older than 30 days)
DELETE FROM runs 
WHERE status = 'completed' 
  AND updated_at < NOW() - INTERVAL '30 days';
-- Cascades to messages, events, artifacts records
```

**Note**: Artifact files must be cleaned up separately from filesystem.

---

## See Also

- [Architecture](./architecture.md) - System design
- [API Reference](./api-reference.md) - HTTP endpoints
- [Store Interface](https://github.com/your-repo/gavryn-local/blob/main/control-plane/internal/store/store.go) - Go interface
- [pgvector Documentation](https://github.com/pgvector/pgvector) - Vector extension
