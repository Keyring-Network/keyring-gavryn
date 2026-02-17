ALTER TABLE IF EXISTS run_events
  ADD COLUMN IF NOT EXISTS actor TEXT,
  ADD COLUMN IF NOT EXISTS reason_code TEXT,
  ADD COLUMN IF NOT EXISTS redaction_markers JSONB DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS run_events_actor_idx ON run_events(actor);
CREATE INDEX IF NOT EXISTS run_events_reason_code_idx ON run_events(reason_code);
