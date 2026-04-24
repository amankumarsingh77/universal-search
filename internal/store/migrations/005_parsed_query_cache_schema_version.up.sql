ALTER TABLE parsed_query_cache ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_parsed_query_cache_version
  ON parsed_query_cache(query_text_normalized, schema_version);
