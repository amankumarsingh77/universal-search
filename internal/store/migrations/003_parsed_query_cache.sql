CREATE TABLE IF NOT EXISTS parsed_query_cache (
    query_text_normalized TEXT PRIMARY KEY,
    spec_json             TEXT NOT NULL,
    created_at            INTEGER NOT NULL,
    last_used_at          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_parsed_query_last_used ON parsed_query_cache(last_used_at);
