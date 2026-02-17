ALTER TABLE IF EXISTS run_steps
  ADD COLUMN IF NOT EXISTS plan_id TEXT,
  ADD COLUMN IF NOT EXISTS attempt INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS policy_decision TEXT NOT NULL DEFAULT 'allow';

CREATE INDEX IF NOT EXISTS run_steps_run_plan_idx ON run_steps(run_id, plan_id);
