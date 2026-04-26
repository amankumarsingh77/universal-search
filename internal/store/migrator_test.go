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

	if len(got) != 7 {
		t.Fatalf("expected 7 migrations applied, got %d", len(got))
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
	// and migration 6 (ALTER TABLE files) can find the expected tables.
	if _, err := db.Exec(`CREATE TABLE chunks (id INTEGER PRIMARY KEY, file_id INTEGER, vector_id TEXT UNIQUE, chunk_index INTEGER, start_time REAL, end_time REAL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE files (id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE, file_type TEXT NOT NULL, extension TEXT NOT NULL, size_bytes INTEGER NOT NULL, modified_at DATETIME NOT NULL, indexed_at DATETIME NOT NULL, content_hash TEXT NOT NULL DEFAULT '', thumbnail_path TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}

	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Fatalf("expected 7 rows, got %d", count)
	}

	// Version 2's ALTER TABLE must have run: chunks.vector_blob column exists.
	if !columnExists(t, db, "chunks", "vector_blob") {
		t.Error("expected vector_blob column after migration 2 ran")
	}
	// Version 3 table exists.
	if !tableExists(t, db, "parsed_query_cache") {
		t.Error("expected parsed_query_cache after migration 3 ran")
	}
	// Version 5's ALTER TABLE must have run: schema_version column exists.
	if !columnExists(t, db, "parsed_query_cache", "schema_version") {
		t.Error("expected schema_version column after migration 5 ran")
	}
	// Version 6's ALTER TABLE must have run: derived columns exist.
	for _, col := range []string{"basename", "parent", "stem"} {
		if !columnExists(t, db, "files", col) {
			t.Errorf("expected files.%s column after migration 6 ran", col)
		}
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
	// Legacy adoption stamps 1..3; migrations 004, 005, 006, and 007 are real new migrations that run on top.
	if count != 7 {
		t.Fatalf("expected 7 rows after legacy adoption + migrations 004+005+006+007, got %d", count)
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
	// Migration 006: derived path columns.
	for _, col := range []string{"basename", "parent", "stem"} {
		if !columnExists(t, s.db, "files", col) {
			t.Errorf("expected files.%s column to exist after migration 006", col)
		}
	}
	// Migration 007: FTS5 virtual table.
	if !tableExists(t, s.db, "filename_search") {
		t.Error("expected filename_search FTS5 virtual table to exist after migration 007")
	}
	// FilenameFTSAvailable should report true when the table exists.
	if !s.FilenameFTSAvailable() {
		t.Error("expected FilenameFTSAvailable() == true after successful migration 007")
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

// TestMigration_006_BackfillsDerivedColumns seeds a v5 DB with rows that
// lack the derived columns, runs Apply, and asserts every row is correctly
// backfilled with basename, parent, and stem values.
func TestMigration_006_BackfillsDerivedColumns(t *testing.T) {
	db := openMemDB(t)

	// Build a v5-equivalent schema: same tables as migrations 1-5 but without
	// the derived columns that migration 6 will add.
	v5Schema := `
		CREATE TABLE files (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			path          TEXT NOT NULL UNIQUE,
			file_type     TEXT NOT NULL,
			extension     TEXT NOT NULL,
			size_bytes    INTEGER NOT NULL,
			modified_at   DATETIME NOT NULL,
			indexed_at    DATETIME NOT NULL,
			content_hash  TEXT NOT NULL DEFAULT '',
			thumbnail_path TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE chunks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id     INTEGER NOT NULL,
			vector_id   TEXT NOT NULL UNIQUE,
			chunk_index INTEGER NOT NULL,
			start_time  REAL NOT NULL DEFAULT 0,
			end_time    REAL NOT NULL DEFAULT 0,
			vector_blob BLOB,
			embedding_model TEXT NOT NULL DEFAULT '',
			embedding_dims  INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE indexed_folders (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE);
		CREATE TABLE excluded_patterns (id INTEGER PRIMARY KEY AUTOINCREMENT, pattern TEXT NOT NULL UNIQUE);
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE query_cache (query TEXT PRIMARY KEY, vector BLOB NOT NULL, created_at INTEGER NOT NULL);
		CREATE TABLE parsed_query_cache (
			query_text_normalized TEXT PRIMARY KEY,
			spec_json TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL,
			schema_version INTEGER NOT NULL DEFAULT 0
		);
	`
	if _, err := db.Exec(v5Schema); err != nil {
		t.Fatalf("seed v5 schema: %v", err)
	}

	// Seed fixture rows covering the path shapes we care about.
	type fixture struct {
		path         string
		wantBasename string
		wantParent   string
		wantStem     string
	}
	fixtures := []fixture{
		{"/home/user/documents/report.pdf", "report.pdf", "/home/user/documents", "report"},
		{"/opt/backups/data.tar.gz", "data.tar.gz", "/opt/backups", "data.tar"},
		{"/usr/bin/grep", "grep", "/usr/bin", "grep"},
		{"/etc/ssh/sshd_config", "sshd_config", "/etc/ssh", "sshd_config"},
		{"standalone.txt", "standalone.txt", "", "standalone"},
		{"/file_at_root.txt", "file_at_root.txt", "/", "file_at_root"},
	}
	for i, f := range fixtures {
		_, err := db.Exec(
			`INSERT INTO files (path, file_type, extension, size_bytes, modified_at, indexed_at)
			 VALUES (?, 'text', '.txt', 0, '2020-01-01', '2020-01-01')`,
			f.path,
		)
		if err != nil {
			t.Fatalf("insert fixture %d (%s): %v", i, f.path, err)
		}
	}

	// Stamp versions 1–5 as already applied so Apply only runs migration 6.
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	for v := 1; v <= 5; v++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?, ?)`, v, ts); err != nil {
			t.Fatalf("stamp version %d: %v", v, err)
		}
	}

	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Assert derived columns are now present.
	for _, col := range []string{"basename", "parent", "stem"} {
		if !columnExists(t, db, "files", col) {
			t.Errorf("expected files.%s column after migration 006", col)
		}
	}

	// Assert each seeded row was backfilled correctly.
	rows, err := db.Query(`SELECT path, basename, parent, stem FROM files ORDER BY path`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	got := make(map[string][3]string)
	for rows.Next() {
		var path, basename, parent, stem string
		if err := rows.Scan(&path, &basename, &parent, &stem); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[path] = [3]string{basename, parent, stem}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for _, f := range fixtures {
		vals, ok := got[f.path]
		if !ok {
			t.Errorf("path %q not found in results", f.path)
			continue
		}
		if vals[0] != f.wantBasename {
			t.Errorf("path=%q: basename = %q, want %q", f.path, vals[0], f.wantBasename)
		}
		if vals[1] != f.wantParent {
			t.Errorf("path=%q: parent = %q, want %q", f.path, vals[1], f.wantParent)
		}
		if vals[2] != f.wantStem {
			t.Errorf("path=%q: stem = %q, want %q", f.path, vals[2], f.wantStem)
		}
	}
}

// TestMigration_006_BatchedBackfill_12kRows seeds 12 000 synthetic rows in a
// v5 DB, runs Apply (which applies migration 006), and asserts that every row
// has its derived columns populated correctly. This exercises the batched
// Go-driven backfill path (EDGE-4): the backfill runs in 5k-row chunks to
// avoid long-held WAL write locks on large existing databases.
func TestMigration_006_BatchedBackfill_12kRows(t *testing.T) {
	db := openMemDB(t)

	v5Schema := `
		CREATE TABLE files (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			path          TEXT NOT NULL UNIQUE,
			file_type     TEXT NOT NULL,
			extension     TEXT NOT NULL,
			size_bytes    INTEGER NOT NULL,
			modified_at   DATETIME NOT NULL,
			indexed_at    DATETIME NOT NULL,
			content_hash  TEXT NOT NULL DEFAULT '',
			thumbnail_path TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE chunks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id     INTEGER NOT NULL,
			vector_id   TEXT NOT NULL UNIQUE,
			chunk_index INTEGER NOT NULL,
			start_time  REAL NOT NULL DEFAULT 0,
			end_time    REAL NOT NULL DEFAULT 0,
			vector_blob BLOB,
			embedding_model TEXT NOT NULL DEFAULT '',
			embedding_dims  INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE indexed_folders (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE);
		CREATE TABLE excluded_patterns (id INTEGER PRIMARY KEY AUTOINCREMENT, pattern TEXT NOT NULL UNIQUE);
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE query_cache (query TEXT PRIMARY KEY, vector BLOB NOT NULL, created_at INTEGER NOT NULL);
		CREATE TABLE parsed_query_cache (
			query_text_normalized TEXT PRIMARY KEY,
			spec_json TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL,
			schema_version INTEGER NOT NULL DEFAULT 0
		);
	`
	if _, err := db.Exec(v5Schema); err != nil {
		t.Fatalf("seed v5 schema: %v", err)
	}

	// Insert 12 000 rows with deterministic paths spread across different
	// directory depths to exercise the full path-decomposition formula.
	const total = 12000
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin seed tx: %v", err)
	}
	for i := 0; i < total; i++ {
		path := fmt.Sprintf("/home/user/dir%04d/file%05d.txt", i%100, i)
		if _, err := tx.Exec(
			`INSERT INTO files (path, file_type, extension, size_bytes, modified_at, indexed_at)
			 VALUES (?, 'text', '.txt', 0, '2020-01-01', '2020-01-01')`,
			path,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert row %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}

	// Stamp versions 1–5 as already applied so Apply only runs migration 006.
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	for v := 1; v <= 5; v++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?, ?)`, v, ts); err != nil {
			t.Fatalf("stamp version %d: %v", v, err)
		}
	}

	if err := Apply(db, migratorTestLogger); err != nil {
		t.Fatalf("Apply (migration 006 with 12k rows): %v", err)
	}

	// All rows must have non-empty basename and parent.
	var emptyBasename, emptyStem int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files WHERE basename = ''`).Scan(&emptyBasename); err != nil {
		t.Fatalf("count empty basename: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM files WHERE stem = ''`).Scan(&emptyStem); err != nil {
		t.Fatalf("count empty stem: %v", err)
	}
	if emptyBasename != 0 {
		t.Errorf("EDGE-4: %d rows still have empty basename after batched backfill", emptyBasename)
	}
	if emptyStem != 0 {
		t.Errorf("EDGE-4: %d rows still have empty stem after batched backfill", emptyStem)
	}

	// Spot-check a known row.
	var basename, parent, stem string
	if err := db.QueryRow(
		`SELECT basename, parent, stem FROM files WHERE path = '/home/user/dir0000/file00000.txt'`,
	).Scan(&basename, &parent, &stem); err != nil {
		t.Fatalf("spot-check row: %v", err)
	}
	if basename != "file00000.txt" {
		t.Errorf("spot-check: basename = %q, want %q", basename, "file00000.txt")
	}
	if parent != "/home/user/dir0000" {
		t.Errorf("spot-check: parent = %q, want %q", parent, "/home/user/dir0000")
	}
	if stem != "file00000" {
		t.Errorf("spot-check: stem = %q, want %q", stem, "file00000")
	}

	// Total row count must be intact.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != total {
		t.Errorf("expected %d rows, got %d after migration 006", total, count)
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
