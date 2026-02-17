ALTER TABLE IF EXISTS artifacts
  ADD COLUMN IF NOT EXISTS retention_class TEXT NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS artifacts_retention_class_idx ON artifacts(retention_class);
