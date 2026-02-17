CREATE TABLE IF NOT EXISTS skills (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_files (
  id UUID PRIMARY KEY,
  skill_id UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  content BYTEA NOT NULL,
  content_type TEXT,
  size_bytes BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (skill_id, path)
);

CREATE INDEX IF NOT EXISTS skill_files_skill_id_idx ON skill_files(skill_id);
