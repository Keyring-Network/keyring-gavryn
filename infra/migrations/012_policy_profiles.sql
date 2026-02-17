CREATE TABLE IF NOT EXISTS policy_profiles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  command_allowlist JSONB DEFAULT '[]'::jsonb,
  path_allowlist JSONB DEFAULT '[]'::jsonb,
  network_allowlist JSONB DEFAULT '[]'::jsonb,
  limits JSONB DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO policy_profiles (name, description, is_default, limits)
SELECT
  'default',
  'Default local-first policy profile',
  TRUE,
  jsonb_build_object(
    'max_timeout_ms', 600000,
    'max_output_bytes', 204800
  )
WHERE NOT EXISTS (SELECT 1 FROM policy_profiles WHERE name = 'default');
