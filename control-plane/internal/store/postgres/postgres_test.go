package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	storepkg "github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	testDB   *sql.DB
	testConn string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, err := tcpostgres.Run(
		ctx,
		"pgvector/pgvector:pg16",
		tcpostgres.WithDatabase("gavryn"),
		tcpostgres.WithUsername("gavryn"),
		tcpostgres.WithPassword("gavryn"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres container:", err)
		os.Exit(1)
	}
	conn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "connection string:", err)
		os.Exit(1)
	}
	ldb, err := sql.Open("pgx", conn)
	if err != nil {
		_ = container.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	if err := waitForDB(ldb); err != nil {
		_ = ldb.Close()
		_ = container.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "ping db:", err)
		os.Exit(1)
	}
	if err := applyMigrations(ctx, ldb); err != nil {
		_ = ldb.Close()
		_ = container.Terminate(ctx)
		fmt.Fprintln(os.Stderr, "apply migrations:", err)
		os.Exit(1)
	}
	testDB = ldb
	testConn = conn
	code := m.Run()
	_ = ldb.Close()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	migrationsDir := filepath.Join(root, "infra", "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	for _, name := range files {
		path := filepath.Join(migrationsDir, name)
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func waitForDB(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var lastErr error
	for i := 0; i < 20; i++ {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return lastErr
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve repo root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..")), nil
}

func cleanDB(t *testing.T) {
	t.Helper()
	_, err := testDB.Exec(`TRUNCATE TABLE
		run_events,
		run_event_sequences,
		run_steps,
		run_processes,
		messages,
		tool_invocations,
		artifacts,
		runs,
		llm_settings,
		skill_files,
		skills,
		automation_inbox,
		automations,
		context_nodes,
		memory_settings,
		memory_entries,
		personality_settings
		CASCADE`)
	if err != nil {
		t.Fatalf("clean db: %v", err)
	}
}

func newStore(t *testing.T) *PostgresStore {
	t.Helper()
	cleanDB(t)
	return &PostgresStore{db: testDB}
}

func TestNew_Success(t *testing.T) {
	pgStore, err := New(testConn)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if pgStore == nil {
		t.Fatalf("expected store")
	}
	_ = pgStore.db.Close()
}

func TestNew_SchemaVerification(t *testing.T) {
	ctx := context.Background()
	if err := verifySchema(ctx, testDB); err != nil {
		t.Fatalf("verify schema: %v", err)
	}

	required := []string{"runs", "messages", "run_events", "run_event_sequences", "run_steps", "run_processes", "llm_settings", "skills", "skill_files", "context_nodes", "memory_settings", "memory_entries", "personality_settings", "artifacts", "automations", "automation_inbox"}
	for _, table := range required {
		var regclass sql.NullString
		if err := testDB.QueryRowContext(ctx, "SELECT to_regclass($1)", fmt.Sprintf("public.%s", table)).Scan(&regclass); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !regclass.Valid {
			t.Fatalf("expected table %s to exist", table)
		}
	}
}

func TestNew_SchemaMissingTable(t *testing.T) {
	ctx := context.Background()
	cleanDB(t)

	_, err := testDB.ExecContext(ctx, "DROP TABLE IF EXISTS memory_entries")
	if err != nil {
		t.Fatalf("drop table: %v", err)
	}
	_, err = New(testConn)
	if err == nil {
		t.Fatalf("expected schema verification error")
	}
	if err := applyMigrations(ctx, testDB); err != nil {
		t.Fatalf("restore migrations: %v", err)
	}
}

func TestNew_ErrorConnection(t *testing.T) {
	_, err := New("postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable")
	if err == nil {
		t.Fatalf("expected connection error")
	}
}

func TestNew_InvalidDSN(t *testing.T) {
	_, err := New("invalid dsn")
	if err == nil {
		t.Fatalf("expected dsn parse error")
	}
}

func TestNew_OpenError(t *testing.T) {
	prev := openDB
	openDB = func(driverName string, dataSourceName string) (*sql.DB, error) {
		return nil, errors.New("open error")
	}
	defer func() { openDB = prev }()

	if _, err := New("postgres://example"); err == nil {
		t.Fatalf("expected open error")
	}
}

func TestCreateRun(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC()
	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := pgStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	var status string
	if err := testDB.QueryRowContext(ctx, "SELECT status FROM runs WHERE id = $1", run.ID).Scan(&status); err != nil {
		t.Fatalf("query run: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected status running, got %q", status)
	}
}

func TestAddMessage(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	msg := storepkg.Message{ID: uuid.NewString(), RunID: run.ID, Role: "user", Content: "hi", Sequence: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"k": "v"}}
	if err := pgStore.AddMessage(ctx, msg); err != nil {
		t.Fatalf("add message: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE run_id = $1", run.ID).Scan(&count); err != nil {
		t.Fatalf("query messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 message, got %d", count)
	}
}

func TestListRuns_UsesGeneratedTitleEvent(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, pgStore.CreateRun(ctx, run))
	require.NoError(t, pgStore.AddMessage(ctx, storepkg.Message{
		ID:        uuid.NewString(),
		RunID:     run.ID,
		Role:      "user",
		Content:   "very long first message that should not be used as final sidebar title",
		Sequence:  1,
		CreatedAt: now,
	}))
	require.NoError(t, pgStore.AppendEvent(ctx, storepkg.RunEvent{
		RunID:     run.ID,
		Seq:       1,
		Type:      "run.title.updated",
		Timestamp: now,
		Source:    "llm",
		Payload:   map[string]any{"title": "Generated Sidebar Title"},
	}))

	runs, err := pgStore.ListRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "Generated Sidebar Title", runs[0].Title)
}

func TestAddMessage_MetadataNil(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	msg := storepkg.Message{ID: uuid.NewString(), RunID: run.ID, Role: "user", Content: "hi", Sequence: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.AddMessage(ctx, msg); err != nil {
		t.Fatalf("add message: %v", err)
	}
}

func TestAddMessage_MetadataMarshalError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	msg := storepkg.Message{ID: uuid.NewString(), RunID: run.ID, Role: "user", Content: "hi", Sequence: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"bad": func() {}}}
	if err := pgStore.AddMessage(ctx, msg); err == nil {
		t.Fatalf("expected metadata marshal error")
	}
}

func TestAddMessage_ErrorMissingRun(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	msg := storepkg.Message{ID: uuid.NewString(), RunID: uuid.NewString(), Role: "user", Content: "hi", Sequence: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.AddMessage(ctx, msg); err == nil {
		t.Fatalf("expected error when run is missing")
	}

	var count int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages").Scan(&count); err != nil {
		t.Fatalf("query messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no messages after failure")
	}
}

func TestListMessages(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	msg1 := storepkg.Message{ID: uuid.NewString(), RunID: run.ID, Role: "user", Content: "first", Sequence: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Metadata: map[string]any{"a": "b"}}
	if err := pgStore.AddMessage(ctx, msg1); err != nil {
		t.Fatalf("add message: %v", err)
	}

	var metadata any = nil
	_, err := testDB.ExecContext(ctx, "INSERT INTO messages (id, run_id, role, content, sequence, created_at, metadata) VALUES ($1, $2, $3, $4, $5, $6, $7)", uuid.NewString(), run.ID, "assistant", "second", 2, time.Now().UTC(), metadata)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	msgs, err := pgStore.ListMessages(ctx, run.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Sequence != 1 || msgs[1].Sequence != 2 {
		t.Fatalf("expected messages ordered by sequence")
	}
	if msgs[1].Metadata == nil || len(msgs[1].Metadata) != 0 {
		t.Fatalf("expected empty metadata map for null metadata")
	}
}

func TestArtifacts(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.New().String(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	artifact := storepkg.Artifact{
		ID:          uuid.New().String(),
		RunID:       run.ID,
		Type:        "file",
		URI:         "http://example.com/report.pdf",
		ContentType: "application/pdf",
		SizeBytes:   1234,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Metadata:    map[string]any{"source": "test"},
	}
	if err := pgStore.UpsertArtifact(ctx, artifact); err != nil {
		t.Fatalf("upsert artifact: %v", err)
	}

	artifact.Type = "document"
	if err := pgStore.UpsertArtifact(ctx, artifact); err != nil {
		t.Fatalf("upsert artifact: %v", err)
	}

	artifacts, err := pgStore.ListArtifacts(ctx, run.ID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Type != "document" {
		t.Fatalf("expected updated artifact type")
	}
}

func TestRunProcesses(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	require.NoError(t, pgStore.CreateRun(ctx, run))

	exitCode := 0
	process := storepkg.RunProcess{
		RunID:       run.ID,
		ProcessID:   "proc-1",
		Command:     "npm",
		Args:        []string{"run", "dev"},
		Cwd:         ".",
		Status:      "running",
		PID:         12345,
		StartedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		PreviewURLs: []string{"http://localhost:3000"},
		Metadata:    map[string]any{"tool_name": "process.start"},
	}
	require.NoError(t, pgStore.UpsertRunProcess(ctx, process))

	process.Status = "exited"
	process.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
	process.ExitCode = &exitCode
	process.Signal = "SIGTERM"
	require.NoError(t, pgStore.UpsertRunProcess(ctx, process))

	listed, err := pgStore.ListRunProcesses(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "proc-1", listed[0].ProcessID)
	require.Equal(t, "exited", listed[0].Status)
	require.NotNil(t, listed[0].ExitCode)
	require.Equal(t, 0, *listed[0].ExitCode)
	require.Equal(t, "SIGTERM", listed[0].Signal)

	found, err := pgStore.GetRunProcess(ctx, run.ID, "proc-1")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "npm", found.Command)
	require.Equal(t, "exited", found.Status)
}

func TestListMessages_InvalidMetadata(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	_, err := testDB.ExecContext(ctx, "INSERT INTO messages (id, run_id, role, content, sequence, created_at, metadata) VALUES ($1, $2, $3, $4, $5, $6, $7)", uuid.NewString(), run.ID, "user", "bad", 1, time.Now().UTC(), []byte("[]"))
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if _, err := pgStore.ListMessages(ctx, run.ID); err == nil {
		t.Fatalf("expected metadata unmarshal error")
	}
}

func TestListMessages_ClosedDB(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("pgx", testConn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	pgStore := &PostgresStore{db: conn}
	_ = conn.Close()

	if _, err := pgStore.ListMessages(ctx, uuid.NewString()); err == nil {
		t.Fatalf("expected error on closed db")
	}
}

func TestGetLLMSettings(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	settings, err := pgStore.GetLLMSettings(ctx)
	if err != nil {
		t.Fatalf("get llm settings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil settings when empty")
	}
}

func TestGetLLMSettings_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	_, err := testDB.ExecContext(ctx, "INSERT INTO llm_settings (id, mode, provider, model, base_url, api_key_enc, codex_auth_path, codex_home, created_at, updated_at) VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9)", "live", "openai", "gpt-4o", nil, nil, nil, nil, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatalf("insert llm settings: %v", err)
	}

	if _, err := pgStore.GetLLMSettings(ctx); err == nil {
		t.Fatalf("expected scan error for null columns")
	}
}

func TestUpsertLLMSettings(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC()
	settings := storepkg.LLMSettings{
		Mode:          "live",
		Provider:      "openai",
		Model:         "gpt-4o",
		BaseURL:       "https://api.openai.com",
		APIKeyEnc:     "enc",
		CodexAuthPath: "/tmp/auth",
		CodexHome:     "/tmp/home",
		CreatedAt:     now.Format(time.RFC3339Nano),
		UpdatedAt:     now.Format(time.RFC3339Nano),
	}
	if err := pgStore.UpsertLLMSettings(ctx, settings); err != nil {
		t.Fatalf("upsert llm settings: %v", err)
	}

	settings.Provider = "local"
	settings.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := pgStore.UpsertLLMSettings(ctx, settings); err != nil {
		t.Fatalf("upsert llm settings: %v", err)
	}

	fetched, err := pgStore.GetLLMSettings(ctx)
	if err != nil {
		t.Fatalf("get llm settings: %v", err)
	}
	if fetched == nil || fetched.Provider != "local" {
		t.Fatalf("expected updated settings")
	}
}

func TestListSkills(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_ = pgStore.CreateSkill(ctx, storepkg.Skill{ID: uuid.NewString(), Name: "beta", CreatedAt: now, UpdatedAt: now})
	_ = pgStore.CreateSkill(ctx, storepkg.Skill{ID: uuid.NewString(), Name: "alpha", CreatedAt: now, UpdatedAt: now})

	skills, err := pgStore.ListSkills(ctx)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Name != "alpha" || skills[1].Name != "beta" {
		t.Fatalf("expected skills ordered by name")
	}
}

func TestListSkills_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	_, err := testDB.ExecContext(ctx, "INSERT INTO skills (id, name, description, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)", uuid.NewString(), "alpha", nil, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatalf("insert skill: %v", err)
	}

	if _, err := pgStore.ListSkills(ctx); err == nil {
		t.Fatalf("expected scan error for null description")
	}
}

func TestGetSkill(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	missing, err := pgStore.GetSkill(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing skill")
	}

	name := "alpha"
	skillID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := pgStore.CreateSkill(ctx, storepkg.Skill{ID: skillID, Name: name, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	got, err := pgStore.GetSkill(ctx, skillID)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if got == nil || got.Name != name {
		t.Fatalf("expected skill to be returned")
	}
}

func TestGetSkill_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skillID := uuid.NewString()
	_, err := testDB.ExecContext(ctx, "INSERT INTO skills (id, name, description, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)", skillID, "alpha", nil, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatalf("insert skill: %v", err)
	}

	if _, err := pgStore.GetSkill(ctx, skillID); err == nil {
		t.Fatalf("expected scan error for null description")
	}
}

func TestCreateSkill(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skill := storepkg.Skill{ID: uuid.NewString(), Name: "alpha", Description: "desc", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateSkill(ctx, skill); err != nil {
		t.Fatalf("create skill: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM skills").Scan(&count); err != nil {
		t.Fatalf("query skills: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 skill, got %d", count)
	}

	dup := storepkg.Skill{ID: uuid.NewString(), Name: "alpha", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pgStore.CreateSkill(ctx, dup); err == nil {
		t.Fatalf("expected error on duplicate skill name")
	}
}

func TestUpdateSkill(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skillID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := pgStore.CreateSkill(ctx, storepkg.Skill{ID: skillID, Name: "alpha", Description: "old", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create skill: %v", err)
	}

	if err := pgStore.UpdateSkill(ctx, storepkg.Skill{ID: skillID, Name: "beta", Description: "new", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("update skill: %v", err)
	}

	updated, _ := pgStore.GetSkill(ctx, skillID)
	if updated == nil || updated.Name != "beta" || updated.Description != "new" {
		t.Fatalf("expected skill to be updated")
	}
}

func TestDeleteSkill(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skillID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := pgStore.CreateSkill(ctx, storepkg.Skill{ID: skillID, Name: "alpha", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create skill: %v", err)
	}

	if err := pgStore.DeleteSkill(ctx, skillID); err != nil {
		t.Fatalf("delete skill: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM skills WHERE id = $1", skillID).Scan(&count); err != nil {
		t.Fatalf("query skills: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected skill to be deleted")
	}
}

func TestListSkillFiles(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skillID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_ = pgStore.CreateSkill(ctx, storepkg.Skill{ID: skillID, Name: "alpha", CreatedAt: now, UpdatedAt: now})
	_ = pgStore.UpsertSkillFile(ctx, storepkg.SkillFile{ID: uuid.NewString(), SkillID: skillID, Path: "b.txt", Content: []byte("b"), ContentType: "text/plain", SizeBytes: 1, CreatedAt: now, UpdatedAt: now})
	_ = pgStore.UpsertSkillFile(ctx, storepkg.SkillFile{ID: uuid.NewString(), SkillID: skillID, Path: "a.txt", Content: []byte("a"), ContentType: "text/plain", SizeBytes: 1, CreatedAt: now, UpdatedAt: now})

	files, err := pgStore.ListSkillFiles(ctx, skillID)
	if err != nil {
		t.Fatalf("list skill files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path != "a.txt" || files[1].Path != "b.txt" {
		t.Fatalf("expected files ordered by path")
	}
}

func TestListSkillFiles_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skillID := uuid.NewString()
	_ = pgStore.CreateSkill(ctx, storepkg.Skill{ID: skillID, Name: "alpha", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	_, err := testDB.ExecContext(ctx, "INSERT INTO skill_files (id, skill_id, path, content, content_type, size_bytes, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)", uuid.NewString(), skillID, "bad.txt", []byte("x"), nil, 1, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatalf("insert skill file: %v", err)
	}

	if _, err := pgStore.ListSkillFiles(ctx, skillID); err == nil {
		t.Fatalf("expected scan error for null content_type")
	}
}

func TestUpsertSkillFile(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skillID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_ = pgStore.CreateSkill(ctx, storepkg.Skill{ID: skillID, Name: "alpha", CreatedAt: now, UpdatedAt: now})

	file := storepkg.SkillFile{ID: uuid.NewString(), SkillID: skillID, Path: "readme.md", Content: []byte("one"), ContentType: "text/plain", SizeBytes: 3, CreatedAt: now, UpdatedAt: now}
	if err := pgStore.UpsertSkillFile(ctx, file); err != nil {
		t.Fatalf("upsert skill file: %v", err)
	}

	file.Content = []byte("two")
	file.SizeBytes = 3
	file.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := pgStore.UpsertSkillFile(ctx, file); err != nil {
		t.Fatalf("upsert skill file: %v", err)
	}

	files, _ := pgStore.ListSkillFiles(ctx, skillID)
	if len(files) != 1 || string(files[0].Content) != "two" {
		t.Fatalf("expected skill file to be updated")
	}
}

func TestDeleteSkillFile(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	skillID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_ = pgStore.CreateSkill(ctx, storepkg.Skill{ID: skillID, Name: "alpha", CreatedAt: now, UpdatedAt: now})
	_ = pgStore.UpsertSkillFile(ctx, storepkg.SkillFile{ID: uuid.NewString(), SkillID: skillID, Path: "readme.md", Content: []byte("x"), ContentType: "text/plain", SizeBytes: 1, CreatedAt: now, UpdatedAt: now})

	if err := pgStore.DeleteSkillFile(ctx, skillID, "readme.md"); err != nil {
		t.Fatalf("delete skill file: %v", err)
	}

	files, _ := pgStore.ListSkillFiles(ctx, skillID)
	if len(files) != 0 {
		t.Fatalf("expected no files after delete")
	}
}

func TestListContextNodes(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	rootID := uuid.NewString()
	childID := uuid.NewString()
	_ = pgStore.CreateContextFolder(ctx, storepkg.ContextNode{ID: rootID, Name: "root", NodeType: "folder", CreatedAt: now, UpdatedAt: now})
	_ = pgStore.CreateContextFolder(ctx, storepkg.ContextNode{ID: childID, ParentID: rootID, Name: "child", NodeType: "folder", CreatedAt: now, UpdatedAt: now})

	nodes, err := pgStore.ListContextNodes(ctx)
	if err != nil {
		t.Fatalf("list context nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestListContextNodes_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	_, err := testDB.ExecContext(ctx, "INSERT INTO context_nodes (id, parent_id, name, node_type, content, content_type, size_bytes, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)", uuid.NewString(), nil, "root", "folder", nil, nil, nil, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatalf("insert context node: %v", err)
	}

	if _, err := pgStore.ListContextNodes(ctx); err == nil {
		t.Fatalf("expected scan error for null size_bytes")
	}
}

func TestGetContextFile(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	missing, err := pgStore.GetContextFile(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("get context file: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing context file")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	parentID := uuid.NewString()
	_ = pgStore.CreateContextFolder(ctx, storepkg.ContextNode{ID: parentID, Name: "root", NodeType: "folder", CreatedAt: now, UpdatedAt: now})
	nodeID := uuid.NewString()
	if err := pgStore.CreateContextFile(ctx, storepkg.ContextNode{ID: nodeID, ParentID: parentID, Name: "file.txt", NodeType: "file", Content: []byte("ok"), ContentType: "text/plain", SizeBytes: 2, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create context file: %v", err)
	}
	got, err := pgStore.GetContextFile(ctx, nodeID)
	if err != nil {
		t.Fatalf("get context file: %v", err)
	}
	if got == nil || string(got.Content) != "ok" || got.ParentID != parentID {
		t.Fatalf("expected context file content")
	}
}

func TestGetContextFile_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	nodeID := uuid.NewString()
	_, err := testDB.ExecContext(ctx, "INSERT INTO context_nodes (id, parent_id, name, node_type, content, content_type, size_bytes, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)", nodeID, nil, "file", "file", []byte("x"), nil, nil, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatalf("insert context node: %v", err)
	}

	if _, err := pgStore.GetContextFile(ctx, nodeID); err == nil {
		t.Fatalf("expected scan error for null columns")
	}
}

func TestCreateContextFolder(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	nodeID := uuid.NewString()
	if err := pgStore.CreateContextFolder(ctx, storepkg.ContextNode{ID: nodeID, Name: "root", NodeType: "folder", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create context folder: %v", err)
	}

	var parentID sql.NullString
	if err := testDB.QueryRowContext(ctx, "SELECT parent_id FROM context_nodes WHERE id = $1", nodeID).Scan(&parentID); err != nil {
		t.Fatalf("query context node: %v", err)
	}
	if parentID.Valid {
		t.Fatalf("expected null parent_id for root node")
	}
}

func TestCreateContextFile(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	parentID := uuid.NewString()
	_ = pgStore.CreateContextFolder(ctx, storepkg.ContextNode{ID: parentID, Name: "root", NodeType: "folder", CreatedAt: now, UpdatedAt: now})

	fileID := uuid.NewString()
	if err := pgStore.CreateContextFile(ctx, storepkg.ContextNode{ID: fileID, ParentID: parentID, Name: "file.txt", NodeType: "file", Content: []byte("data"), ContentType: "text/plain", SizeBytes: 4, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create context file: %v", err)
	}

	var name string
	if err := testDB.QueryRowContext(ctx, "SELECT name FROM context_nodes WHERE id = $1", fileID).Scan(&name); err != nil {
		t.Fatalf("query context file: %v", err)
	}
	if name != "file.txt" {
		t.Fatalf("expected file.txt, got %q", name)
	}
}

func TestDeleteContextNode(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	rootID := uuid.NewString()
	childID := uuid.NewString()
	_ = pgStore.CreateContextFolder(ctx, storepkg.ContextNode{ID: rootID, Name: "root", NodeType: "folder", CreatedAt: now, UpdatedAt: now})
	_ = pgStore.CreateContextFolder(ctx, storepkg.ContextNode{ID: childID, ParentID: rootID, Name: "child", NodeType: "folder", CreatedAt: now, UpdatedAt: now})

	if err := pgStore.DeleteContextNode(ctx, rootID); err != nil {
		t.Fatalf("delete context node: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM context_nodes").Scan(&count); err != nil {
		t.Fatalf("query context nodes: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected all nodes deleted")
	}
}

func TestGetMemorySettings(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	settings, err := pgStore.GetMemorySettings(ctx)
	if err != nil {
		t.Fatalf("get memory settings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil settings")
	}
}

func TestGetMemorySettings_ClosedDB(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("pgx", testConn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	pgStore := &PostgresStore{db: conn}
	_ = conn.Close()

	if _, err := pgStore.GetMemorySettings(ctx); err == nil {
		t.Fatalf("expected error on closed db")
	}
}

func TestUpsertMemorySettings(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := pgStore.UpsertMemorySettings(ctx, storepkg.MemorySettings{Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert memory settings: %v", err)
	}
	if err := pgStore.UpsertMemorySettings(ctx, storepkg.MemorySettings{Enabled: false, CreatedAt: now, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("upsert memory settings: %v", err)
	}
	settings, err := pgStore.GetMemorySettings(ctx)
	if err != nil {
		t.Fatalf("get memory settings: %v", err)
	}
	if settings == nil || settings.Enabled {
		t.Fatalf("expected memory settings to be updated")
	}
}

func TestGetPersonalitySettings(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	settings, err := pgStore.GetPersonalitySettings(ctx)
	if err != nil {
		t.Fatalf("get personality settings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil settings")
	}
}

func TestUpsertPersonalitySettings(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := pgStore.UpsertPersonalitySettings(ctx, storepkg.PersonalitySettings{Content: "Be Gavryn.", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert personality settings: %v", err)
	}
	if err := pgStore.UpsertPersonalitySettings(ctx, storepkg.PersonalitySettings{Content: "Still Gavryn.", CreatedAt: now, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("upsert personality settings: %v", err)
	}
	settings, err := pgStore.GetPersonalitySettings(ctx)
	if err != nil {
		t.Fatalf("get personality settings: %v", err)
	}
	if settings == nil || settings.Content != "Still Gavryn." {
		t.Fatalf("expected personality settings to be updated")
	}
}

func TestSearchMemory(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	results, err := pgStore.SearchMemory(ctx, "", 5)
	if err != nil {
		t.Fatalf("search memory: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results for empty query")
	}

	now := time.Now().UTC()
	_, err = testDB.ExecContext(ctx, "INSERT INTO memory_entries (id, content, metadata, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)", uuid.NewString(), "hello world", []byte(`{"source":"one"}`), now, now)
	if err != nil {
		t.Fatalf("insert memory entry: %v", err)
	}
	_, err = testDB.ExecContext(ctx, "INSERT INTO memory_entries (id, content, metadata, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)", uuid.NewString(), "another note", nil, now, now)
	if err != nil {
		t.Fatalf("insert memory entry: %v", err)
	}

	results, err = pgStore.SearchMemory(ctx, "hello", 10)
	if err != nil {
		t.Fatalf("search memory: %v", err)
	}
	if len(results) != 1 || !strings.Contains(strings.ToLower(results[0].Content), "hello") {
		t.Fatalf("expected search to return matching entry")
	}

	results, err = pgStore.SearchMemory(ctx, "another", 10)
	if err != nil {
		t.Fatalf("search memory: %v", err)
	}
	if len(results) != 1 || results[0].Metadata == nil || len(results[0].Metadata) != 0 {
		t.Fatalf("expected empty metadata map for null metadata")
	}
}

func TestUpsertMemoryEntry_Dedupe(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	entry := storepkg.MemoryEntry{
		ID:      uuid.NewString(),
		Content: "hello world",
		Metadata: map[string]any{
			"fingerprint": "fp-1",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	inserted, err := pgStore.UpsertMemoryEntry(ctx, entry)
	if err != nil {
		t.Fatalf("upsert memory entry: %v", err)
	}
	if !inserted {
		t.Fatalf("expected entry to be inserted")
	}
	inserted, err = pgStore.UpsertMemoryEntry(ctx, entry)
	if err != nil {
		t.Fatalf("upsert memory entry: %v", err)
	}
	if inserted {
		t.Fatalf("expected entry to be deduped")
	}
}

func TestSearchMemoryWithEmbedding_FallbackToFTS(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	now := time.Now().UTC()
	_, err := testDB.ExecContext(ctx, "INSERT INTO memory_entries (id, content, metadata, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)", uuid.NewString(), "hello embedding", []byte(`{"source":"one"}`), now, now)
	if err != nil {
		t.Fatalf("insert memory entry: %v", err)
	}
	results, err := pgStore.SearchMemoryWithEmbedding(ctx, "hello", []float32{0.1, 0.2}, 5)
	if err != nil {
		t.Fatalf("search memory with embedding: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected fallback results")
	}
}

func TestSearchMemory_InvalidMetadata(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	_, err := testDB.ExecContext(ctx, "INSERT INTO memory_entries (id, content, metadata, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)", uuid.NewString(), "bad", []byte("[]"), time.Now().UTC(), time.Now().UTC())
	if err != nil {
		t.Fatalf("insert memory entry: %v", err)
	}

	if _, err := pgStore.SearchMemory(ctx, "bad", 5); err == nil {
		t.Fatalf("expected metadata unmarshal error")
	}
}

func TestSearchMemory_ClosedDB(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("pgx", testConn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	pgStore := &PostgresStore{db: conn}
	_ = conn.Close()

	if _, err := pgStore.SearchMemory(ctx, "query", 5); err == nil {
		t.Fatalf("expected error on closed db")
	}
}

func TestAppendEvent(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_ = pgStore.CreateRun(ctx, run)

	if err := pgStore.AppendEvent(ctx, storepkg.RunEvent{RunID: run.ID, Seq: 1, Type: "run.started", Source: "system"}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM run_events WHERE run_id = $1", run.ID).Scan(&count); err != nil {
		t.Fatalf("query run events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestAppendEvent_UpsertsRunSteps(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	require.NoError(t, pgStore.CreateRun(ctx, run))

	require.NoError(t, pgStore.AppendEvent(ctx, storepkg.RunEvent{
		RunID:     run.ID,
		Seq:       1,
		Type:      "step.started",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "llm",
		Payload: map[string]any{
			"step_id": "assistant_reply",
			"name":    "Generate assistant reply",
		},
	}))
	require.NoError(t, pgStore.AppendEvent(ctx, storepkg.RunEvent{
		RunID:     run.ID,
		Seq:       2,
		Type:      "step.failed",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "llm",
		Payload: map[string]any{
			"step_id": "assistant_reply",
			"error":   "provider timeout",
		},
	}))

	steps, err := pgStore.ListRunSteps(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Equal(t, "assistant_reply", steps[0].ID)
	require.Equal(t, "failed", steps[0].Status)
	require.Equal(t, "provider timeout", steps[0].Error)
	require.Equal(t, "step", steps[0].Kind)
}

func TestAppendEvent_MarshalError(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_ = pgStore.CreateRun(ctx, run)

	if err := pgStore.AppendEvent(ctx, storepkg.RunEvent{RunID: run.ID, Seq: 1, Type: "bad", Source: "system", Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatalf("expected payload marshal error")
	}
}

func TestListEvents(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_ = pgStore.CreateRun(ctx, run)

	_, err := testDB.ExecContext(ctx, "INSERT INTO run_events (run_id, seq, type, timestamp, source, trace_id, payload) VALUES ($1, $2, $3, $4, $5, $6, $7)", run.ID, 1, "start", time.Now().UTC(), "system", uuid.NewString(), []byte(`{"ok":true}`))
	if err != nil {
		t.Fatalf("insert run event: %v", err)
	}
	_, err = testDB.ExecContext(ctx, "INSERT INTO run_events (run_id, seq, type, timestamp, source, trace_id, payload) VALUES ($1, $2, $3, $4, $5, $6, $7)", run.ID, 2, "step", time.Now().UTC(), "system", nil, nil)
	if err != nil {
		t.Fatalf("insert run event: %v", err)
	}

	events, err := pgStore.ListEvents(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].Payload == nil || len(events[1].Payload) != 0 {
		t.Fatalf("expected empty payload for null payload")
	}
}

func TestListEvents_InvalidPayload(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_ = pgStore.CreateRun(ctx, run)

	_, err := testDB.ExecContext(ctx, "INSERT INTO run_events (run_id, seq, type, timestamp, source, trace_id, payload) VALUES ($1, $2, $3, $4, $5, $6, $7)", run.ID, 1, "bad", time.Now().UTC(), "system", nil, []byte("[]"))
	if err != nil {
		t.Fatalf("insert run event: %v", err)
	}

	if _, err := pgStore.ListEvents(ctx, run.ID, 0); err == nil {
		t.Fatalf("expected payload unmarshal error")
	}
}

func TestNextSeq(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_ = pgStore.CreateRun(ctx, run)

	seq1, err := pgStore.NextSeq(ctx, run.ID)
	if err != nil {
		t.Fatalf("next seq: %v", err)
	}
	seq2, err := pgStore.NextSeq(ctx, run.ID)
	if err != nil {
		t.Fatalf("next seq: %v", err)
	}
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("expected seq 1 and 2, got %d and %d", seq1, seq2)
	}
}

func TestNextSeq_ClosedDB(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("pgx", testConn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	pgStore := &PostgresStore{db: conn}
	_ = conn.Close()

	if _, err := pgStore.NextSeq(ctx, uuid.NewString()); err == nil {
		t.Fatalf("expected error on closed db")
	}
}

func TestConcurrentNextSeq(t *testing.T) {
	ctx := context.Background()
	pgStore := newStore(t)

	run := storepkg.Run{ID: uuid.NewString(), Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_ = pgStore.CreateRun(ctx, run)

	count := 20
	results := make([]int64, 0, count)
	mu := sync.Mutex{}
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := pgStore.NextSeq(ctx, run.ID)
			if err != nil {
				t.Errorf("next seq: %v", err)
				return
			}
			mu.Lock()
			results = append(results, seq)
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })
	if len(results) != count {
		t.Fatalf("expected %d sequences, got %d", count, len(results))
	}
	for i, seq := range results {
		if seq != int64(i+1) {
			t.Fatalf("expected seq %d, got %d", i+1, seq)
		}
	}
}

func TestListSkills_ClosedDB(t *testing.T) {
	ctx := context.Background()
	conn, err := sql.Open("pgx", testConn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := &PostgresStore{db: conn}
	_ = conn.Close()

	if _, err := store.ListSkills(ctx); err == nil {
		t.Fatalf("expected error on closed db")
	}
}
