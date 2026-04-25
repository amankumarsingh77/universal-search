package config

func snapshotMigrators() map[int]Migrator {
	migratorsMu.Lock()
	defer migratorsMu.Unlock()
	out := make(map[int]Migrator, len(migrators))
	for k, v := range migrators {
		out[k] = v
	}
	return out
}

func restoreMigrators(snap map[int]Migrator) {
	migratorsMu.Lock()
	defer migratorsMu.Unlock()
	migrators = make(map[int]Migrator, len(snap))
	for k, v := range snap {
		migrators[k] = v
	}
}

func clearMigrators() {
	migratorsMu.Lock()
	defer migratorsMu.Unlock()
	migrators = map[int]Migrator{}
}

func setCurrentSchemaVersion(v int) {
	migratorsMu.Lock()
	defer migratorsMu.Unlock()
	CurrentSchemaVersion = v
}
