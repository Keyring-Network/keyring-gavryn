ALTER TABLE IF EXISTS runs
  ADD COLUMN IF NOT EXISTS phase TEXT NOT NULL DEFAULT 'planning',
  ADD COLUMN IF NOT EXISTS completion_reason TEXT,
  ADD COLUMN IF NOT EXISTS resumed_from UUID,
  ADD COLUMN IF NOT EXISTS checkpoint_seq BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS policy_profile TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS model_route TEXT,
  ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]'::jsonb;

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

CREATE INDEX IF NOT EXISTS runs_phase_idx ON runs(phase);
CREATE INDEX IF NOT EXISTS runs_policy_profile_idx ON runs(policy_profile);
CREATE INDEX IF NOT EXISTS runs_resumed_from_idx ON runs(resumed_from);
