CREATE TABLE IF NOT EXISTS run_processes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  process_id TEXT NOT NULL,
  command TEXT NOT NULL,
  args JSONB DEFAULT '[]'::jsonb,
  cwd TEXT,
  status TEXT NOT NULL DEFAULT 'running',
  pid INT,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ended_at TIMESTAMPTZ,
  exit_code INT,
  signal TEXT,
  preview_urls JSONB DEFAULT '[]'::jsonb,
  metadata JSONB DEFAULT '{}'::jsonb
);

CREATE UNIQUE INDEX IF NOT EXISTS run_processes_run_process_id_idx ON run_processes(run_id, process_id);
CREATE INDEX IF NOT EXISTS run_processes_run_status_idx ON run_processes(run_id, status);
