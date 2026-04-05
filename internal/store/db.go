package store

import (
	"database/sql"
	"fmt"
	"log/slog"
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
	ID         int64
	FileID     int64
	VectorID   string
	ChunkIndex int
	StartTime  float64
	EndTime    float64
}

// SearchResult joins chunk and file data for search responses.
type SearchResult struct {
	File      FileRecord
	ChunkID   int64
	VectorID  string
	StartTime float64
	EndTime   float64
}

const schema = `
CREATE TABLE IF NOT EXISTS files (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	path          TEXT NOT NULL UNIQUE,
	file_type     TEXT NOT NULL,
	extension     TEXT NOT NULL,
	size_bytes    INTEGER NOT NULL,
	modified_at   DATETIME NOT NULL,
	indexed_at    DATETIME NOT NULL,
	content_hash  TEXT NOT NULL DEFAULT '',
	thumbnail_path TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);
CREATE INDEX IF NOT EXISTS idx_files_content_hash ON files(content_hash);

CREATE TABLE IF NOT EXISTS chunks (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id     INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
	vector_id   TEXT NOT NULL UNIQUE,
	chunk_index INTEGER NOT NULL,
	start_time  REAL NOT NULL DEFAULT 0,
	end_time    REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_chunks_file_id ON chunks(file_id);
CREATE INDEX IF NOT EXISTS idx_chunks_vector_id ON chunks(vector_id);

CREATE TABLE IF NOT EXISTS indexed_folders (
	id   INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS excluded_patterns (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	pattern TEXT NOT NULL UNIQUE
);
`

// NewStore opens the SQLite database at dsn, enables WAL mode and foreign keys,
// and runs schema migrations.
func NewStore(dsn string, logger *slog.Logger) (*Store, error) {
	log := logger.WithGroup("store")
	log.Info("opening database", "path", dsn)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	// Enable foreign key enforcement.
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	// Run migrations.
	if _, err := db.Exec(schema); err != nil {
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
func (s *Store) InsertChunk(c ChunkRecord) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO chunks (file_id, vector_id, chunk_index, start_time, end_time)
		VALUES (?, ?, ?, ?, ?)
	`, c.FileID, c.VectorID, c.ChunkIndex, c.StartTime, c.EndTime)
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
		       c.id, c.vector_id, c.start_time, c.end_time
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
			&r.ChunkID, &r.VectorID, &r.StartTime, &r.EndTime,
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
