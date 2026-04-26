package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// legacyStampVersion is the highest migration version considered already
// present on pre-refactor databases. Migrations above this number are real new
// schema changes that must run even when legacy adoption fires.
const legacyStampVersion = 3

// BackfillFunc runs after new migrations have been applied in a given
// ApplyWithBackfill call. It receives the live *sql.DB so it can execute any
// data-migration statements the caller needs (for example, populating new
// columns added by migration 004).
type BackfillFunc func(db *sql.DB) error

// Apply runs any embedded SQL migrations not yet recorded in schema_migrations.
// Legacy pre-refactor databases are detected and stamped as already at the
// current schema version. If the database records a version greater than the
// binary's max embedded version, Apply refuses to proceed.
func Apply(db *sql.DB, logger *slog.Logger) error {
	return applyFS(db, logger, migrationsFS, migrationsDir, nil)
}

// ApplyWithBackfill runs migrations like Apply, then invokes backfill if
// migration 004 was applied during this call. If backfill is nil it behaves
// identically to Apply.
func ApplyWithBackfill(db *sql.DB, logger *slog.Logger, backfill BackfillFunc) error {
	return applyFS(db, logger, migrationsFS, migrationsDir, backfill)
}

// fts5MigrationVersion is the schema version that creates the FTS5 virtual
// table. If FTS5 is not available in the current build, this migration is
// skipped and stamped so subsequent Apply calls do not attempt it.
const fts5MigrationVersion = 7

// probeFTS5 checks whether the FTS5 extension is available by trying to
// create a transient virtual table inside a SAVEPOINT. Returns true when
// FTS5 is present, false when the attempt fails.
func probeFTS5(db *sql.DB) bool {
	_, err := db.Exec(`SAVEPOINT probe_fts5`)
	if err != nil {
		return false
	}
	_, createErr := db.Exec(`CREATE VIRTUAL TABLE temp.probe_fts5_tbl USING fts5(x)`)
	if createErr != nil {
		_, _ = db.Exec(`ROLLBACK TO probe_fts5`)
		_, _ = db.Exec(`RELEASE probe_fts5`)
		return false
	}
	_, _ = db.Exec(`DROP TABLE IF EXISTS temp.probe_fts5_tbl`)
	_, _ = db.Exec(`RELEASE probe_fts5`)
	return true
}

// applyFS is the internal entry point parameterised by the source FS so tests
// can inject a fixture directory of failing migrations.
func applyFS(db *sql.DB, logger *slog.Logger, src fs.FS, dir string, backfill BackfillFunc) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := loadApplied(db)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(src, dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	type migration struct {
		version int
		name    string
	}
	migrations := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// Skip rollback (down) migration files — only forward migrations are applied.
		if strings.HasSuffix(e.Name(), ".down.sql") {
			continue
		}
		v, err := parseVersion(e.Name())
		if err != nil {
			return fmt.Errorf("parse migration name %q: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{version: v, name: e.Name()})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	maxEmbedded := 0
	for _, m := range migrations {
		if m.version > maxEmbedded {
			maxEmbedded = m.version
		}
	}

	if len(applied) == 0 && detectLegacy(db) {
		legacyThrough := legacyStampVersion
		logger.Info("legacy database detected, stamping migrations as applied", "through", legacyThrough)
		for _, m := range migrations {
			if m.version > legacyThrough {
				continue
			}
			if err := stamp(db, m.version); err != nil {
				return fmt.Errorf("stamp legacy version %d: %w", m.version, err)
			}
			applied[m.version] = true
		}
	}

	for v := range applied {
		if v > maxEmbedded {
			return fmt.Errorf("database is at schema version %d but binary only knows up to %d — downgrade detected", v, maxEmbedded)
		}
	}

	// Probe FTS5 availability once before the migration loop so we can decide
	// whether to skip migration 007. The probe is cheap and only happens when
	// FTS5 migration has not yet been applied.
	fts5Available := true
	if !applied[fts5MigrationVersion] {
		fts5Available = probeFTS5(db)
		if !fts5Available {
			logger.Warn("FTS5 extension not available; skipping filename FTS migration — filename search will use LIKE fallback")
		}
	}

	appliedThisRun := make(map[int]bool)
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		// Skip the FTS5 migration when the extension is absent. Stamp it as
		// applied so future Apply calls do not retry it.
		if m.version == fts5MigrationVersion && !fts5Available {
			if err := stamp(db, m.version); err != nil {
				return fmt.Errorf("stamp skipped FTS5 migration: %w", err)
			}
			logger.Info("stamped FTS5 migration as skipped", "version", m.version)
			continue
		}
		body, err := fs.ReadFile(src, dir+"/"+m.name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.name, err)
		}
		if err := runMigration(db, m.version, m.name, string(body)); err != nil {
			return err
		}
		appliedThisRun[m.version] = true
		logger.Info("applied migration", "version", m.version, "file", m.name)
	}

	if backfill != nil && appliedThisRun[4] {
		if err := backfill(db); err != nil {
			return fmt.Errorf("backfill after migration 004: %w", err)
		}
	}

	// EDGE-4: Run the derived-column backfill in 5k-row batches when migration
	// 006 was applied during this call. Each batch runs in its own transaction
	// so WAL writers can interleave between batches on large existing databases.
	if appliedThisRun[6] {
		if err := backfill006(db, logger); err != nil {
			return fmt.Errorf("backfill after migration 006: %w", err)
		}
	}

	return nil
}

// backfill006BatchSize is the number of rows updated per transaction in
// backfill006. 5000 rows keeps each WAL transaction short while still being
// large enough to amortise transaction overhead.
const backfill006BatchSize = 5000

// backfill006 populates the basename, parent, and stem columns for all existing
// rows in the files table. It runs in batches of backfill006BatchSize rows,
// each in its own transaction, so that WAL writers can interleave on large
// databases (EDGE-4).
//
// The path decomposition uses the same SQLite string-function formula documented
// in 006_file_derived_columns.up.sql.
func backfill006(db *sql.DB, logger *slog.Logger) error {
	const batchSQL = `
UPDATE files
SET
  basename = substr(path,
               length(rtrim(path, replace(path, '/', ''))) + 1),

  parent   = CASE
               WHEN instr(path, '/') = 0
               THEN ''
               WHEN length(rtrim(path, replace(path, '/', ''))) = 1
               THEN '/'
               ELSE substr(path, 1,
                      length(rtrim(path, replace(path, '/', ''))) - 1)
             END,

  stem     = (
    WITH bn(v) AS (
      SELECT substr(path, length(rtrim(path, replace(path, '/', ''))) + 1)
    )
    SELECT CASE
             WHEN instr(v, '.') <= 1
             THEN v
             ELSE substr(v, 1, length(rtrim(v, replace(v, '.', ''))) - 1)
           END
    FROM bn
  )

WHERE id IN (
  SELECT id FROM files WHERE basename = '' LIMIT ?
)`

	total := 0
	for {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin backfill006 batch: %w", err)
		}
		res, err := tx.Exec(batchSQL, backfill006BatchSize)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("execute backfill006 batch: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rows affected backfill006 batch: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit backfill006 batch: %w", err)
		}
		total += int(n)
		if total > 0 && total%50000 == 0 {
			logger.Info("migration 006 backfill progress", "rows_done", total)
		}
		if n == 0 {
			break
		}
	}
	if total > 0 {
		logger.Info("migration 006 backfill complete", "rows_updated", total)
	}
	return nil
}

func runMigration(db *sql.DB, version int, name, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", name, err)
	}
	if _, err := tx.Exec(body); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %s: %w", name, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("stamp migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}

func loadApplied(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan applied version: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func stamp(db *sql.DB, version int) error {
	_, err := db.Exec(
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// parseVersion reads the leading integer from a filename like "002_foo.sql".
func parseVersion(name string) (int, error) {
	prefix := strings.SplitN(name, "_", 2)[0]
	return strconv.Atoi(prefix)
}

// detectLegacy returns true when the database shape matches a pre-refactor
// install: no schema_migrations rows, but chunks.vector_blob and the
// parsed_query_cache table are already present.
func detectLegacy(db *sql.DB) bool {
	if !hasColumn(db, "chunks", "vector_blob") {
		return false
	}
	if !hasTable(db, "parsed_query_cache") {
		return false
	}
	return true
}

func hasTable(db *sql.DB, name string) bool {
	var got string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
		name,
	).Scan(&got)
	return err == nil && got == name
}

func hasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return false
		}
		if n == column {
			return true
		}
	}
	return false
}
