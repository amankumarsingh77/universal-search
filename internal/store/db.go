package store

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// Store wraps a SQLite database for file and chunk metadata.
type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

// FileRecord represents a file tracked by the indexer.
type FileRecord struct {
	ID           int64
	Path         string
	FileType     string
	Extension    string
	SizeBytes    int64
	ModifiedAt   time.Time
	IndexedAt    time.Time
	ContentHash  string
	ThumbnailPath string
}

// ChunkRecord represents a chunk of a file (e.g., a time segment of video/audio).
type ChunkRecord struct {
	ID             int64
	FileID         int64
	VectorID       string
	ChunkIndex     int
	StartTime      float64
	EndTime        float64
	VectorBlob     []byte // optional: raw little-endian float32 vector stored inline
	EmbeddingModel string
	EmbeddingDims  int
}

// SearchResult joins chunk and file data for search responses.
type SearchResult struct {
	File           FileRecord
	ChunkID        int64
	VectorID       string
	StartTime      float64
	EndTime        float64
	Distance       float32 // cosine distance from vectorstore (0=identical, 2=opposite)
	FinalScore     float32 // reranked score (0–1+); set by search.Rerank, 0 if not reranked
	EmbeddingModel string  // empty if the chunk predates migration 004
}

// NewStore opens the SQLite database at dsn, enables WAL mode and foreign keys,
// and runs schema migrations.
func NewStore(dsn string, logger *slog.Logger) (*Store, error) {
	return NewStoreWithBackfill(dsn, logger, nil)
}

// NewStoreWithBackfill opens the database and runs migrations, invoking the
// given backfill callback if migration 004 applies during this open (typically
// the first launch after upgrading from a pre-embedding-model-gate install).
func NewStoreWithBackfill(dsn string, logger *slog.Logger, backfill BackfillFunc) (*Store, error) {
	log := logger.WithGroup("store")
	log.Info("opening database", "path", dsn)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := ApplyWithBackfill(db, log, backfill); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	log.Info("database ready")
	return &Store{db: db, logger: log}, nil
}

// Close checkpoints the WAL and closes the underlying database connection.
func (s *Store) Close() error {
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

// UpsertFile inserts or updates a file record by path. Returns the file ID.
func (s *Store) UpsertFile(f FileRecord) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO files (path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			file_type      = excluded.file_type,
			extension      = excluded.extension,
			size_bytes     = excluded.size_bytes,
			modified_at    = excluded.modified_at,
			indexed_at     = excluded.indexed_at,
			content_hash   = excluded.content_hash,
			thumbnail_path = excluded.thumbnail_path
	`, f.Path, f.FileType, f.Extension, f.SizeBytes, f.ModifiedAt, f.IndexedAt, f.ContentHash, f.ThumbnailPath)
	if err != nil {
		return 0, fmt.Errorf("upsert file: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	// On conflict update, LastInsertId may return 0. Query the actual ID.
	if id == 0 {
		err = s.db.QueryRow(`SELECT id FROM files WHERE path = ?`, f.Path).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("get file id: %w", err)
		}
	}

	return id, nil
}

// GetFileByPath retrieves a file record by its path.
func (s *Store) GetFileByPath(path string) (FileRecord, error) {
	var f FileRecord
	err := s.db.QueryRow(`
		SELECT id, path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path
		FROM files WHERE path = ?
	`, path).Scan(&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes, &f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath)
	if err != nil {
		return f, fmt.Errorf("get file by path: %w", err)
	}
	return f, nil
}

// InsertChunk inserts a new chunk record. Returns the chunk ID.
// If VectorBlob is set it is stored inline; on conflict the vector_blob is upserted.
func (s *Store) InsertChunk(c ChunkRecord) (int64, error) {
	var blob any
	if len(c.VectorBlob) > 0 {
		blob = c.VectorBlob
	}
	res, err := s.db.Exec(`
		INSERT INTO chunks (file_id, vector_id, chunk_index, start_time, end_time, vector_blob, embedding_model, embedding_dims)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vector_id) DO UPDATE SET
			vector_blob     = excluded.vector_blob,
			embedding_model = excluded.embedding_model,
			embedding_dims  = excluded.embedding_dims
	`, c.FileID, c.VectorID, c.ChunkIndex, c.StartTime, c.EndTime, blob, c.EmbeddingModel, c.EmbeddingDims)
	if err != nil {
		return 0, fmt.Errorf("insert chunk: %w", err)
	}
	return res.LastInsertId()
}

// DeleteChunksByFileID removes all chunks associated with a file.
func (s *Store) DeleteChunksByFileID(fileID int64) error {
	_, err := s.db.Exec(`DELETE FROM chunks WHERE file_id = ?`, fileID)
	if err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	return nil
}

// GetChunksByVectorIDs retrieves search results for the given vector IDs,
// joining chunk and file data.
func (s *Store) GetChunksByVectorIDs(vectorIDs []string) ([]SearchResult, error) {
	if len(vectorIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(vectorIDs))
	args := make([]interface{}, len(vectorIDs))
	for i, id := range vectorIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT f.id, f.path, f.file_type, f.extension, f.size_bytes, f.modified_at, f.indexed_at, f.content_hash, f.thumbnail_path,
		       c.id, c.vector_id, c.start_time, c.end_time, c.embedding_model
		FROM chunks c
		JOIN files f ON f.id = c.file_id
		WHERE c.vector_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get chunks by vector ids: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		err := rows.Scan(
			&r.File.ID, &r.File.Path, &r.File.FileType, &r.File.Extension,
			&r.File.SizeBytes, &r.File.ModifiedAt, &r.File.IndexedAt,
			&r.File.ContentHash, &r.File.ThumbnailPath,
			&r.ChunkID, &r.VectorID, &r.StartTime, &r.EndTime, &r.EmbeddingModel,
		)
		if err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetVectorIDsByFileID returns all vector IDs associated with a file.
func (s *Store) GetVectorIDsByFileID(fileID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT vector_id FROM chunks WHERE file_id = ?`, fileID)
	if err != nil {
		return nil, fmt.Errorf("get vector ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan vector id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RemoveFileByPath deletes a file and its chunks by path, returning the
// associated vector IDs for removal from the vector store.
func (s *Store) RemoveFileByPath(path string) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var fileID int64
	err = tx.QueryRow(`SELECT id FROM files WHERE path = ?`, path).Scan(&fileID)
	if err != nil {
		return nil, fmt.Errorf("find file: %w", err)
	}

	rows, err := tx.Query(`SELECT vector_id FROM chunks WHERE file_id = ?`, fileID)
	if err != nil {
		return nil, fmt.Errorf("collect vector ids: %w", err)
	}
	var vecIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan vector id: %w", err)
		}
		vecIDs = append(vecIDs, id)
	}
	rows.Close()

	if _, err := tx.Exec(`DELETE FROM chunks WHERE file_id = ?`, fileID); err != nil {
		return nil, fmt.Errorf("delete chunks: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE id = ?`, fileID); err != nil {
		return nil, fmt.Errorf("delete file: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	s.logger.Info("removed file", "path", path, "vectors", len(vecIDs))
	return vecIDs, nil
}

// AddIndexedFolder adds a folder path to the indexed folders list.
func (s *Store) AddIndexedFolder(path string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO indexed_folders (path) VALUES (?)`, path)
	if err != nil {
		return fmt.Errorf("add indexed folder: %w", err)
	}
	return nil
}

// GetIndexedFolders returns all indexed folder paths.
func (s *Store) GetIndexedFolders() ([]string, error) {
	rows, err := s.db.Query(`SELECT path FROM indexed_folders`)
	if err != nil {
		return nil, fmt.Errorf("get indexed folders: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan folder path: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// AddExcludedPattern adds a glob pattern to the exclusion list.
func (s *Store) AddExcludedPattern(pattern string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO excluded_patterns (pattern) VALUES (?)`, pattern)
	if err != nil {
		return fmt.Errorf("add excluded pattern: %w", err)
	}
	return nil
}

// RemoveIndexedFolder removes a folder from the indexed folders list.
// If deleteData is true, all files and chunks under that path prefix are also deleted,
// and the associated vector IDs are returned for removal from the vector store.
func (s *Store) RemoveIndexedFolder(path string, deleteData bool) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if deleteData {
		rows, err := tx.Query(`
			SELECT c.vector_id FROM chunks c
			JOIN files f ON f.id = c.file_id
			WHERE f.path LIKE ? || '%'
		`, path)
		if err != nil {
			return nil, fmt.Errorf("collect vector ids: %w", err)
		}

		var vecIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan vector id: %w", err)
			}
			vecIDs = append(vecIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate vector ids: %w", err)
		}

		if _, err := tx.Exec(`DELETE FROM files WHERE path LIKE ? || '%'`, path); err != nil {
			return nil, fmt.Errorf("delete files for folder: %w", err)
		}

		if _, err := tx.Exec(`DELETE FROM indexed_folders WHERE path = ?`, path); err != nil {
			return nil, fmt.Errorf("remove indexed folder: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		s.logger.Info("removed folder with data", "path", path, "vectorsRemoved", len(vecIDs))
		return vecIDs, nil
	}

	if _, err := tx.Exec(`DELETE FROM indexed_folders WHERE path = ?`, path); err != nil {
		return nil, fmt.Errorf("remove indexed folder: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	s.logger.Info("removed folder (data kept)", "path", path)
	return nil, nil
}

// GetSetting returns the value for a key, or defaultVal if not found.
func (s *Store) GetSetting(key, defaultVal string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return defaultVal, nil
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetSetting inserts or updates a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

// RemoveExcludedPattern removes a folder name glob from the exclusion list.
// Returns nil if the pattern does not exist.
func (s *Store) RemoveExcludedPattern(pattern string) error {
	_, err := s.db.Exec(`DELETE FROM excluded_patterns WHERE pattern = ?`, pattern)
	if err != nil {
		return fmt.Errorf("remove excluded pattern: %w", err)
	}
	return nil
}

// HasAnyExcludedPattern returns true if the excluded_patterns table is non-empty.
func (s *Store) HasAnyExcludedPattern() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM excluded_patterns`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count excluded patterns: %w", err)
	}
	return count > 0, nil
}

// UpdateContentHash updates the content_hash field for a file by ID.
// It is used in the two-phase commit pattern: the file is inserted first with an
// empty hash, then the hash is set after the content has been fully processed.
// If no row matches the given ID, the function returns nil (0 rows affected is acceptable).
func (s *Store) UpdateContentHash(fileID int64, hash string) error {
	_, err := s.db.Exec(`UPDATE files SET content_hash = ? WHERE id = ?`, hash, fileID)
	if err != nil {
		return fmt.Errorf("update content hash: %w", err)
	}
	return nil
}

// GetAllChunks returns every chunk record in the database, including VectorBlob.
// It is used by the reconciliation pass to detect orphaned vector entries.
func (s *Store) GetAllChunks() ([]ChunkRecord, error) {
	rows, err := s.db.Query(`SELECT id, file_id, vector_id, chunk_index, start_time, end_time, vector_blob FROM chunks`)
	if err != nil {
		return nil, fmt.Errorf("get all chunks: %w", err)
	}
	defer rows.Close()
	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.FileID, &c.VectorID, &c.ChunkIndex, &c.StartTime, &c.EndTime, &c.VectorBlob); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// GetFileByID retrieves a file record by its primary key.
// It is needed by the reconciliation pass to look up a file path from a chunk's file_id.
func (s *Store) GetFileByID(id int64) (FileRecord, error) {
	var f FileRecord
	err := s.db.QueryRow(`
		SELECT id, path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path
		FROM files WHERE id = ?
	`, id).Scan(&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
		&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath)
	if err != nil {
		return f, fmt.Errorf("get file by id: %w", err)
	}
	return f, nil
}

// GetFilesByIDs returns FileRecords for the given set of file IDs in a single query.
func (s *Store) GetFilesByIDs(ids []int64) ([]FileRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.Query(
		`SELECT id, path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path
		 FROM files WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("get files by ids: %w", err)
	}
	defer rows.Close()
	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
			&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetAllFiles returns every file record in the database.
// It is used by the startup rescan to detect and remove files that no longer exist on disk.
func (s *Store) GetAllFiles() ([]FileRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path
		FROM files
	`)
	if err != nil {
		return nil, fmt.Errorf("get all files: %w", err)
	}
	defer rows.Close()
	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
			&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetIncompleteFiles returns files whose content_hash is empty — these
// started indexing but never completed (e.g. interrupted mid-flight).
func (s *Store) GetIncompleteFiles() ([]FileRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path
		FROM files WHERE content_hash = ''
	`)
	if err != nil {
		return nil, fmt.Errorf("get incomplete files: %w", err)
	}
	defer rows.Close()
	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
			&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetExcludedPatterns returns all excluded glob patterns.
func (s *Store) GetExcludedPatterns() ([]string, error) {
	rows, err := s.db.Query(`SELECT pattern FROM excluded_patterns`)
	if err != nil {
		return nil, fmt.Errorf("get excluded patterns: %w", err)
	}
	defer rows.Close()

	var patterns []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan pattern: %w", err)
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

// GetQueryCache returns the cached vector for query, or nil if not found.
// query is normalized (trimmed + lowercased) before lookup.
func (s *Store) GetQueryCache(query string) ([]float32, error) {
	q := normalizeQuery(query)
	var blob []byte
	err := s.db.QueryRow(`SELECT vector FROM query_cache WHERE query = ?`, q).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get query cache: %w", err)
	}
	return blobToVec(blob)
}

// SetQueryCache stores or updates the cached vector for query.
// query is normalized (trimmed + lowercased) before storage.
func (s *Store) SetQueryCache(query string, vec []float32) error {
	q := normalizeQuery(query)
	blob := vecToBlob(vec)
	_, err := s.db.Exec(
		`INSERT INTO query_cache (query, vector, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(query) DO UPDATE SET vector = excluded.vector, created_at = excluded.created_at`,
		q, blob, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("set query cache: %w", err)
	}
	return nil
}

// EvictOldQueryCache deletes all query cache entries older than maxAge.
func (s *Store) EvictOldQueryCache(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge).Unix()
	_, err := s.db.Exec(`DELETE FROM query_cache WHERE created_at <= ?`, cutoff)
	if err != nil {
		return fmt.Errorf("evict query cache: %w", err)
	}
	return nil
}

// CountFiltered returns the number of files matching the given FilterSpec.
func (s *Store) CountFiltered(spec FilterSpec) (int, error) {
	where, args, err := buildWhereClause(spec.Must, spec.MustNot)
	if err != nil {
		return 0, fmt.Errorf("build where clause: %w", err)
	}

	query := `SELECT COUNT(*) FROM files`
	if where != "" {
		query += " WHERE " + where
	}

	var count int
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count filtered: %w", err)
	}
	return count, nil
}

// FilterFileIDs returns the file IDs matching the given FilterSpec.
func (s *Store) FilterFileIDs(spec FilterSpec) ([]int64, error) {
	where, args, err := buildWhereClause(spec.Must, spec.MustNot)
	if err != nil {
		return nil, fmt.Errorf("build where clause: %w", err)
	}

	query := `SELECT id FROM files`
	if where != "" {
		query += " WHERE " + where
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("filter file ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan file id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetVectorBlobs retrieves all stored vector blobs for the given file IDs.
// Chunks with a NULL vector_blob are skipped. Returns map[fileID][]vectors.
func (s *Store) GetVectorBlobs(fileIDs []int64) (map[int64][][]float32, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(fileIDs))
	args := make([]any, len(fileIDs))
	for i, id := range fileIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT file_id, vector_blob FROM chunks WHERE file_id IN (%s) AND vector_blob IS NOT NULL`,
		strings.Join(placeholders, ","),
	)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get vector blobs: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][][]float32)
	for rows.Next() {
		var fileID int64
		var blob []byte
		if err := rows.Scan(&fileID, &blob); err != nil {
			return nil, fmt.Errorf("scan vector blob: %w", err)
		}
		vec, err := blobToVec(blob)
		if err != nil {
			return nil, fmt.Errorf("decode vector blob for file %d: %w", fileID, err)
		}
		result[fileID] = append(result[fileID], vec)
	}
	return result, rows.Err()
}

// UpsertParsedQueryCache stores a parsed query spec, keyed by normalized query text
// and schema version. The primary key is (query_text_normalized), so inserting a row
// with the same key but a different schema_version replaces the prior entry.
func (s *Store) UpsertParsedQueryCache(normalizedQuery, specJSON string, schemaVersion int) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO parsed_query_cache (query_text_normalized, spec_json, schema_version, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?)
	`, normalizedQuery, specJSON, schemaVersion, now, now)
	if err != nil {
		return fmt.Errorf("upsert parsed query cache: %w", err)
	}
	return nil
}

// GetParsedQueryCache returns the cached spec JSON for normalizedQuery at the given
// schema version. Returns "", nil on a cache miss or version mismatch. Updates
// last_used_at on a hit.
func (s *Store) GetParsedQueryCache(normalizedQuery string, schemaVersion int) (string, error) {
	var specJSON string
	err := s.db.QueryRow(
		`SELECT spec_json FROM parsed_query_cache WHERE query_text_normalized = ? AND schema_version = ?`,
		normalizedQuery, schemaVersion,
	).Scan(&specJSON)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get parsed query cache: %w", err)
	}

	// Update last_used_at on hit.
	_, _ = s.db.Exec(
		`UPDATE parsed_query_cache SET last_used_at = ? WHERE query_text_normalized = ? AND schema_version = ?`,
		time.Now().Unix(), normalizedQuery, schemaVersion,
	)
	return specJSON, nil
}

// EvictOldParsedQueryCache deletes cache entries not used in the last 30 days.
func (s *Store) EvictOldParsedQueryCache() error {
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	_, err := s.db.Exec(`DELETE FROM parsed_query_cache WHERE last_used_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("evict parsed query cache: %w", err)
	}
	return nil
}

// SearchFilenameContains returns up to 50 files whose path contains query as a substring.
func (s *Store) SearchFilenameContains(query string) ([]FileRecord, error) {
	escaped := escapeLike(query)
	rows, err := s.db.Query(
		`SELECT id, path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path`+
			` FROM files WHERE path LIKE '%' || ? || '%' ESCAPE '\' LIMIT 50`,
		escaped)
	if err != nil {
		return nil, fmt.Errorf("search filename contains: %w", err)
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
			&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// CountFiles returns the total number of files in the database.
func (s *Store) CountFiles() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count files: %w", err)
	}
	return count, nil
}

// normalizeQuery lowercases and trims whitespace from a query string.
func normalizeQuery(q string) string {
	return strings.ToLower(strings.TrimSpace(q))
}

// VecToBlob encodes a float32 slice as a little-endian byte slice.
// Exported so the indexer and other packages can encode embeddings for storage.
func VecToBlob(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// vecToBlob is an internal alias kept for callers within this package.
func vecToBlob(vec []float32) []byte { return VecToBlob(vec) }

// BlobToVec decodes a little-endian byte slice into a float32 slice.
// Exported so the indexer and tests can decode stored vector blobs.
func BlobToVec(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("invalid vector blob length %d", len(blob))
	}
	vec := make([]float32, len(blob)/4)
	for i := range vec {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		vec[i] = math.Float32frombits(bits)
	}
	return vec, nil
}

// blobToVec is an internal alias kept for callers within this package.
func blobToVec(blob []byte) ([]float32, error) { return BlobToVec(blob) }

// ModelsInIndex returns the distinct embedding_model values currently stored
// in the chunks table. Empty-string values (present after migration 004 but
// before backfill has committed a real model) are excluded.
func (s *Store) ModelsInIndex() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT embedding_model FROM chunks WHERE embedding_model != ''`)
	if err != nil {
		return nil, fmt.Errorf("models in index: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountChunksByModel returns the number of chunk rows whose embedding_model
// equals modelID.
func (s *Store) CountChunksByModel(modelID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE embedding_model = ?`, modelID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count chunks by model: %w", err)
	}
	return count, nil
}

// HasMissingVectorBlobs returns true if any chunks row has NULL vector_blob.
func (s *Store) HasMissingVectorBlobs() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE vector_blob IS NULL`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
