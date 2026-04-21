package store

import "database/sql"

// DBForTesting exposes the underlying *sql.DB for tests in other packages.
// Not part of the production API surface.
func DBForTesting(s *Store) *sql.DB { return s.db }
