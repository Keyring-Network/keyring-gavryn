ALTER TABLE IF EXISTS artifacts
  ADD COLUMN IF NOT EXISTS checksum TEXT,
  ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'generic',
  ADD COLUMN IF NOT EXISTS labels JSONB DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS searchable_text TEXT;

CREATE INDEX IF NOT EXISTS artifacts_run_category_idx ON artifacts(run_id, category);
CREATE INDEX IF NOT EXISTS artifacts_checksum_idx ON artifacts(checksum);
CREATE INDEX IF NOT EXISTS artifacts_searchable_text_idx ON artifacts USING GIN (to_tsvector('simple', COALESCE(searchable_text, '')));
