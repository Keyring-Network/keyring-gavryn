ALTER TABLE IF EXISTS runs
  ADD COLUMN IF NOT EXISTS phase TEXT NOT NULL DEFAULT 'executing',
  ADD COLUMN IF NOT EXISTS completion_reason TEXT,
  ADD COLUMN IF NOT EXISTS resumed_from UUID;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'runs_resumed_from_fk'
  ) THEN
    ALTER TABLE runs
      ADD CONSTRAINT runs_resumed_from_fk
      FOREIGN KEY (resumed_from) REFERENCES runs(id) ON DELETE SET NULL;
  END IF;
END
$$;

ALTER TABLE IF EXISTS run_events
  ADD COLUMN IF NOT EXISTS trace_id UUID;

CREATE INDEX IF NOT EXISTS runs_phase_idx ON runs(phase);
CREATE INDEX IF NOT EXISTS run_events_trace_id_idx ON run_events(trace_id);
