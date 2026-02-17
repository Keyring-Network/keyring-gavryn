package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type PostgresStore struct {
	db *sql.DB
}

var openDB = sql.Open

func New(conn string) (*PostgresStore, error) {
	db, err := openDB("pgx", conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := verifySchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &PostgresStore{db: db}, nil
}

func verifySchema(ctx context.Context, db *sql.DB) error {
	required := []string{
		"runs",
		"messages",
		"run_events",
		"run_event_sequences",
		"run_steps",
		"run_processes",
		"llm_settings",
		"skills",
		"skill_files",
		"context_nodes",
		"memory_settings",
		"memory_entries",
		"personality_settings",
		"artifacts",
		"automations",
		"automation_inbox",
	}
	for _, table := range required {
		var regclass sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT to_regclass($1)", fmt.Sprintf("public.%s", table)).Scan(&regclass); err != nil {
			return err
		}
		if !regclass.Valid {
			return fmt.Errorf("database schema missing: %s table not found (run infra/migrations/001_init.sql)", table)
		}
	}
	return nil
}

func (p *PostgresStore) CreateRun(ctx context.Context, run store.Run) error {
	phase := strings.TrimSpace(run.Phase)
	if phase == "" {
		phase = "planning"
	}
	policyProfile := strings.TrimSpace(run.PolicyProfile)
	if policyProfile == "" {
		policyProfile = "default"
	}
	tagsBytes, err := json.Marshal(run.Tags)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO runs (
			id,
			status,
			phase,
			completion_reason,
			resumed_from,
			checkpoint_seq,
			policy_profile,
			model_route,
			tags,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = p.db.ExecContext(
		ctx,
		query,
		run.ID,
		run.Status,
		phase,
		nullString(run.CompletionReason),
		nullString(run.ResumedFrom),
		run.CheckpointSeq,
		policyProfile,
		nullString(run.ModelRoute),
		tagsBytes,
		run.CreatedAt,
		run.UpdatedAt,
	)
	return err
}

func (p *PostgresStore) DeleteRun(ctx context.Context, runID string) error {
	_, err := p.db.ExecContext(ctx, "DELETE FROM runs WHERE id = $1", runID)
	return err
}

func (p *PostgresStore) ListRuns(ctx context.Context) ([]store.RunSummary, error) {
	const query = `
		SELECT
			r.id,
			COALESCE(
				CASE latest.type
					WHEN 'run.completed' THEN 'completed'
					WHEN 'run.failed' THEN 'failed'
					WHEN 'run.cancelled' THEN 'cancelled'
					WHEN 'run.partial' THEN 'partial'
					WHEN 'run.started' THEN 'running'
					ELSE r.status
				END,
				r.status
			) AS status,
			r.phase,
			r.completion_reason,
			r.resumed_from,
			r.checkpoint_seq,
			r.policy_profile,
			r.model_route,
			r.tags,
			r.created_at,
			COALESCE(latest.timestamp, r.updated_at) AS updated_at,
			COALESCE(NULLIF(r.title, ''), title_event.title, first_message.content, '') AS title,
			COUNT(m.id) AS message_count
		FROM runs r
		LEFT JOIN LATERAL (
			SELECT type, timestamp
			FROM run_events
			WHERE run_id = r.id
			ORDER BY seq DESC
			LIMIT 1
		) latest ON true
		LEFT JOIN LATERAL (
			SELECT payload->>'title' AS title
			FROM run_events
			WHERE run_id = r.id
				AND type = 'run.title.updated'
				AND payload ? 'title'
			ORDER BY seq DESC
			LIMIT 1
		) title_event ON true
		LEFT JOIN LATERAL (
			SELECT content
			FROM messages
			WHERE run_id = r.id AND role = 'user'
			ORDER BY sequence ASC
			LIMIT 1
		) first_message ON true
		LEFT JOIN messages m ON m.run_id = r.id
		GROUP BY r.id, r.status, r.phase, r.completion_reason, r.resumed_from, r.checkpoint_seq, r.policy_profile, r.model_route, r.tags, r.created_at, r.updated_at, r.title, latest.type, latest.timestamp, title_event.title, first_message.content
		ORDER BY COALESCE(latest.timestamp, r.updated_at) DESC
	`
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.RunSummary{}
	for rows.Next() {
		var createdAt time.Time
		var updatedAt time.Time
		var completionReason sql.NullString
		var resumedFrom sql.NullString
		var modelRoute sql.NullString
		var tagsBytes []byte
		var summary store.RunSummary
		if err := rows.Scan(
			&summary.ID,
			&summary.Status,
			&summary.Phase,
			&completionReason,
			&resumedFrom,
			&summary.CheckpointSeq,
			&summary.PolicyProfile,
			&modelRoute,
			&tagsBytes,
			&createdAt,
			&updatedAt,
			&summary.Title,
			&summary.MessageCount,
		); err != nil {
			return nil, err
		}
		if completionReason.Valid {
			summary.CompletionReason = completionReason.String
		}
		if resumedFrom.Valid {
			summary.ResumedFrom = resumedFrom.String
		}
		if modelRoute.Valid {
			summary.ModelRoute = modelRoute.String
		}
		summary.Tags = decodeStringSlice(tagsBytes)
		if summary.Phase == "" {
			summary.Phase = "planning"
		}
		summary.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		summary.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		results = append(results, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) AddMessage(ctx context.Context, msg store.Message) error {
	metadata := msg.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO messages (id, run_id, role, content, sequence, created_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = p.db.ExecContext(ctx, query, msg.ID, msg.RunID, msg.Role, msg.Content, msg.Sequence, msg.CreatedAt, encoded)
	return err
}

func (p *PostgresStore) ListMessages(ctx context.Context, runID string) ([]store.Message, error) {
	const query = `
		SELECT id, run_id, role, content, sequence, created_at, metadata
		FROM messages
		WHERE run_id = $1
		ORDER BY sequence ASC
	`
	rows, err := p.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.Message{}
	for rows.Next() {
		var createdAt time.Time
		var metadataBytes []byte
		var msg store.Message
		if err := rows.Scan(&msg.ID, &msg.RunID, &msg.Role, &msg.Content, &msg.Sequence, &createdAt, &metadataBytes); err != nil {
			return nil, err
		}
		msg.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		if len(metadataBytes) > 0 {
			metadata := map[string]any{}
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				return nil, err
			}
			msg.Metadata = metadata
		} else {
			msg.Metadata = map[string]any{}
		}
		results = append(results, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) GetLLMSettings(ctx context.Context) (*store.LLMSettings, error) {
	const query = `
		SELECT mode, provider, model, base_url, api_key_enc, codex_auth_path, codex_home, created_at, updated_at
		FROM llm_settings
		WHERE id = 1
	`
	var createdAt time.Time
	var updatedAt time.Time
	settings := store.LLMSettings{}
	if err := p.db.QueryRowContext(ctx, query).Scan(
		&settings.Mode,
		&settings.Provider,
		&settings.Model,
		&settings.BaseURL,
		&settings.APIKeyEnc,
		&settings.CodexAuthPath,
		&settings.CodexHome,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	settings.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	settings.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &settings, nil
}

func (p *PostgresStore) UpsertLLMSettings(ctx context.Context, settings store.LLMSettings) error {
	const query = `
		INSERT INTO llm_settings
			(id, mode, provider, model, base_url, api_key_enc, codex_auth_path, codex_home, created_at, updated_at)
		VALUES
			(1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id)
		DO UPDATE SET
			mode = EXCLUDED.mode,
			provider = EXCLUDED.provider,
			model = EXCLUDED.model,
			base_url = EXCLUDED.base_url,
			api_key_enc = EXCLUDED.api_key_enc,
			codex_auth_path = EXCLUDED.codex_auth_path,
			codex_home = EXCLUDED.codex_home,
			updated_at = EXCLUDED.updated_at
	`
	_, err := p.db.ExecContext(
		ctx,
		query,
		settings.Mode,
		settings.Provider,
		settings.Model,
		settings.BaseURL,
		settings.APIKeyEnc,
		settings.CodexAuthPath,
		settings.CodexHome,
		settings.CreatedAt,
		settings.UpdatedAt,
	)
	return err
}

func (p *PostgresStore) ListSkills(ctx context.Context) ([]store.Skill, error) {
	const query = `
		SELECT id, name, description, created_at, updated_at
		FROM skills
		ORDER BY name ASC
	`
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.Skill{}
	for rows.Next() {
		var createdAt time.Time
		var updatedAt time.Time
		var skill store.Skill
		if err := rows.Scan(&skill.ID, &skill.Name, &skill.Description, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		skill.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		skill.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		results = append(results, skill)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) GetSkill(ctx context.Context, skillID string) (*store.Skill, error) {
	const query = `
		SELECT id, name, description, created_at, updated_at
		FROM skills
		WHERE id = $1
	`
	var createdAt time.Time
	var updatedAt time.Time
	skill := store.Skill{}
	if err := p.db.QueryRowContext(ctx, query, skillID).Scan(
		&skill.ID,
		&skill.Name,
		&skill.Description,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	skill.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	skill.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &skill, nil
}

func (p *PostgresStore) CreateSkill(ctx context.Context, skill store.Skill) error {
	const query = `
		INSERT INTO skills (id, name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := p.db.ExecContext(ctx, query, skill.ID, skill.Name, skill.Description, skill.CreatedAt, skill.UpdatedAt)
	return err
}

func (p *PostgresStore) UpdateSkill(ctx context.Context, skill store.Skill) error {
	const query = `
		UPDATE skills
		SET name = $1, description = $2, updated_at = $3
		WHERE id = $4
	`
	_, err := p.db.ExecContext(ctx, query, skill.Name, skill.Description, skill.UpdatedAt, skill.ID)
	return err
}

func (p *PostgresStore) DeleteSkill(ctx context.Context, skillID string) error {
	const query = `
		DELETE FROM skills
		WHERE id = $1
	`
	_, err := p.db.ExecContext(ctx, query, skillID)
	return err
}

func (p *PostgresStore) ListSkillFiles(ctx context.Context, skillID string) ([]store.SkillFile, error) {
	const query = `
		SELECT id, skill_id, path, content, content_type, size_bytes, created_at, updated_at
		FROM skill_files
		WHERE skill_id = $1
		ORDER BY path ASC
	`
	rows, err := p.db.QueryContext(ctx, query, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.SkillFile{}
	for rows.Next() {
		var createdAt time.Time
		var updatedAt time.Time
		var file store.SkillFile
		if err := rows.Scan(
			&file.ID,
			&file.SkillID,
			&file.Path,
			&file.Content,
			&file.ContentType,
			&file.SizeBytes,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		file.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		file.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		results = append(results, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) UpsertSkillFile(ctx context.Context, file store.SkillFile) error {
	const query = `
		INSERT INTO skill_files
			(id, skill_id, path, content, content_type, size_bytes, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (skill_id, path)
		DO UPDATE SET
			content = EXCLUDED.content,
			content_type = EXCLUDED.content_type,
			size_bytes = EXCLUDED.size_bytes,
			updated_at = EXCLUDED.updated_at
	`
	_, err := p.db.ExecContext(
		ctx,
		query,
		file.ID,
		file.SkillID,
		file.Path,
		file.Content,
		file.ContentType,
		file.SizeBytes,
		file.CreatedAt,
		file.UpdatedAt,
	)
	return err
}

func (p *PostgresStore) DeleteSkillFile(ctx context.Context, skillID string, path string) error {
	const query = `
		DELETE FROM skill_files
		WHERE skill_id = $1 AND path = $2
	`
	_, err := p.db.ExecContext(ctx, query, skillID, path)
	return err
}

func (p *PostgresStore) ListContextNodes(ctx context.Context) ([]store.ContextNode, error) {
	const query = `
		SELECT id, parent_id, name, node_type, content_type, size_bytes, created_at, updated_at
		FROM context_nodes
		ORDER BY name ASC
	`
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.ContextNode{}
	for rows.Next() {
		var parentID sql.NullString
		var createdAt time.Time
		var updatedAt time.Time
		var node store.ContextNode
		if err := rows.Scan(
			&node.ID,
			&parentID,
			&node.Name,
			&node.NodeType,
			&node.ContentType,
			&node.SizeBytes,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		if parentID.Valid {
			node.ParentID = parentID.String
		}
		node.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		node.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		results = append(results, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) GetContextFile(ctx context.Context, nodeID string) (*store.ContextNode, error) {
	const query = `
		SELECT id, parent_id, name, node_type, content, content_type, size_bytes, created_at, updated_at
		FROM context_nodes
		WHERE id = $1
	`
	var parentID sql.NullString
	var createdAt time.Time
	var updatedAt time.Time
	node := store.ContextNode{}
	if err := p.db.QueryRowContext(ctx, query, nodeID).Scan(
		&node.ID,
		&parentID,
		&node.Name,
		&node.NodeType,
		&node.Content,
		&node.ContentType,
		&node.SizeBytes,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if parentID.Valid {
		node.ParentID = parentID.String
	}
	node.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	node.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &node, nil
}

func (p *PostgresStore) CreateContextFolder(ctx context.Context, node store.ContextNode) error {
	const query = `
		INSERT INTO context_nodes (id, parent_id, name, node_type, content, content_type, size_bytes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	var parentID any
	if node.ParentID == "" {
		parentID = nil
	} else {
		parentID = node.ParentID
	}
	_, err := p.db.ExecContext(
		ctx,
		query,
		node.ID,
		parentID,
		node.Name,
		node.NodeType,
		node.Content,
		node.ContentType,
		node.SizeBytes,
		node.CreatedAt,
		node.UpdatedAt,
	)
	return err
}

func (p *PostgresStore) CreateContextFile(ctx context.Context, node store.ContextNode) error {
	return p.CreateContextFolder(ctx, node)
}

func (p *PostgresStore) DeleteContextNode(ctx context.Context, nodeID string) error {
	const query = `
		DELETE FROM context_nodes
		WHERE id = $1
	`
	_, err := p.db.ExecContext(ctx, query, nodeID)
	return err
}

func (p *PostgresStore) GetMemorySettings(ctx context.Context) (*store.MemorySettings, error) {
	const query = `
		SELECT enabled, created_at, updated_at
		FROM memory_settings
		WHERE id = 1
	`
	var createdAt time.Time
	var updatedAt time.Time
	settings := store.MemorySettings{}
	if err := p.db.QueryRowContext(ctx, query).Scan(&settings.Enabled, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	settings.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	settings.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &settings, nil
}

func (p *PostgresStore) UpsertMemorySettings(ctx context.Context, settings store.MemorySettings) error {
	const query = `
		INSERT INTO memory_settings (id, enabled, created_at, updated_at)
		VALUES (1, $1, $2, $3)
		ON CONFLICT (id)
		DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at
	`
	_, err := p.db.ExecContext(ctx, query, settings.Enabled, settings.CreatedAt, settings.UpdatedAt)
	return err
}

func (p *PostgresStore) UpsertMemoryEntry(ctx context.Context, entry store.MemoryEntry) (bool, error) {
	metadata := entry.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return false, err
	}
	fingerprint := readMetadataString(metadata, "fingerprint")
	if fingerprint == "" {
		return p.insertMemoryEntry(ctx, entry, encoded)
	}
	return p.insertMemoryEntryWithFingerprint(ctx, entry, encoded, fingerprint)
}

func (p *PostgresStore) GetPersonalitySettings(ctx context.Context) (*store.PersonalitySettings, error) {
	const query = `
		SELECT content, created_at, updated_at
		FROM personality_settings
		WHERE id = 1
	`
	settings := store.PersonalitySettings{}
	var createdAt time.Time
	var updatedAt time.Time
	if err := p.db.QueryRowContext(ctx, query).Scan(&settings.Content, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	settings.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	settings.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &settings, nil
}

func (p *PostgresStore) UpsertPersonalitySettings(ctx context.Context, settings store.PersonalitySettings) error {
	const query = `
		INSERT INTO personality_settings (id, content, created_at, updated_at)
		VALUES (1, $1, $2, $3)
		ON CONFLICT (id)
		DO UPDATE SET content = EXCLUDED.content, updated_at = EXCLUDED.updated_at
	`
	_, err := p.db.ExecContext(ctx, query, settings.Content, settings.CreatedAt, settings.UpdatedAt)
	return err
}

func (p *PostgresStore) SearchMemory(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return []store.MemoryEntry{}, nil
	}
	const sqlQuery = `
		SELECT id, content, metadata, created_at, updated_at
		FROM memory_entries
		WHERE tsv @@ plainto_tsquery('english', $1)
		ORDER BY ts_rank_cd(tsv, plainto_tsquery('english', $1)) DESC, updated_at DESC
		LIMIT $2
	`
	rows, err := p.db.QueryContext(ctx, sqlQuery, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.MemoryEntry{}
	for rows.Next() {
		var createdAt time.Time
		var updatedAt time.Time
		var metadataBytes []byte
		var entry store.MemoryEntry
		if err := rows.Scan(&entry.ID, &entry.Content, &metadataBytes, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		entry.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		entry.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		if len(metadataBytes) > 0 {
			metadata := map[string]any{}
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				return nil, err
			}
			entry.Metadata = metadata
		} else {
			entry.Metadata = map[string]any{}
		}
		results = append(results, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) SearchMemoryWithEmbedding(ctx context.Context, query string, embedding []float32, limit int) ([]store.MemoryEntry, error) {
	if limit <= 0 {
		return []store.MemoryEntry{}, nil
	}
	if len(embedding) == 0 || len(embedding) != memoryEmbeddingDimensions {
		return p.SearchMemory(ctx, query, limit)
	}
	vector := formatVector(embedding)
	const sqlQuery = `
		SELECT id, content, metadata, created_at, updated_at
		FROM memory_entries
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`
	rows, err := p.db.QueryContext(ctx, sqlQuery, vector, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.MemoryEntry{}
	for rows.Next() {
		var createdAt time.Time
		var updatedAt time.Time
		var metadataBytes []byte
		var entry store.MemoryEntry
		if err := rows.Scan(&entry.ID, &entry.Content, &metadataBytes, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		entry.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		entry.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		if len(metadataBytes) > 0 {
			metadata := map[string]any{}
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				return nil, err
			}
			entry.Metadata = metadata
		} else {
			entry.Metadata = map[string]any{}
		}
		results = append(results, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return p.SearchMemory(ctx, query, limit)
	}
	return results, nil
}

func (p *PostgresStore) AppendEvent(ctx context.Context, event store.RunEvent) error {
	event.Type = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(event.Type)), "_", ".")
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	timestamp := event.Timestamp
	if timestamp == "" {
		timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	timestampValue := parseTimestampValue(timestamp)
	traceID := strings.TrimSpace(event.TraceID)
	var traceIDValue any
	if traceID == "" {
		traceIDValue = nil
	} else if _, err := uuid.Parse(traceID); err != nil {
		traceIDValue = nil
	} else {
		traceIDValue = traceID
	}
	const query = `
		INSERT INTO run_events (run_id, seq, type, timestamp, source, trace_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, query, event.RunID, event.Seq, event.Type, timestampValue, event.Source, traceIDValue, encoded); err != nil {
		return err
	}
	if step, ok := store.BuildRunStepFromEvent(event); ok {
		if err = upsertRunStepTx(ctx, tx, step); err != nil {
			return err
		}
	}
	if err = applyRunStateUpdateTx(ctx, tx, event); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func (p *PostgresStore) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
	const query = `
		SELECT run_id, seq, type, timestamp, source, trace_id, payload
		FROM run_events
		WHERE run_id = $1 AND seq > $2
		ORDER BY seq ASC
	`
	rows, err := p.db.QueryContext(ctx, query, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.RunEvent{}
	for rows.Next() {
		var payloadBytes []byte
		var timestamp time.Time
		var traceID sql.NullString
		var event store.RunEvent
		if err := rows.Scan(&event.RunID, &event.Seq, &event.Type, &timestamp, &event.Source, &traceID, &payloadBytes); err != nil {
			return nil, err
		}
		event.Timestamp = timestamp.UTC().Format(time.RFC3339Nano)
		if traceID.Valid {
			event.TraceID = traceID.String
		}
		if len(payloadBytes) > 0 {
			payload := map[string]any{}
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				return nil, err
			}
			event.Payload = payload
		} else {
			event.Payload = map[string]any{}
		}
		results = append(results, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) ListRunSteps(ctx context.Context, runID string) ([]store.RunStep, error) {
	const query = `
		SELECT run_id,
			step_id,
			parent_step_id,
			name,
			status,
			plan_id,
			attempt,
			policy_decision,
			dependencies,
			expected_artifacts,
			diagnostics,
			started_at,
			completed_at
		FROM run_steps
		WHERE run_id = $1
		ORDER BY COALESCE((diagnostics->>'seq')::bigint, 9223372036854775807), created_at ASC
	`
	rows, err := p.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	steps := []store.RunStep{}
	for rows.Next() {
		var (
			step              store.RunStep
			parentStepID      sql.NullString
			name              sql.NullString
			planID            sql.NullString
			policyDecision    sql.NullString
			dependenciesBytes []byte
			expectedBytes     []byte
			diagnosticsBytes  []byte
			startedAt         sql.NullTime
			completedAt       sql.NullTime
		)
		if err := rows.Scan(
			&step.RunID,
			&step.ID,
			&parentStepID,
			&name,
			&step.Status,
			&planID,
			&step.Attempt,
			&policyDecision,
			&dependenciesBytes,
			&expectedBytes,
			&diagnosticsBytes,
			&startedAt,
			&completedAt,
		); err != nil {
			return nil, err
		}
		if parentStepID.Valid {
			step.ParentStepID = parentStepID.String
		}
		if name.Valid {
			step.Name = name.String
		}
		if planID.Valid {
			step.PlanID = planID.String
		}
		if policyDecision.Valid {
			step.PolicyDecision = policyDecision.String
		}
		if startedAt.Valid {
			step.StartedAt = startedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if completedAt.Valid {
			step.CompletedAt = completedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		step.Dependencies = decodeStringSlice(dependenciesBytes)
		step.ExpectedArtifacts = decodeStringSlice(expectedBytes)
		step.Diagnostics = decodeJSONMap(diagnosticsBytes)
		step.Kind = readDiagString(step.Diagnostics, "kind")
		if step.Kind == "" {
			step.Kind = "step"
		}
		step.Source = readDiagString(step.Diagnostics, "source")
		step.Seq = readDiagInt64(step.Diagnostics, "seq")
		step.Error = readDiagString(step.Diagnostics, "error")
		if step.Name == "" {
			step.Name = step.ID
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return steps, nil
}

func (p *PostgresStore) UpsertRunProcess(ctx context.Context, process store.RunProcess) error {
	if strings.TrimSpace(process.RunID) == "" {
		return fmt.Errorf("run id required")
	}
	if strings.TrimSpace(process.ProcessID) == "" {
		return fmt.Errorf("process id required")
	}
	if strings.TrimSpace(process.Command) == "" {
		process.Command = "process"
	}
	if strings.TrimSpace(process.Status) == "" {
		process.Status = "running"
	}
	argsBytes, err := json.Marshal(process.Args)
	if err != nil {
		return err
	}
	previewBytes, err := json.Marshal(process.PreviewURLs)
	if err != nil {
		return err
	}
	metadata := process.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO run_processes (
			run_id,
			process_id,
			command,
			args,
			cwd,
			status,
			pid,
			started_at,
			ended_at,
			exit_code,
			signal,
			preview_urls,
			metadata
		) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb)
		ON CONFLICT (run_id, process_id)
		DO UPDATE SET
			command = COALESCE(NULLIF(EXCLUDED.command, ''), run_processes.command),
			args = CASE
				WHEN jsonb_typeof(EXCLUDED.args) = 'array' AND jsonb_array_length(EXCLUDED.args) > 0 THEN EXCLUDED.args
				ELSE run_processes.args
			END,
			cwd = COALESCE(NULLIF(EXCLUDED.cwd, ''), run_processes.cwd),
			status = COALESCE(NULLIF(EXCLUDED.status, ''), run_processes.status),
			pid = CASE WHEN EXCLUDED.pid > 0 THEN EXCLUDED.pid ELSE run_processes.pid END,
			started_at = COALESCE(run_processes.started_at, EXCLUDED.started_at),
			ended_at = COALESCE(EXCLUDED.ended_at, run_processes.ended_at),
			exit_code = COALESCE(EXCLUDED.exit_code, run_processes.exit_code),
			signal = COALESCE(NULLIF(EXCLUDED.signal, ''), run_processes.signal),
			preview_urls = CASE
				WHEN jsonb_typeof(EXCLUDED.preview_urls) = 'array' AND jsonb_array_length(EXCLUDED.preview_urls) > 0
					THEN EXCLUDED.preview_urls
				ELSE run_processes.preview_urls
			END,
			metadata = run_processes.metadata || EXCLUDED.metadata
	`
	_, err = p.db.ExecContext(
		ctx,
		query,
		process.RunID,
		process.ProcessID,
		process.Command,
		argsBytes,
		nullString(process.Cwd),
		process.Status,
		process.PID,
		parseTimestampNull(process.StartedAt),
		parseTimestampNull(process.EndedAt),
		intPtrValue(process.ExitCode),
		nullString(process.Signal),
		previewBytes,
		metadataBytes,
	)
	return err
}

func (p *PostgresStore) GetRunProcess(ctx context.Context, runID string, processID string) (*store.RunProcess, error) {
	const query = `
		SELECT run_id,
			process_id,
			command,
			args,
			cwd,
			status,
			pid,
			started_at,
			ended_at,
			exit_code,
			signal,
			preview_urls,
			metadata
		FROM run_processes
		WHERE run_id = $1 AND process_id = $2
	`
	processes, err := p.queryRunProcesses(ctx, query, runID, processID)
	if err != nil {
		return nil, err
	}
	if len(processes) == 0 {
		return nil, nil
	}
	return &processes[0], nil
}

func (p *PostgresStore) ListRunProcesses(ctx context.Context, runID string) ([]store.RunProcess, error) {
	const query = `
		SELECT run_id,
			process_id,
			command,
			args,
			cwd,
			status,
			pid,
			started_at,
			ended_at,
			exit_code,
			signal,
			preview_urls,
			metadata
		FROM run_processes
		WHERE run_id = $1
		ORDER BY started_at DESC, process_id ASC
	`
	return p.queryRunProcesses(ctx, query, runID)
}

func (p *PostgresStore) NextSeq(ctx context.Context, runID string) (int64, error) {
	const query = `
		INSERT INTO run_event_sequences (run_id, last_seq)
		VALUES ($1, 1)
		ON CONFLICT (run_id)
		DO UPDATE SET last_seq = run_event_sequences.last_seq + 1
		RETURNING last_seq
	`
	var seq int64
	if err := p.db.QueryRowContext(ctx, query, runID).Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

func (p *PostgresStore) UpsertArtifact(ctx context.Context, artifact store.Artifact) error {
	metadata := artifact.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	labelsBytes, err := json.Marshal(artifact.Labels)
	if err != nil {
		return err
	}
	category := strings.TrimSpace(artifact.Category)
	if category == "" {
		category = "generic"
	}
	retentionClass := strings.TrimSpace(artifact.RetentionClass)
	if retentionClass == "" {
		retentionClass = "default"
	}
	const query = `
		INSERT INTO artifacts (
			id,
			run_id,
			type,
			category,
			uri,
			content_type,
			size_bytes,
			checksum,
			labels,
			searchable_text,
			retention_class,
			created_at,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13)
		ON CONFLICT (id)
		DO UPDATE SET
			type = EXCLUDED.type,
			category = EXCLUDED.category,
			uri = EXCLUDED.uri,
			content_type = EXCLUDED.content_type,
			size_bytes = EXCLUDED.size_bytes,
			checksum = EXCLUDED.checksum,
			labels = EXCLUDED.labels,
			searchable_text = EXCLUDED.searchable_text,
			retention_class = EXCLUDED.retention_class,
			metadata = EXCLUDED.metadata,
			created_at = EXCLUDED.created_at
	`
	_, err = p.db.ExecContext(
		ctx,
		query,
		artifact.ID,
		artifact.RunID,
		artifact.Type,
		category,
		artifact.URI,
		artifact.ContentType,
		artifact.SizeBytes,
		nullString(artifact.Checksum),
		labelsBytes,
		nullString(artifact.SearchableText),
		retentionClass,
		artifact.CreatedAt,
		encoded,
	)
	return err
}

func (p *PostgresStore) ListArtifacts(ctx context.Context, runID string) ([]store.Artifact, error) {
	const query = `
		SELECT id, run_id, type, category, uri, content_type, size_bytes, checksum, labels, searchable_text, retention_class, created_at, metadata
		FROM artifacts
		WHERE run_id = $1
		ORDER BY created_at ASC
	`
	rows, err := p.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.Artifact{}
	for rows.Next() {
		var createdAt time.Time
		var category sql.NullString
		var checksum sql.NullString
		var searchableText sql.NullString
		var retentionClass sql.NullString
		var labelsBytes []byte
		var metadataBytes []byte
		var artifact store.Artifact
		if err := rows.Scan(
			&artifact.ID,
			&artifact.RunID,
			&artifact.Type,
			&category,
			&artifact.URI,
			&artifact.ContentType,
			&artifact.SizeBytes,
			&checksum,
			&labelsBytes,
			&searchableText,
			&retentionClass,
			&createdAt,
			&metadataBytes,
		); err != nil {
			return nil, err
		}
		if category.Valid {
			artifact.Category = category.String
		}
		if checksum.Valid {
			artifact.Checksum = checksum.String
		}
		artifact.Labels = decodeStringSlice(labelsBytes)
		if searchableText.Valid {
			artifact.SearchableText = searchableText.String
		}
		if retentionClass.Valid {
			artifact.RetentionClass = retentionClass.String
		}
		artifact.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		if len(metadataBytes) > 0 {
			metadata := map[string]any{}
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				return nil, err
			}
			artifact.Metadata = metadata
		} else {
			artifact.Metadata = map[string]any{}
		}
		results = append(results, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) ListAutomations(ctx context.Context) ([]store.Automation, error) {
	const query = `
		SELECT id, name, prompt, model, days, time_of_day, timezone, enabled, next_run_at, last_run_at, in_progress, created_at, updated_at
		FROM automations
		ORDER BY updated_at DESC
	`
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]store.Automation, 0)
	for rows.Next() {
		var (
			item      store.Automation
			daysBytes []byte
			nextRunAt sql.NullTime
			lastRunAt sql.NullTime
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Prompt,
			&item.Model,
			&daysBytes,
			&item.TimeOfDay,
			&item.Timezone,
			&item.Enabled,
			&nextRunAt,
			&lastRunAt,
			&item.InProgress,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		item.Days = decodeStringSlice(daysBytes)
		if nextRunAt.Valid {
			item.NextRunAt = nextRunAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if lastRunAt.Valid {
			item.LastRunAt = lastRunAt.Time.UTC().Format(time.RFC3339Nano)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) GetAutomation(ctx context.Context, automationID string) (*store.Automation, error) {
	const query = `
		SELECT id, name, prompt, model, days, time_of_day, timezone, enabled, next_run_at, last_run_at, in_progress, created_at, updated_at
		FROM automations
		WHERE id = $1
	`
	var (
		item      store.Automation
		daysBytes []byte
		nextRunAt sql.NullTime
		lastRunAt sql.NullTime
		createdAt time.Time
		updatedAt time.Time
	)
	if err := p.db.QueryRowContext(ctx, query, automationID).Scan(
		&item.ID,
		&item.Name,
		&item.Prompt,
		&item.Model,
		&daysBytes,
		&item.TimeOfDay,
		&item.Timezone,
		&item.Enabled,
		&nextRunAt,
		&lastRunAt,
		&item.InProgress,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.Days = decodeStringSlice(daysBytes)
	if nextRunAt.Valid {
		item.NextRunAt = nextRunAt.Time.UTC().Format(time.RFC3339Nano)
	}
	if lastRunAt.Valid {
		item.LastRunAt = lastRunAt.Time.UTC().Format(time.RFC3339Nano)
	}
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &item, nil
}

func (p *PostgresStore) CreateAutomation(ctx context.Context, automation store.Automation) error {
	daysBytes, err := json.Marshal(automation.Days)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO automations (
			id, name, prompt, model, days, time_of_day, timezone, enabled, next_run_at, last_run_at, in_progress, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12, $13
		)
	`
	_, err = p.db.ExecContext(
		ctx,
		query,
		automation.ID,
		automation.Name,
		automation.Prompt,
		automation.Model,
		daysBytes,
		automation.TimeOfDay,
		automation.Timezone,
		automation.Enabled,
		parseTimestampNull(automation.NextRunAt),
		parseTimestampNull(automation.LastRunAt),
		automation.InProgress,
		parseTimestampValue(automation.CreatedAt),
		parseTimestampValue(automation.UpdatedAt),
	)
	return err
}

func (p *PostgresStore) UpdateAutomation(ctx context.Context, automation store.Automation) error {
	daysBytes, err := json.Marshal(automation.Days)
	if err != nil {
		return err
	}
	const query = `
		UPDATE automations
		SET
			name = $2,
			prompt = $3,
			model = $4,
			days = $5::jsonb,
			time_of_day = $6,
			timezone = $7,
			enabled = $8,
			next_run_at = $9,
			last_run_at = $10,
			in_progress = $11,
			updated_at = $12
		WHERE id = $1
	`
	_, err = p.db.ExecContext(
		ctx,
		query,
		automation.ID,
		automation.Name,
		automation.Prompt,
		automation.Model,
		daysBytes,
		automation.TimeOfDay,
		automation.Timezone,
		automation.Enabled,
		parseTimestampNull(automation.NextRunAt),
		parseTimestampNull(automation.LastRunAt),
		automation.InProgress,
		parseTimestampValue(automation.UpdatedAt),
	)
	return err
}

func (p *PostgresStore) DeleteAutomation(ctx context.Context, automationID string) error {
	_, err := p.db.ExecContext(ctx, "DELETE FROM automations WHERE id = $1", automationID)
	return err
}

func (p *PostgresStore) ListAutomationInbox(ctx context.Context, automationID string) ([]store.AutomationInboxEntry, error) {
	const query = `
		SELECT id, automation_id, run_id, status, phase, completion_reason, final_response, timed_out, error, unread, trigger, started_at, completed_at, diagnostics, created_at, updated_at
		FROM automation_inbox
		WHERE automation_id = $1
		ORDER BY started_at DESC, created_at DESC
	`
	rows, err := p.db.QueryContext(ctx, query, automationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]store.AutomationInboxEntry, 0)
	for rows.Next() {
		var (
			entry            store.AutomationInboxEntry
			runID            sql.NullString
			phase            sql.NullString
			completionReason sql.NullString
			finalResponse    sql.NullString
			errorValue       sql.NullString
			completedAt      sql.NullTime
			diagnosticsBytes []byte
			createdAt        time.Time
			updatedAt        time.Time
			startedAt        time.Time
		)
		if err := rows.Scan(
			&entry.ID,
			&entry.AutomationID,
			&runID,
			&entry.Status,
			&phase,
			&completionReason,
			&finalResponse,
			&entry.TimedOut,
			&errorValue,
			&entry.Unread,
			&entry.Trigger,
			&startedAt,
			&completedAt,
			&diagnosticsBytes,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		if runID.Valid {
			entry.RunID = runID.String
		}
		if phase.Valid {
			entry.Phase = phase.String
		}
		if completionReason.Valid {
			entry.CompletionReason = completionReason.String
		}
		if finalResponse.Valid {
			entry.FinalResponse = finalResponse.String
		}
		if errorValue.Valid {
			entry.Error = errorValue.String
		}
		entry.StartedAt = startedAt.UTC().Format(time.RFC3339Nano)
		if completedAt.Valid {
			entry.CompletedAt = completedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		entry.Diagnostics = decodeJSONMap(diagnosticsBytes)
		entry.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		entry.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		results = append(results, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresStore) CreateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	diagnostics := entry.Diagnostics
	if diagnostics == nil {
		diagnostics = map[string]any{}
	}
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO automation_inbox (
			id, automation_id, run_id, status, phase, completion_reason, final_response, timed_out, error, unread, trigger, started_at, completed_at, diagnostics, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15, $16
		)
	`
	_, err = p.db.ExecContext(
		ctx,
		query,
		entry.ID,
		entry.AutomationID,
		nullString(entry.RunID),
		entry.Status,
		nullString(entry.Phase),
		nullString(entry.CompletionReason),
		nullString(entry.FinalResponse),
		entry.TimedOut,
		nullString(entry.Error),
		entry.Unread,
		entry.Trigger,
		parseTimestampValue(entry.StartedAt),
		parseTimestampNull(entry.CompletedAt),
		encoded,
		parseTimestampValue(entry.CreatedAt),
		parseTimestampValue(entry.UpdatedAt),
	)
	return err
}

func (p *PostgresStore) UpdateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	diagnostics := entry.Diagnostics
	if diagnostics == nil {
		diagnostics = map[string]any{}
	}
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		return err
	}
	const query = `
		UPDATE automation_inbox
		SET
			run_id = $3,
			status = $4,
			phase = $5,
			completion_reason = $6,
			final_response = $7,
			timed_out = $8,
			error = $9,
			unread = $10,
			trigger = $11,
			started_at = $12,
			completed_at = $13,
			diagnostics = $14::jsonb,
			updated_at = $15
		WHERE automation_id = $1 AND id = $2
	`
	_, err = p.db.ExecContext(
		ctx,
		query,
		entry.AutomationID,
		entry.ID,
		nullString(entry.RunID),
		entry.Status,
		nullString(entry.Phase),
		nullString(entry.CompletionReason),
		nullString(entry.FinalResponse),
		entry.TimedOut,
		nullString(entry.Error),
		entry.Unread,
		entry.Trigger,
		parseTimestampValue(entry.StartedAt),
		parseTimestampNull(entry.CompletedAt),
		encoded,
		parseTimestampValue(entry.UpdatedAt),
	)
	return err
}

func (p *PostgresStore) MarkAutomationInboxEntryRead(ctx context.Context, automationID string, entryID string) error {
	_, err := p.db.ExecContext(
		ctx,
		`UPDATE automation_inbox SET unread = FALSE, updated_at = NOW() WHERE automation_id = $1 AND id = $2`,
		automationID,
		entryID,
	)
	return err
}

func (p *PostgresStore) MarkAutomationInboxReadAll(ctx context.Context, automationID string) error {
	_, err := p.db.ExecContext(
		ctx,
		`UPDATE automation_inbox SET unread = FALSE, updated_at = NOW() WHERE automation_id = $1`,
		automationID,
	)
	return err
}

func (p *PostgresStore) insertMemoryEntry(ctx context.Context, entry store.MemoryEntry, metadata []byte) (bool, error) {
	query := `
		INSERT INTO memory_entries (id, content, metadata, embedding, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $5)
	`
	params := []any{entry.ID, entry.Content, metadata, entry.CreatedAt, entry.UpdatedAt}
	if len(entry.Embedding) == memoryEmbeddingDimensions {
		query = `
			INSERT INTO memory_entries (id, content, metadata, embedding, created_at, updated_at)
			VALUES ($1, $2, $3, $4::vector, $5, $6)
		`
		params = []any{entry.ID, entry.Content, metadata, formatVector(entry.Embedding), entry.CreatedAt, entry.UpdatedAt}
	}
	if _, err := p.db.ExecContext(ctx, query, params...); err != nil {
		return false, err
	}
	return true, nil
}

func (p *PostgresStore) insertMemoryEntryWithFingerprint(ctx context.Context, entry store.MemoryEntry, metadata []byte, fingerprint string) (bool, error) {
	query := `
		WITH inserted AS (
			INSERT INTO memory_entries (id, content, metadata, embedding, created_at, updated_at)
			SELECT $1, $2, $3, NULL, $4, $5
			WHERE NOT EXISTS (
				SELECT 1 FROM memory_entries WHERE metadata->>'fingerprint' = $6
			)
			RETURNING id
		)
		SELECT COUNT(*) FROM inserted
	`
	params := []any{entry.ID, entry.Content, metadata, entry.CreatedAt, entry.UpdatedAt, fingerprint}
	if len(entry.Embedding) == memoryEmbeddingDimensions {
		query = `
			WITH inserted AS (
				INSERT INTO memory_entries (id, content, metadata, embedding, created_at, updated_at)
				SELECT $1, $2, $3, $4::vector, $5, $6
				WHERE NOT EXISTS (
					SELECT 1 FROM memory_entries WHERE metadata->>'fingerprint' = $7
				)
				RETURNING id
			)
			SELECT COUNT(*) FROM inserted
		`
		params = []any{entry.ID, entry.Content, metadata, formatVector(entry.Embedding), entry.CreatedAt, entry.UpdatedAt, fingerprint}
	}
	var insertedCount int
	if err := p.db.QueryRowContext(ctx, query, params...).Scan(&insertedCount); err != nil {
		return false, err
	}
	return insertedCount > 0, nil
}

func upsertRunStepTx(ctx context.Context, tx *sql.Tx, step store.RunStep) error {
	if strings.TrimSpace(step.RunID) == "" || strings.TrimSpace(step.ID) == "" {
		return nil
	}
	if strings.TrimSpace(step.Name) == "" {
		step.Name = step.ID
	}
	if strings.TrimSpace(step.Kind) == "" {
		step.Kind = "step"
	}
	if strings.TrimSpace(step.Status) == "" {
		step.Status = "running"
	}
	if strings.TrimSpace(step.PolicyDecision) == "" {
		step.PolicyDecision = "unknown"
	}
	diagnostics := step.Diagnostics
	if diagnostics == nil {
		diagnostics = map[string]any{}
	}
	diagnostics["kind"] = step.Kind
	if step.Source != "" {
		diagnostics["source"] = step.Source
	}
	if step.Seq > 0 {
		diagnostics["seq"] = step.Seq
	}
	if step.Error != "" {
		diagnostics["error"] = step.Error
	}
	if step.PlanID != "" {
		diagnostics["plan_id"] = step.PlanID
	}
	if step.Attempt > 0 {
		diagnostics["attempt"] = step.Attempt
	}
	if step.PolicyDecision != "" {
		diagnostics["policy_decision"] = step.PolicyDecision
	}
	dependenciesBytes, err := json.Marshal(step.Dependencies)
	if err != nil {
		return err
	}
	expectedArtifactsBytes, err := json.Marshal(step.ExpectedArtifacts)
	if err != nil {
		return err
	}
	diagnosticsBytes, err := json.Marshal(diagnostics)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO run_steps (
			run_id,
			step_id,
			parent_step_id,
			name,
			status,
			plan_id,
			attempt,
			policy_decision,
			dependencies,
			expected_artifacts,
			diagnostics,
			started_at,
			completed_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, $11::jsonb, $12, $13, NOW(), NOW())
		ON CONFLICT (run_id, step_id)
		DO UPDATE SET
			parent_step_id = COALESCE(run_steps.parent_step_id, EXCLUDED.parent_step_id),
			name = COALESCE(NULLIF(EXCLUDED.name, ''), run_steps.name),
			status = EXCLUDED.status,
			plan_id = COALESCE(NULLIF(EXCLUDED.plan_id, ''), run_steps.plan_id),
			attempt = CASE WHEN EXCLUDED.attempt > 0 THEN EXCLUDED.attempt ELSE run_steps.attempt END,
			policy_decision = COALESCE(NULLIF(EXCLUDED.policy_decision, ''), run_steps.policy_decision),
			dependencies = CASE
				WHEN jsonb_typeof(EXCLUDED.dependencies) = 'array' AND jsonb_array_length(EXCLUDED.dependencies) > 0 THEN EXCLUDED.dependencies
				ELSE run_steps.dependencies
			END,
			expected_artifacts = CASE
				WHEN jsonb_typeof(EXCLUDED.expected_artifacts) = 'array' AND jsonb_array_length(EXCLUDED.expected_artifacts) > 0 THEN EXCLUDED.expected_artifacts
				ELSE run_steps.expected_artifacts
			END,
			diagnostics = run_steps.diagnostics || EXCLUDED.diagnostics,
			started_at = COALESCE(run_steps.started_at, EXCLUDED.started_at),
			completed_at = COALESCE(EXCLUDED.completed_at, run_steps.completed_at),
			updated_at = NOW()
	`
	_, err = tx.ExecContext(
		ctx,
		query,
		step.RunID,
		step.ID,
		nullString(step.ParentStepID),
		step.Name,
		step.Status,
		nullString(step.PlanID),
		step.Attempt,
		nullString(step.PolicyDecision),
		dependenciesBytes,
		expectedArtifactsBytes,
		diagnosticsBytes,
		parseTimestampNull(step.StartedAt),
		parseTimestampNull(step.CompletedAt),
	)
	return err
}

func applyRunStateUpdateTx(ctx context.Context, tx *sql.Tx, event store.RunEvent) error {
	eventType := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(event.Type)), "_", ".")
	if eventType == "" {
		return nil
	}
	phase := ""
	status := ""
	completionReason := ""
	resumedFrom := ""

	switch eventType {
	case "run.started":
		status = "running"
		phase = "planning"
	case "run.phase.changed":
		phase = readDiagString(event.Payload, "phase")
	case "run.completed":
		status = "completed"
		phase = "completed"
		completionReason = readDiagString(event.Payload, "completion_reason")
	case "run.partial":
		status = "partial"
		phase = "completed"
		completionReason = readDiagString(event.Payload, "completion_reason")
	case "run.failed":
		status = "failed"
		phase = "failed"
		completionReason = readDiagString(event.Payload, "completion_reason")
		if completionReason == "" {
			completionReason = "activity_error"
		}
	case "run.cancelled":
		status = "cancelled"
		phase = "cancelled"
		completionReason = "user_cancelled"
	case "run.resumed":
		status = "running"
		phase = "planning"
		resumedFrom = readDiagString(event.Payload, "resumed_from")
	default:
		return nil
	}

	query := `
		UPDATE runs
		SET
			status = COALESCE(NULLIF($2, ''), status),
			phase = COALESCE(NULLIF($3, ''), phase),
			completion_reason = CASE
				WHEN NULLIF($4, '') IS NOT NULL THEN $4
				ELSE completion_reason
			END,
			resumed_from = CASE
				WHEN NULLIF($5, '') IS NOT NULL THEN $5::uuid
				ELSE resumed_from
			END,
			checkpoint_seq = GREATEST(checkpoint_seq, $6),
			updated_at = $7
		WHERE id = $1
	`
	_, err := tx.ExecContext(
		ctx,
		query,
		event.RunID,
		status,
		phase,
		nullString(completionReason),
		nullString(resumedFrom),
		event.Seq,
		parseTimestampValue(event.Timestamp),
	)
	return err
}

func parseTimestampValue(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Now().UTC()
	}
	return parsed.UTC()
}

func parseTimestampNull(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return parsed.UTC()
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func decodeStringSlice(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	values := []string{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func decodeJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func readDiagString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func readDiagInt64(payload map[string]any, key string) int64 {
	if payload == nil {
		return 0
	}
	value, ok := payload[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
	}
	return 0
}

func intPtrValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func (p *PostgresStore) queryRunProcesses(ctx context.Context, query string, args ...any) ([]store.RunProcess, error) {
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []store.RunProcess{}
	for rows.Next() {
		var (
			process      store.RunProcess
			argsBytes    []byte
			previewBytes []byte
			metaBytes    []byte
			cwd          sql.NullString
			status       sql.NullString
			pid          sql.NullInt64
			startedAt    sql.NullTime
			endedAt      sql.NullTime
			exitCode     sql.NullInt64
			signal       sql.NullString
		)
		if err := rows.Scan(
			&process.RunID,
			&process.ProcessID,
			&process.Command,
			&argsBytes,
			&cwd,
			&status,
			&pid,
			&startedAt,
			&endedAt,
			&exitCode,
			&signal,
			&previewBytes,
			&metaBytes,
		); err != nil {
			return nil, err
		}
		process.Args = decodeStringSlice(argsBytes)
		if cwd.Valid {
			process.Cwd = cwd.String
		}
		if status.Valid {
			process.Status = status.String
		}
		if pid.Valid {
			process.PID = int(pid.Int64)
		}
		if startedAt.Valid {
			process.StartedAt = startedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if endedAt.Valid {
			process.EndedAt = endedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if exitCode.Valid {
			code := int(exitCode.Int64)
			process.ExitCode = &code
		}
		if signal.Valid {
			process.Signal = signal.String
		}
		process.PreviewURLs = decodeStringSlice(previewBytes)
		process.Metadata = decodeJSONMap(metaBytes)
		results = append(results, process)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func formatVector(values []float32) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%g", value))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

const memoryEmbeddingDimensions = 1536

func readMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
