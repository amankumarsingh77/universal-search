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

	appliedThisRun := make(map[int]bool)
	for _, m := range migrations {
		if applied[m.version] {
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
