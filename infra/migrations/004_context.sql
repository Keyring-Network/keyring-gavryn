CREATE TABLE IF NOT EXISTS context_nodes (
  id UUID PRIMARY KEY,
  parent_id UUID REFERENCES context_nodes(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  node_type TEXT NOT NULL,
  content BYTEA,
  content_type TEXT,
  size_bytes BIGINT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS context_nodes_parent_id_idx ON context_nodes(parent_id);
