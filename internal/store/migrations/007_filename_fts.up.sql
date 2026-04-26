-- Migration 007: FTS5 virtual table for fast filename search.
--
-- Creates filename_search using the trigram tokenizer so any substring query
-- (e.g. "dem" matching "demo.py") works without explicit wildcards.
-- path and file_id are UNINDEXED (stored but not tokenized) for retrieval.
--
-- Backfills from the files table which already has basename/parent/stem from
-- migration 006.
--
-- Three triggers keep the FTS table in sync with files:
--   files_ai_filename_fts  — after INSERT
--   files_ad_filename_fts  — after DELETE
--   files_au_filename_fts  — after UPDATE of the relevant columns

CREATE VIRTUAL TABLE filename_search USING fts5(
  basename,
  parent,
  stem,
  ext,
  path      UNINDEXED,
  file_id   UNINDEXED,
  tokenize  = 'trigram case_sensitive 0'
);

-- Backfill all existing rows.
INSERT INTO filename_search(rowid, basename, parent, stem, ext, path, file_id)
SELECT id, basename, parent, stem, extension, path, id
FROM files;

-- After-insert trigger: keep FTS in sync when a new file is added.
CREATE TRIGGER files_ai_filename_fts AFTER INSERT ON files BEGIN
  INSERT INTO filename_search(rowid, basename, parent, stem, ext, path, file_id)
  VALUES (NEW.id, NEW.basename, NEW.parent, NEW.stem, NEW.extension, NEW.path, NEW.id);
END;

-- After-delete trigger: remove the FTS entry when a file is deleted.
-- For regular (non-external-content) FTS5 tables, deletion is via DELETE…WHERE rowid.
CREATE TRIGGER files_ad_filename_fts AFTER DELETE ON files BEGIN
  DELETE FROM filename_search WHERE rowid = OLD.id;
END;

-- After-update trigger: replace the FTS entry when path-derived columns change.
CREATE TRIGGER files_au_filename_fts
  AFTER UPDATE OF basename, parent, stem, extension, path ON files
BEGIN
  DELETE FROM filename_search WHERE rowid = OLD.id;
  INSERT INTO filename_search(rowid, basename, parent, stem, ext, path, file_id)
  VALUES (NEW.id, NEW.basename, NEW.parent, NEW.stem, NEW.extension, NEW.path, NEW.id);
END;
