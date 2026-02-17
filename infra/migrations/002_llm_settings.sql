CREATE TABLE IF NOT EXISTS llm_settings (
  id INT PRIMARY KEY CHECK (id = 1),
  mode TEXT NOT NULL,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  base_url TEXT,
  api_key_enc TEXT,
  codex_auth_path TEXT,
  codex_home TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
