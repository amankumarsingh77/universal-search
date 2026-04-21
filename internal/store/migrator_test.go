package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

var migratorTestLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestApply_FreshDB (REF-010): empty DB applies all embedded migrations in order.
func TestApply_FreshDB(t *testing.T) {
	db := openMemDB(t)

	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	rows, err := db.Query(`SELECT version, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	defer rows.Close()

	type row struct {
		version   int
		appliedAt string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.version, &r.appliedAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 4 {
		t.Fatalf("expected 4 migrations applied, got %d", len(got))
	}
	for i, r := range got {
		if r.version != i+1 {
			t.Errorf("row %d: expected version %d, got %d", i, i+1, r.version)
		}
		if _, err := time.Parse(time.RFC3339, r.appliedAt); err != nil {
			t.Errorf("row %d: applied_at %q is not RFC3339: %v", i, r.appliedAt, err)
		}
	}

	// Monotonic applied_at.
	for i := 1; i < len(got); i++ {
		prev, _ := time.Parse(time.RFC3339, got[i-1].appliedAt)
		cur, _ := time.Parse(time.RFC3339, got[i].appliedAt)
		if cur.Before(prev) {
			t.Errorf("applied_at not monotonic at %d", i)
		}
	}
}

// TestApply_SkipsAlreadyApplied (REF-011): pre-seeded versions are not re-run.
func TestApply_SkipsAlreadyApplied(t *testing.T) {
	db := openMemDB(t)

	// Create the migrations table and stamp version 1 without running SQL.
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (1, ?)`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	// Also hand-create the tables that migration 1 would create, so migration 2 (ALTER TABLE chunks)
	// can find the chunks table.
	if _, err := db.Exec(`CREATE TABLE chunks (id INTEGER PRIMARY KEY, file_id INTEGER, vector_id TEXT UNIQUE, chunk_index INTEGER, start_time REAL, end_time REAL)`); err != nil {
		t.Fatal(err)
	}

	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("expected 4 rows, got %d", count)
	}

	// Version 2's ALTER TABLE must have run: chunks.vector_blob column exists.
	if !columnExists(t, db, "chunks", "vector_blob") {
		t.Error("expected vector_blob column after migration 2 ran")
	}
	// Version 3 table exists.
	if !tableExists(t, db, "parsed_query_cache") {
		t.Error("expected parsed_query_cache after migration 3 ran")
	}
}

// TestApply_Idempotent (REF-011): calling Apply twice is a no-op the second time.
func TestApply_Idempotent(t *testing.T) {
	db := openMemDB(t)

	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	var firstCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&firstCount); err != nil {
		t.Fatal(err)
	}
	var firstAppliedAt string
	if err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = 1`).Scan(&firstAppliedAt); err != nil {
		t.Fatal(err)
	}

	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	var secondCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&secondCount); err != nil {
		t.Fatal(err)
	}
	if firstCount != secondCount {
		t.Fatalf("row count changed: %d -> %d", firstCount, secondCount)
	}

	var secondAppliedAt string
	if err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = 1`).Scan(&secondAppliedAt); err != nil {
		t.Fatal(err)
	}
	if firstAppliedAt != secondAppliedAt {
		t.Errorf("applied_at for version 1 changed between calls: %q -> %q", firstAppliedAt, secondAppliedAt)
	}
}

//go:embed testdata/bad_migrations/*.sql
var badMigrationsFS embed.FS

// TestApply_TransactionalRollback (REF-011): failing migration rolls back and returns error with filename.
func TestApply_TransactionalRollback(t *testing.T) {
	db := openMemDB(t)

	err := applyFS(db, migratorTestLogger, badMigrationsFS, "testdata/bad_migrations", nil)
	if err == nil {
		t.Fatal("expected error from failing migration, got nil")
	}
	if !strings.Contains(err.Error(), "002_broken.sql") {
		t.Errorf("error should name failing migration file, got: %v", err)
	}

	// First migration committed, second rolled back.
	var versions []int
	rows, qerr := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if qerr != nil {
		t.Fatalf("query: %v", qerr)
	}
	for rows.Next() {
		var v int
		_ = rows.Scan(&v)
		versions = append(versions, v)
	}
	rows.Close()
	if len(versions) != 1 || versions[0] != 1 {
		t.Errorf("expected only version 1 stamped, got %v", versions)
	}
	// Partial table from failed migration must not exist.
	if tableExists(t, db, "should_not_exist") {
		t.Error("partial table from rolled-back migration exists")
	}
}

// TestApply_LegacyAdoption (REF-012): pre-refactor DB shape stamps versions 1..3.
func TestApply_LegacyAdoption(t *testing.T) {
	db := openMemDB(t)

	// Build a DB in pre-refactor shape by running the *old* combined schema path:
	// the original CREATE statements plus the idempotent ALTER, plus parsed_query_cache.
	legacySchema := `
		CREATE TABLE files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL UNIQUE,
			file_type TEXT NOT NULL,
			extension TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			modified_at DATETIME NOT NULL,
			indexed_at DATETIME NOT NULL,
			content_hash TEXT NOT NULL DEFAULT '',
			thumbnail_path TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			vector_id TEXT NOT NULL UNIQUE,
			chunk_index INTEGER NOT NULL,
			start_time REAL NOT NULL DEFAULT 0,
			end_time REAL NOT NULL DEFAULT 0
		);
		CREATE TABLE indexed_folders (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE);
		CREATE TABLE excluded_patterns (id INTEGER PRIMARY KEY AUTOINCREMENT, pattern TEXT NOT NULL UNIQUE);
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE query_cache (query TEXT PRIMARY KEY, vector BLOB NOT NULL, created_at INTEGER NOT NULL);
		CREATE TABLE parsed_query_cache (
			query_text_normalized TEXT PRIMARY KEY,
			spec_json TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL
		);
		ALTER TABLE chunks ADD COLUMN vector_blob BLOB;
	`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}

	// Apply must stamp 1..3 without attempting CREATE on existing tables.
	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("Apply on legacy DB: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	// Legacy adoption stamps 1..3; migration 004 is a real new migration that runs on top.
	if count != 4 {
		t.Fatalf("expected 4 rows after legacy adoption + migration 004, got %d", count)
	}
	if !columnExists(t, db, "chunks", "embedding_model") {
		t.Error("expected embedding_model column after migration 004 ran")
	}
	if !columnExists(t, db, "chunks", "embedding_dims") {
		t.Error("expected embedding_dims column after migration 004 ran")
	}
}

// TestApply_DowngradeRefusal (REF-013): version beyond embedded max returns downgrade error.
func TestApply_DowngradeRefusal(t *testing.T) {
	db := openMemDB(t)

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (999, ?)`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	err := Apply(db, migratorTestLogger)
	if err == nil {
		t.Fatal("expected downgrade error, got nil")
	}
	if !strings.Contains(err.Error(), "downgrade") {
		t.Errorf("error should mention downgrade, got: %v", err)
	}
}

// TestStore_NewStore_RunsMigrations: NewStore wires the migrator; tables all present.
func TestStore_NewStore_RunsMigrations(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	tables := []string{
		"files", "chunks", "indexed_folders", "excluded_patterns",
		"settings", "query_cache", "parsed_query_cache", "schema_migrations",
	}
	for _, tbl := range tables {
		if !tableExists(t, s.db, tbl) {
			t.Errorf("expected table %s to exist", tbl)
		}
	}
	if !columnExists(t, s.db, "chunks", "vector_blob") {
		t.Error("expected chunks.vector_blob column to exist")
	}
}

// TestMigration_004_BackfillsExistingRows (REF-060): after migration 004 applies,
// the backfill callback is invoked and pre-existing chunk rows get the supplied
// model+dims values.
func TestMigration_004_BackfillsExistingRows(t *testing.T) {
	db := openMemDB(t)

	// Apply migrations 1..3 manually by using ApplyWithBackfill with a filter
	// that runs the migrations. Simplest: use Apply (all 4) but we also want to
	// test that pre-existing rows with model='' get updated. Seed rows BEFORE
	// migration 004 by stamping 1..3, creating tables, inserting, then running
	// Apply — which will only apply 004 and call the backfill.
	legacySchema := `
		CREATE TABLE files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL UNIQUE,
			file_type TEXT NOT NULL,
			extension TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			modified_at DATETIME NOT NULL,
			indexed_at DATETIME NOT NULL,
			content_hash TEXT NOT NULL DEFAULT '',
			thumbnail_path TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id INTEGER NOT NULL,
			vector_id TEXT NOT NULL UNIQUE,
			chunk_index INTEGER NOT NULL,
			start_time REAL NOT NULL DEFAULT 0,
			end_time REAL NOT NULL DEFAULT 0
		);
		CREATE TABLE indexed_folders (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE);
		CREATE TABLE excluded_patterns (id INTEGER PRIMARY KEY AUTOINCREMENT, pattern TEXT NOT NULL UNIQUE);
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE query_cache (query TEXT PRIMARY KEY, vector BLOB NOT NULL, created_at INTEGER NOT NULL);
		CREATE TABLE parsed_query_cache (
			query_text_normalized TEXT PRIMARY KEY,
			spec_json TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL
		);
		ALTER TABLE chunks ADD COLUMN vector_blob BLOB;
		INSERT INTO files (path, file_type, extension, size_bytes, modified_at, indexed_at)
		VALUES ('/x', 't', '.t', 0, '2020-01-01', '2020-01-01');
	`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := db.Exec(`INSERT INTO chunks (file_id, vector_id, chunk_index) VALUES (1, ?, ?)`,
			fmt.Sprintf("v%d", i), i); err != nil {
			t.Fatal(err)
		}
	}

	backfillCalled := false
	backfill := func(db *sql.DB) error {
		backfillCalled = true
		_, err := db.Exec(`UPDATE chunks SET embedding_model = ?, embedding_dims = ? WHERE embedding_model = ''`, "fake-a", 64)
		return err
	}

	if err := ApplyWithBackfill(db, migratorTestLogger, backfill); err != nil {
		t.Fatalf("ApplyWithBackfill: %v", err)
	}
	if !backfillCalled {
		t.Fatal("expected backfill callback to be invoked")
	}

	rows, err := db.Query(`SELECT embedding_model, embedding_dims FROM chunks`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var m string
		var d int
		if err := rows.Scan(&m, &d); err != nil {
			t.Fatal(err)
		}
		if m != "fake-a" {
			t.Errorf("row %d: embedding_model = %q, want %q", n, m, "fake-a")
		}
		if d != 64 {
			t.Errorf("row %d: embedding_dims = %d, want 64", n, d)
		}
		n++
	}
	if n != 10 {
		t.Fatalf("expected 10 chunk rows, got %d", n)
	}
}

// TestApplyWithBackfill_SkipsCallbackWhen004AlreadyApplied: on a DB that already has
// migration 4 applied, the backfill callback is NOT invoked.
func TestApplyWithBackfill_SkipsCallbackWhen004AlreadyApplied(t *testing.T) {
	db := openMemDB(t)
	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	called := false
	backfill := func(db *sql.DB) error {
		called = true
		return nil
	}
	if err := ApplyWithBackfill(db, migratorTestLogger, backfill); err != nil {
		t.Fatalf("ApplyWithBackfill: %v", err)
	}
	if called {
		t.Error("backfill should not be invoked when migration 004 was already applied")
	}
}

// --- helpers ---

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&got)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("table lookup: %v", err)
	}
	return got == name
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if n == column {
			return true
		}
	}
	return false
}
