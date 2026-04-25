package config

import (
	"fmt"
	"sort"
	"sync"
)

// CurrentSchemaVersion is the latest config schema version understood by this build.
var CurrentSchemaVersion = 1

// Migrator upgrades an in-memory config document from version N to N+1 in place.
type Migrator func(doc map[string]any) error

var (
	migratorsMu sync.Mutex
	migrators   = map[int]Migrator{}
)

// RegisterMigrator registers a migrator that upgrades a document from
// fromVersion to fromVersion+1. Migrators are idempotent keyed by fromVersion;
// a second call for the same version overwrites the first.
func RegisterMigrator(fromVersion int, m Migrator) {
	migratorsMu.Lock()
	defer migratorsMu.Unlock()
	migrators[fromVersion] = m
}

// RunMigrations applies migrators in ascending order until doc reaches
// CurrentSchemaVersion. Missing migrators between currentVersion and target
// are treated as no-ops. Returns the upgraded document and its new version.
func RunMigrations(doc map[string]any, currentVersion int) (map[string]any, int, error) {
	migratorsMu.Lock()
	available := make([]int, 0, len(migrators))
	for v := range migrators {
		available = append(available, v)
	}
	migratorsMu.Unlock()
	sort.Ints(available)

	version := currentVersion
	for _, from := range available {
		if from < version {
			continue
		}
		if from >= CurrentSchemaVersion {
			break
		}
		migratorsMu.Lock()
		m := migrators[from]
		migratorsMu.Unlock()
		if err := m(doc); err != nil {
			return nil, version, fmt.Errorf("migrator %d->%d: %w", from, from+1, err)
		}
		version = from + 1
	}
	if version < CurrentSchemaVersion {
		version = CurrentSchemaVersion
	}
	doc["schema_version"] = int64(version)
	return doc, version, nil
}
