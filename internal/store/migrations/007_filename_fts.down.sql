-- Rollback migration 007: remove FTS5 triggers and virtual table.
DROP TRIGGER IF EXISTS files_au_filename_fts;
DROP TRIGGER IF EXISTS files_ad_filename_fts;
DROP TRIGGER IF EXISTS files_ai_filename_fts;
DROP TABLE IF EXISTS filename_search;
