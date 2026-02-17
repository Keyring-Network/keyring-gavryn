ALTER TABLE IF EXISTS tool_invocations
  ADD COLUMN IF NOT EXISTS idempotency_key TEXT,
  ADD COLUMN IF NOT EXISTS retry_count INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS policy_decision TEXT NOT NULL DEFAULT 'allow',
  ADD COLUMN IF NOT EXISTS metrics JSONB DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS contract_version TEXT NOT NULL DEFAULT 'v1';

CREATE UNIQUE INDEX IF NOT EXISTS tool_invocations_run_idempotency_idx
  ON tool_invocations(run_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;
