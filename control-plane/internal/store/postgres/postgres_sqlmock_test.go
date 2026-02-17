package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newMockStore(t *testing.T) (*PostgresStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	cleanup := func() {
		_ = db.Close()
	}
	return &PostgresStore{db: db}, mock, cleanup
}

func TestVerifySchema_QueryError(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT to_regclass").WillReturnError(errors.New("query error"))
	if err := verifySchema(ctx, pgStore.db); err == nil {
		t.Fatalf("expected schema verification error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListMessages_RowsErr(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "run_id", "role", "content", "sequence", "created_at", "metadata"}).
		AddRow("m-1", "r-1", "user", "hi", int64(1), time.Now(), []byte("{}")).
		AddRow("m-2", "r-1", "user", "hi", int64(2), time.Now(), []byte("{}"))
	rows.RowError(1, errors.New("row error"))

	mock.ExpectQuery("SELECT id, run_id, role, content, sequence, created_at, metadata").WillReturnRows(rows)
	if _, err := pgStore.ListMessages(ctx, "r-1"); err == nil {
		t.Fatalf("expected rows error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListMessages_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "run_id", "role", "content", "sequence", "created_at", "metadata"}).
		AddRow("m-1", "r-1", "user", "hi", "not-int", time.Now(), []byte("{}"))

	mock.ExpectQuery("SELECT id, run_id, role, content, sequence, created_at, metadata").WillReturnRows(rows)
	if _, err := pgStore.ListMessages(ctx, "r-1"); err == nil {
		t.Fatalf("expected scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListSkills_RowsErr(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "name", "description", "created_at", "updated_at"}).
		AddRow("s-1", "alpha", "desc", time.Now(), time.Now()).
		AddRow("s-2", "beta", "desc", time.Now(), time.Now())
	rows.RowError(1, errors.New("row error"))

	mock.ExpectQuery("SELECT id, name, description, created_at, updated_at").WillReturnRows(rows)
	if _, err := pgStore.ListSkills(ctx); err == nil {
		t.Fatalf("expected rows error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListSkillFiles_RowsErr(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "skill_id", "path", "content", "content_type", "size_bytes", "created_at", "updated_at"}).
		AddRow("f-1", "s-1", "a.txt", []byte("a"), "text/plain", int64(1), time.Now(), time.Now()).
		AddRow("f-2", "s-1", "b.txt", []byte("b"), "text/plain", int64(1), time.Now(), time.Now())
	rows.RowError(1, errors.New("row error"))

	mock.ExpectQuery("SELECT id, skill_id, path, content, content_type, size_bytes, created_at, updated_at").WillReturnRows(rows)
	if _, err := pgStore.ListSkillFiles(ctx, "s-1"); err == nil {
		t.Fatalf("expected rows error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListSkillFiles_QueryError(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT id, skill_id, path, content, content_type, size_bytes, created_at, updated_at").WillReturnError(errors.New("query error"))
	if _, err := pgStore.ListSkillFiles(ctx, "s-1"); err == nil {
		t.Fatalf("expected query error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListSkillFiles_ScanErrorSqlmock(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "skill_id", "path", "content", "content_type", "size_bytes", "created_at", "updated_at"}).
		AddRow("f-1", "s-1", "a.txt", []byte("a"), "text/plain", "bad", time.Now(), time.Now())

	mock.ExpectQuery("SELECT id, skill_id, path, content, content_type, size_bytes, created_at, updated_at").WillReturnRows(rows)
	if _, err := pgStore.ListSkillFiles(ctx, "s-1"); err == nil {
		t.Fatalf("expected scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListContextNodes_RowsErr(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "parent_id", "name", "node_type", "content_type", "size_bytes", "created_at", "updated_at"}).
		AddRow("n-1", nil, "root", "folder", "", int64(0), time.Now(), time.Now()).
		AddRow("n-2", "n-1", "child", "file", "text/plain", int64(1), time.Now(), time.Now())
	rows.RowError(1, errors.New("row error"))

	mock.ExpectQuery("SELECT id, parent_id, name, node_type, content_type, size_bytes, created_at, updated_at").WillReturnRows(rows)
	if _, err := pgStore.ListContextNodes(ctx); err == nil {
		t.Fatalf("expected rows error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListContextNodes_QueryError(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT id, parent_id, name, node_type, content_type, size_bytes, created_at, updated_at").WillReturnError(errors.New("query error"))
	if _, err := pgStore.ListContextNodes(ctx); err == nil {
		t.Fatalf("expected query error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListContextNodes_ScanErrorSqlmock(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "parent_id", "name", "node_type", "content_type", "size_bytes", "created_at", "updated_at"}).
		AddRow("n-1", nil, "root", "folder", "", "bad", time.Now(), time.Now())

	mock.ExpectQuery("SELECT id, parent_id, name, node_type, content_type, size_bytes, created_at, updated_at").WillReturnRows(rows)
	if _, err := pgStore.ListContextNodes(ctx); err == nil {
		t.Fatalf("expected scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSearchMemory_RowsErr(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "content", "metadata", "created_at", "updated_at"}).
		AddRow("m-1", "hello", []byte("{}"), time.Now(), time.Now()).
		AddRow("m-2", "world", []byte("{}"), time.Now(), time.Now())
	rows.RowError(1, errors.New("row error"))

	mock.ExpectQuery("SELECT id, content, metadata, created_at, updated_at").WillReturnRows(rows)
	if _, err := pgStore.SearchMemory(ctx, "hello", 5); err == nil {
		t.Fatalf("expected rows error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSearchMemory_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "content", "metadata", "created_at", "updated_at"}).
		AddRow("m-1", "hello", []byte("{}"), "bad", time.Now())

	mock.ExpectQuery("SELECT id, content, metadata, created_at, updated_at").WillReturnRows(rows)
	if _, err := pgStore.SearchMemory(ctx, "hello", 5); err == nil {
		t.Fatalf("expected scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListEvents_RowsErr(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"run_id", "seq", "type", "timestamp", "source", "trace_id", "payload"}).
		AddRow("r-1", int64(1), "start", time.Now(), "system", "trace-1", []byte("{}")).
		AddRow("r-1", int64(2), "step", time.Now(), "system", nil, []byte("{}"))
	rows.RowError(1, errors.New("row error"))

	mock.ExpectQuery("SELECT run_id, seq, type, timestamp, source, trace_id, payload").WillReturnRows(rows)
	if _, err := pgStore.ListEvents(ctx, "r-1", 0); err == nil {
		t.Fatalf("expected rows error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListEvents_QueryError(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT run_id, seq, type, timestamp, source, trace_id, payload").WillReturnError(errors.New("query error"))
	if _, err := pgStore.ListEvents(ctx, "r-1", 0); err == nil {
		t.Fatalf("expected query error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestListEvents_ScanError(t *testing.T) {
	ctx := context.Background()
	pgStore, mock, cleanup := newMockStore(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"run_id", "seq", "type", "timestamp", "source", "trace_id", "payload"}).
		AddRow("r-1", int64(1), "start", "bad", "system", "trace-1", []byte("{}"))

	mock.ExpectQuery("SELECT run_id, seq, type, timestamp, source, trace_id, payload").WillReturnRows(rows)
	if _, err := pgStore.ListEvents(ctx, "r-1", 0); err == nil {
		t.Fatalf("expected scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
