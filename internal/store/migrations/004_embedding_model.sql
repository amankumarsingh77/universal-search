ALTER TABLE chunks ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';
ALTER TABLE chunks ADD COLUMN embedding_dims INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_chunks_embedding_model ON chunks(embedding_model);
