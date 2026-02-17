CREATE TABLE IF NOT EXISTS automations (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL,
  prompt TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  days JSONB NOT NULL DEFAULT '[]'::jsonb,
  time_of_day TEXT NOT NULL,
  timezone TEXT NOT NULL DEFAULT 'UTC',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  next_run_at TIMESTAMPTZ,
  last_run_at TIMESTAMPTZ,
  in_progress BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS automations_enabled_idx ON automations(enabled);
CREATE INDEX IF NOT EXISTS automations_next_run_idx ON automations(next_run_at);

CREATE TABLE IF NOT EXISTS automation_inbox (
  id UUID PRIMARY KEY,
  automation_id UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
  run_id UUID,
  status TEXT NOT NULL,
  phase TEXT,
  completion_reason TEXT,
  final_response TEXT,
  timed_out BOOLEAN NOT NULL DEFAULT FALSE,
  error TEXT,
  unread BOOLEAN NOT NULL DEFAULT TRUE,
  trigger TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  diagnostics JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS automation_inbox_automation_started_idx ON automation_inbox(automation_id, started_at DESC);
CREATE INDEX IF NOT EXISTS automation_inbox_automation_unread_idx ON automation_inbox(automation_id, unread);
