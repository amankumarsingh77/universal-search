-- Add derived path columns to files for fast filename search.
ALTER TABLE files ADD COLUMN basename TEXT NOT NULL DEFAULT '';
ALTER TABLE files ADD COLUMN parent   TEXT NOT NULL DEFAULT '';
ALTER TABLE files ADD COLUMN stem     TEXT NOT NULL DEFAULT '';

-- Indexes are created here; the backfill UPDATE is intentionally omitted.
--
-- EDGE-4: On databases with large numbers of existing rows (e.g. 500k files),
-- a single UPDATE touching every row would block all WAL writers for tens of
-- seconds. Instead, the backfill is performed by the Go migrator in 5000-row
-- batches (see backfill006 in internal/store/migrator.go), each in its own
-- transaction, so WAL writers can interleave between batches.
--
-- Path decomposition formula used by the Go backfill:
--
--   last_slash_len = length(rtrim(path, replace(path,'/','')))
--
--   basename = substr(path, last_slash_len + 1)
--
--   parent   = '' when no slash
--            | '/' when last slash is at position 1 (directly under root)
--            | substr(path, 1, last_slash_len - 1) otherwise
--
--   stem:  last-dot trick on basename; a leading dot (e.g. ".bashrc") is NOT
--          treated as an extension separator.
--            stem = basename  when instr(basename,'.') <= 1
--            stem = substr(basename, 1, last_dot_len - 1) otherwise

CREATE INDEX IF NOT EXISTS idx_files_basename ON files(basename);
CREATE INDEX IF NOT EXISTS idx_files_stem     ON files(stem);
