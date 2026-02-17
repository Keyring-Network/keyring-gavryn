CREATE TABLE IF NOT EXISTS run_steps (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  step_id TEXT NOT NULL,
  parent_step_id TEXT,
  name TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  dependencies JSONB DEFAULT '[]'::jsonb,
  expected_artifacts JSONB DEFAULT '[]'::jsonb,
  diagnostics JSONB DEFAULT '{}'::jsonb,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS run_steps_run_step_id_idx ON run_steps(run_id, step_id);
CREATE INDEX IF NOT EXISTS run_steps_run_status_idx ON run_steps(run_id, status);
