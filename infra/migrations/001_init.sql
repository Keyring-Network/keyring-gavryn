CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS runs (
  id UUID PRIMARY KEY,
  status TEXT NOT NULL,
  title TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS run_event_sequences (
  run_id UUID PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
  last_seq BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
  id UUID PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  sequence BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS messages_run_id_idx ON messages(run_id);

CREATE TABLE IF NOT EXISTS tool_invocations (
  id UUID PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TIMESTAMPTZ,
  ended_at TIMESTAMPTZ,
  input_json JSONB DEFAULT '{}'::jsonb,
  output_json JSONB DEFAULT '{}'::jsonb,
  error TEXT,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS tool_invocations_run_id_idx ON tool_invocations(run_id);

CREATE TABLE IF NOT EXISTS artifacts (
  id UUID PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  uri TEXT NOT NULL,
  content_type TEXT,
  size_bytes BIGINT,
  created_at TIMESTAMPTZ NOT NULL,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS artifacts_run_id_idx ON artifacts(run_id);

CREATE TABLE IF NOT EXISTS run_events (
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

CREATE UNIQUE INDEX IF NOT EXISTS run_events_run_seq_idx ON run_events(run_id, seq);
