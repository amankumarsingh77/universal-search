-- Rollback: remove derived path columns and their indexes.
-- SQLite does not support DROP COLUMN before version 3.35.0 (2021-03-12).
-- ncruces/go-sqlite3 bundles a recent SQLite, so DROP COLUMN is available.
DROP INDEX IF EXISTS idx_files_basename;
DROP INDEX IF EXISTS idx_files_stem;
ALTER TABLE files DROP COLUMN basename;
ALTER TABLE files DROP COLUMN parent;
ALTER TABLE files DROP COLUMN stem;
