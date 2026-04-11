package store

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestNewStore_CreatesTablesSuccessfully(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer s.Close()

	_, err = s.db.Exec(`INSERT INTO indexed_folders (path) VALUES ('/tmp/test')`)
	if err != nil {
		t.Fatalf("indexed_folders table missing: %v", err)
	}

	_, err = s.db.Exec(`INSERT INTO excluded_patterns (pattern) VALUES ('*.tmp')`)
	if err != nil {
		t.Fatalf("excluded_patterns table missing: %v", err)
	}
}

func TestUpsertFile_And_GetByPath(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	f := FileRecord{
		Path:        "/tmp/test.pdf",
		FileType:    "text",
		Extension:   ".pdf",
		SizeBytes:   1024,
		ModifiedAt:  time.Now(),
		IndexedAt:   time.Now(),
		ContentHash: "abc123",
	}

	id, err := s.UpsertFile(f)
	if err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetFileByPath("/tmp/test.pdf")
	if err != nil {
		t.Fatalf("GetFileByPath failed: %v", err)
	}
	if got.Extension != ".pdf" {
		t.Fatalf("expected .pdf, got %s", got.Extension)
	}
}

func TestInsertChunk_And_GetByVectorIDs(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fileID, _ := s.UpsertFile(FileRecord{
		Path: "/tmp/video.mp4", FileType: "video", Extension: ".mp4",
		SizeBytes: 1024, ModifiedAt: time.Now(), IndexedAt: time.Now(),
	})

	_, err = s.InsertChunk(ChunkRecord{
		FileID: fileID, VectorID: "vec-001", StartTime: 0, EndTime: 30, ChunkIndex: 0,
	})
	if err != nil {
		t.Fatalf("InsertChunk failed: %v", err)
	}

	results, err := s.GetChunksByVectorIDs([]string{"vec-001"})
	if err != nil {
		t.Fatalf("GetChunksByVectorIDs failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].File.Path != "/tmp/video.mp4" {
		t.Fatalf("expected /tmp/video.mp4, got %s", results[0].File.Path)
	}
	if results[0].VectorID != "vec-001" {
		t.Fatalf("expected vec-001, got %s", results[0].VectorID)
	}
}

func TestDeleteChunksByFileID(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fileID, _ := s.UpsertFile(FileRecord{
		Path: "/tmp/video.mp4", FileType: "video", Extension: ".mp4",
		SizeBytes: 1024, ModifiedAt: time.Now(), IndexedAt: time.Now(),
	})

	s.InsertChunk(ChunkRecord{FileID: fileID, VectorID: "vec-001", ChunkIndex: 0})
	s.InsertChunk(ChunkRecord{FileID: fileID, VectorID: "vec-002", ChunkIndex: 1})

	err = s.DeleteChunksByFileID(fileID)
	if err != nil {
		t.Fatalf("DeleteChunksByFileID failed: %v", err)
	}

	results, _ := s.GetChunksByVectorIDs([]string{"vec-001", "vec-002"})
	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}
}

func TestGetVectorIDsByFileID(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fileID, _ := s.UpsertFile(FileRecord{
		Path: "/tmp/video.mp4", FileType: "video", Extension: ".mp4",
		SizeBytes: 1024, ModifiedAt: time.Now(), IndexedAt: time.Now(),
	})

	s.InsertChunk(ChunkRecord{FileID: fileID, VectorID: "vec-001", ChunkIndex: 0})
	s.InsertChunk(ChunkRecord{FileID: fileID, VectorID: "vec-002", ChunkIndex: 1})

	ids, err := s.GetVectorIDsByFileID(fileID)
	if err != nil {
		t.Fatalf("GetVectorIDsByFileID failed: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 vector IDs, got %d", len(ids))
	}
}

func TestAddAndGetIndexedFolders(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.AddIndexedFolder("/home/user/docs")
	if err != nil {
		t.Fatalf("AddIndexedFolder failed: %v", err)
	}

	folders, err := s.GetIndexedFolders()
	if err != nil {
		t.Fatalf("GetIndexedFolders failed: %v", err)
	}
	if len(folders) != 1 || folders[0] != "/home/user/docs" {
		t.Fatalf("expected [/home/user/docs], got %v", folders)
	}
}

func TestAddAndGetExcludedPatterns(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.AddExcludedPattern("*.tmp")
	if err != nil {
		t.Fatalf("AddExcludedPattern failed: %v", err)
	}

	patterns, err := s.GetExcludedPatterns()
	if err != nil {
		t.Fatalf("GetExcludedPatterns failed: %v", err)
	}
	if len(patterns) != 1 || patterns[0] != "*.tmp" {
		t.Fatalf("expected [*.tmp], got %v", patterns)
	}
}

func TestRemoveIndexedFolder(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.AddIndexedFolder("/home/user/docs")
	if err != nil {
		t.Fatalf("AddIndexedFolder failed: %v", err)
	}

	vecIDs, err := s.RemoveIndexedFolder("/home/user/docs", false)
	if err != nil {
		t.Fatalf("RemoveIndexedFolder failed: %v", err)
	}
	if vecIDs != nil {
		t.Fatalf("expected nil vector IDs when deleteData=false, got %v", vecIDs)
	}

	folders, err := s.GetIndexedFolders()
	if err != nil {
		t.Fatalf("GetIndexedFolders failed: %v", err)
	}
	if len(folders) != 0 {
		t.Fatalf("expected folder to be removed, got %v", folders)
	}
}

func TestRemoveIndexedFolder_WithData(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.AddIndexedFolder("/home/user/docs")
	if err != nil {
		t.Fatalf("AddIndexedFolder failed: %v", err)
	}

	fileID, err := s.UpsertFile(FileRecord{
		Path: "/home/user/docs/report.pdf", FileType: "text", Extension: ".pdf",
		SizeBytes: 2048, ModifiedAt: time.Now(), IndexedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}

	_, err = s.InsertChunk(ChunkRecord{FileID: fileID, VectorID: "vec-folder-001", ChunkIndex: 0})
	if err != nil {
		t.Fatalf("InsertChunk failed: %v", err)
	}
	_, err = s.InsertChunk(ChunkRecord{FileID: fileID, VectorID: "vec-folder-002", ChunkIndex: 1})
	if err != nil {
		t.Fatalf("InsertChunk failed: %v", err)
	}

	vecIDs, err := s.RemoveIndexedFolder("/home/user/docs", true)
	if err != nil {
		t.Fatalf("RemoveIndexedFolder failed: %v", err)
	}
	if len(vecIDs) != 2 {
		t.Fatalf("expected 2 vector IDs, got %d: %v", len(vecIDs), vecIDs)
	}
	if vecIDs[0] != "vec-folder-001" && vecIDs[1] != "vec-folder-001" {
		t.Fatalf("expected vec-folder-001 in returned IDs, got %v", vecIDs)
	}
	if vecIDs[0] != "vec-folder-002" && vecIDs[1] != "vec-folder-002" {
		t.Fatalf("expected vec-folder-002 in returned IDs, got %v", vecIDs)
	}

	_, err = s.GetFileByPath("/home/user/docs/report.pdf")
	if err == nil {
		t.Fatal("expected file to be deleted, but GetFileByPath succeeded")
	}

	folders, err := s.GetIndexedFolders()
	if err != nil {
		t.Fatalf("GetIndexedFolders failed: %v", err)
	}
	if len(folders) != 0 {
		t.Fatalf("expected folder to be removed, got %v", folders)
	}
}

func TestSetAndGetSetting(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.SetSetting("hotkey", "Ctrl+Space")
	if err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}

	val, err := s.GetSetting("hotkey", "")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "Ctrl+Space" {
		t.Fatalf("expected Ctrl+Space, got %s", val)
	}
}

func TestGetSettingDefault(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	val, err := s.GetSetting("missing_key", "fallback")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "fallback" {
		t.Fatalf("expected fallback, got %s", val)
	}
}

func TestSetSettingUpsert(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.SetSetting("theme", "light")
	s.SetSetting("theme", "dark")

	val, err := s.GetSetting("theme", "")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "dark" {
		t.Fatalf("expected dark, got %s", val)
	}
}

func TestGetSettingEmptyValue(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.SetSetting("empty_key", "")
	if err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}

	val, err := s.GetSetting("empty_key", "default")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty string, got %s", val)
	}
}

func TestRemoveExcludedPattern_ExistingPattern(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.AddExcludedPattern("node_modules")
	if err != nil {
		t.Fatalf("AddExcludedPattern failed: %v", err)
	}

	err = s.RemoveExcludedPattern("node_modules")
	if err != nil {
		t.Fatalf("RemoveExcludedPattern failed: %v", err)
	}

	patterns, err := s.GetExcludedPatterns()
	if err != nil {
		t.Fatalf("GetExcludedPatterns failed: %v", err)
	}
	for _, p := range patterns {
		if p == "node_modules" {
			t.Fatal("expected node_modules to be removed, but it still exists")
		}
	}
}

func TestRemoveExcludedPattern_NonExistentPattern(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.RemoveExcludedPattern("does_not_exist")
	if err != nil {
		t.Fatalf("RemoveExcludedPattern on non-existent pattern should return nil, got: %v", err)
	}
}

func TestRemoveExcludedPattern_LastPattern(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.AddExcludedPattern(".git")
	if err != nil {
		t.Fatalf("AddExcludedPattern failed: %v", err)
	}

	err = s.RemoveExcludedPattern(".git")
	if err != nil {
		t.Fatalf("RemoveExcludedPattern failed: %v", err)
	}

	patterns, err := s.GetExcludedPatterns()
	if err != nil {
		t.Fatalf("GetExcludedPatterns failed: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("expected empty slice after removing last pattern, got %v", patterns)
	}
}

// TestHasMissingVectorBlobs_TrueWhenNull — chunk with nil VectorBlob returns true.
func TestHasMissingVectorBlobs_TrueWhenNull(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fileID, _ := s.UpsertFile(FileRecord{
		Path: "/tmp/test.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 100, ModifiedAt: time.Now(), IndexedAt: time.Now(),
	})

	// Insert chunk with nil VectorBlob — missing vector data.
	_, err = s.InsertChunk(ChunkRecord{
		FileID: fileID, VectorID: "vec-null-1", ChunkIndex: 0,
		VectorBlob: nil,
	})
	if err != nil {
		t.Fatalf("InsertChunk failed: %v", err)
	}

	has, err := s.HasMissingVectorBlobs()
	if err != nil {
		t.Fatalf("HasMissingVectorBlobs returned error: %v", err)
	}
	if !has {
		t.Fatal("expected HasMissingVectorBlobs to return true when chunk has NULL vector_blob")
	}
}

// TestHasMissingVectorBlobs_FalseWhenPresent — all chunks have VectorBlob set, returns false.
func TestHasMissingVectorBlobs_FalseWhenPresent(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fileID, _ := s.UpsertFile(FileRecord{
		Path: "/tmp/test2.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 100, ModifiedAt: time.Now(), IndexedAt: time.Now(),
	})

	// Insert chunk with valid VectorBlob.
	blob := VecToBlob([]float32{0.1, 0.2, 0.3})
	_, err = s.InsertChunk(ChunkRecord{
		FileID: fileID, VectorID: "vec-present-1", ChunkIndex: 0,
		VectorBlob: blob,
	})
	if err != nil {
		t.Fatalf("InsertChunk failed: %v", err)
	}

	has, err := s.HasMissingVectorBlobs()
	if err != nil {
		t.Fatalf("HasMissingVectorBlobs returned error: %v", err)
	}
	if has {
		t.Fatal("expected HasMissingVectorBlobs to return false when all chunks have vector_blob set")
	}
}

func TestHasAnyExcludedPattern_EmptyTable(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	has, err := s.HasAnyExcludedPattern()
	if err != nil {
		t.Fatalf("HasAnyExcludedPattern failed: %v", err)
	}
	if has {
		t.Fatal("expected false for empty table, got true")
	}
}

func TestHasAnyExcludedPattern_NonEmptyTable(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.AddExcludedPattern("vendor")
	if err != nil {
		t.Fatalf("AddExcludedPattern failed: %v", err)
	}

	has, err := s.HasAnyExcludedPattern()
	if err != nil {
		t.Fatalf("HasAnyExcludedPattern failed: %v", err)
	}
	if !has {
		t.Fatal("expected true after adding a pattern, got false")
	}
}

func TestUpdateContentHash_UpdatesHash(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	id, err := s.UpsertFile(FileRecord{
		Path: "/tmp/file.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 512, ModifiedAt: now, IndexedAt: now, ContentHash: "",
	})
	if err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}

	err = s.UpdateContentHash(id, "sha256-deadbeef")
	if err != nil {
		t.Fatalf("UpdateContentHash failed: %v", err)
	}

	got, err := s.GetFileByPath("/tmp/file.txt")
	if err != nil {
		t.Fatalf("GetFileByPath failed: %v", err)
	}
	if got.ContentHash != "sha256-deadbeef" {
		t.Fatalf("expected sha256-deadbeef, got %s", got.ContentHash)
	}
}

func TestUpdateContentHash_NonExistentID_ReturnsNil(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.UpdateContentHash(99999, "some-hash")
	if err != nil {
		t.Fatalf("expected nil error for non-existent ID, got: %v", err)
	}
}

func TestGetAllChunks_EmptyDB(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	chunks, err := s.GetAllChunks()
	if err != nil {
		t.Fatalf("GetAllChunks failed: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected empty slice, got %d chunks", len(chunks))
	}
}

func TestGetAllChunks_Returns6Chunks(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	fileID1, _ := s.UpsertFile(FileRecord{
		Path: "/tmp/file1.mp4", FileType: "video", Extension: ".mp4",
		SizeBytes: 1024, ModifiedAt: now, IndexedAt: now,
	})
	fileID2, _ := s.UpsertFile(FileRecord{
		Path: "/tmp/file2.mp4", FileType: "video", Extension: ".mp4",
		SizeBytes: 2048, ModifiedAt: now, IndexedAt: now,
	})

	for i := 0; i < 3; i++ {
		s.InsertChunk(ChunkRecord{FileID: fileID1, VectorID: fmt.Sprintf("vec-f1-%d", i), ChunkIndex: i})
		s.InsertChunk(ChunkRecord{FileID: fileID2, VectorID: fmt.Sprintf("vec-f2-%d", i), ChunkIndex: i})
	}

	chunks, err := s.GetAllChunks()
	if err != nil {
		t.Fatalf("GetAllChunks failed: %v", err)
	}
	if len(chunks) != 6 {
		t.Fatalf("expected 6 chunks, got %d", len(chunks))
	}

	// Verify each chunk has correct file_id and vector_id set.
	for _, c := range chunks {
		if c.FileID != fileID1 && c.FileID != fileID2 {
			t.Fatalf("unexpected file_id %d", c.FileID)
		}
		if c.VectorID == "" {
			t.Fatal("expected non-empty vector_id")
		}
	}
}

func TestGetFileByID_ReturnsCorrectRecord(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Truncate(time.Second)
	id, err := s.UpsertFile(FileRecord{
		Path: "/tmp/byid.pdf", FileType: "text", Extension: ".pdf",
		SizeBytes: 4096, ModifiedAt: now, IndexedAt: now, ContentHash: "hash-xyz",
	})
	if err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}

	got, err := s.GetFileByID(id)
	if err != nil {
		t.Fatalf("GetFileByID failed: %v", err)
	}
	if got.Path != "/tmp/byid.pdf" {
		t.Fatalf("expected /tmp/byid.pdf, got %s", got.Path)
	}
	if got.ContentHash != "hash-xyz" {
		t.Fatalf("expected hash-xyz, got %s", got.ContentHash)
	}
	if got.ID != id {
		t.Fatalf("expected ID %d, got %d", id, got.ID)
	}
}

func TestGetFileByID_NonExistent_ReturnsError(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, err = s.GetFileByID(99999)
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
}

func TestGetAllFiles_Returns3Files(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	paths := []string{"/tmp/a.txt", "/tmp/b.txt", "/tmp/c.txt"}
	for _, p := range paths {
		_, err := s.UpsertFile(FileRecord{
			Path: p, FileType: "text", Extension: ".txt",
			SizeBytes: 100, ModifiedAt: now, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile failed for %s: %v", p, err)
		}
	}

	files, err := s.GetAllFiles()
	if err != nil {
		t.Fatalf("GetAllFiles failed: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	got := make(map[string]bool)
	for _, f := range files {
		got[f.Path] = true
	}
	for _, p := range paths {
		if !got[p] {
			t.Fatalf("expected path %s in results", p)
		}
	}
}

// --- Query cache tests ---

func TestQueryCache_RoundTrip(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	vec := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	if err := s.SetQueryCache("hello world", vec); err != nil {
		t.Fatalf("SetQueryCache failed: %v", err)
	}

	got, err := s.GetQueryCache("hello world")
	if err != nil {
		t.Fatalf("GetQueryCache failed: %v", err)
	}
	if len(got) != len(vec) {
		t.Fatalf("expected %d floats, got %d", len(vec), len(got))
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Fatalf("mismatch at index %d: want %v, got %v", i, vec[i], got[i])
		}
	}
}

func TestQueryCache_Miss_ReturnsNilNil(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, err := s.GetQueryCache("nonexistent query")
	if err != nil {
		t.Fatalf("expected nil error on cache miss, got: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil vector on cache miss, got: %v", got)
	}
}

func TestQueryCache_Normalization(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	vec := []float32{1.0, 2.0, 3.0}
	// Store with padded and mixed-case key.
	if err := s.SetQueryCache("  Hello World  ", vec); err != nil {
		t.Fatalf("SetQueryCache failed: %v", err)
	}

	// Retrieve with normalized form.
	got, err := s.GetQueryCache("hello world")
	if err != nil {
		t.Fatalf("GetQueryCache failed: %v", err)
	}
	if len(got) != len(vec) {
		t.Fatalf("expected vector via normalized key, got nil or wrong length")
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Fatalf("mismatch at index %d: want %v, got %v", i, vec[i], got[i])
		}
	}
}

func TestQueryCache_Upsert(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	first := []float32{1.0, 2.0}
	second := []float32{9.0, 8.0}

	if err := s.SetQueryCache("query", first); err != nil {
		t.Fatalf("first SetQueryCache failed: %v", err)
	}
	if err := s.SetQueryCache("query", second); err != nil {
		t.Fatalf("second SetQueryCache failed: %v", err)
	}

	got, err := s.GetQueryCache("query")
	if err != nil {
		t.Fatalf("GetQueryCache failed: %v", err)
	}
	if len(got) != len(second) {
		t.Fatalf("expected %d floats, got %d", len(second), len(got))
	}
	for i := range second {
		if got[i] != second[i] {
			t.Fatalf("mismatch at index %d: want %v, got %v", i, second[i], got[i])
		}
	}
}

func TestQueryCache_Eviction_ZeroTTL_RemovesEntry(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	vec := []float32{0.5, 0.6}
	if err := s.SetQueryCache("evict me", vec); err != nil {
		t.Fatalf("SetQueryCache failed: %v", err)
	}

	// Zero duration means cutoff = now, so everything inserted before now is evicted.
	if err := s.EvictOldQueryCache(0); err != nil {
		t.Fatalf("EvictOldQueryCache failed: %v", err)
	}

	got, err := s.GetQueryCache("evict me")
	if err != nil {
		t.Fatalf("GetQueryCache failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected entry to be evicted, but still found: %v", got)
	}
}

func TestQueryCache_Eviction_LargeTTL_KeepsEntry(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	vec := []float32{0.5, 0.6}
	if err := s.SetQueryCache("keep me", vec); err != nil {
		t.Fatalf("SetQueryCache failed: %v", err)
	}

	// 24h TTL — a freshly inserted entry should survive.
	if err := s.EvictOldQueryCache(24 * time.Hour); err != nil {
		t.Fatalf("EvictOldQueryCache failed: %v", err)
	}

	got, err := s.GetQueryCache("keep me")
	if err != nil {
		t.Fatalf("GetQueryCache failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry to be kept, but it was evicted")
	}
}

// --- Phase 1: NL Query Understanding - Schema + Store Layer tests ---

func TestCountFiltered_FileType(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	for i := 0; i < 3; i++ {
		_, err := s.UpsertFile(FileRecord{
			Path: fmt.Sprintf("/tmp/img%d.png", i), FileType: "image", Extension: ".png",
			SizeBytes: 1024, ModifiedAt: now, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile image %d failed: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		_, err := s.UpsertFile(FileRecord{
			Path: fmt.Sprintf("/tmp/doc%d.pdf", i), FileType: "document", Extension: ".pdf",
			SizeBytes: 2048, ModifiedAt: now, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile doc %d failed: %v", i, err)
		}
	}

	count, err := s.CountFiltered(FilterSpec{
		Must: []Clause{{Field: FieldFileType, Op: OpEq, Value: "image"}},
	})
	if err != nil {
		t.Fatalf("CountFiltered failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected count=3 for image files, got %d", count)
	}
}

func TestCountFiltered_DateRange(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	for i, mt := range []time.Time{old, old, recent, recent, now} {
		_, err := s.UpsertFile(FileRecord{
			Path: fmt.Sprintf("/tmp/date%d.txt", i), FileType: "text", Extension: ".txt",
			SizeBytes: 100, ModifiedAt: mt, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile %d failed: %v", i, err)
		}
	}

	cutoff := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	count, err := s.CountFiltered(FilterSpec{
		Must: []Clause{{Field: FieldModifiedAt, Op: OpGte, Value: cutoff.Unix()}},
	})
	if err != nil {
		t.Fatalf("CountFiltered date range failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected count=3 for files after 2024, got %d", count)
	}
}

func TestFilterFileIDs_ReturnsCorrectIDs(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	var imageIDs []int64
	for i := 0; i < 2; i++ {
		id, err := s.UpsertFile(FileRecord{
			Path: fmt.Sprintf("/tmp/img%d.jpg", i), FileType: "image", Extension: ".jpg",
			SizeBytes: 512, ModifiedAt: now, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile failed: %v", err)
		}
		imageIDs = append(imageIDs, id)
	}
	for i := 0; i < 3; i++ {
		_, err := s.UpsertFile(FileRecord{
			Path: fmt.Sprintf("/tmp/audio%d.mp3", i), FileType: "audio", Extension: ".mp3",
			SizeBytes: 4096, ModifiedAt: now, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile audio failed: %v", err)
		}
	}

	ids, err := s.FilterFileIDs(FilterSpec{
		Must: []Clause{{Field: FieldFileType, Op: OpEq, Value: "image"}},
	})
	if err != nil {
		t.Fatalf("FilterFileIDs failed: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d: %v", len(ids), ids)
	}
	idSet := make(map[int64]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	for _, expected := range imageIDs {
		if !idSet[expected] {
			t.Fatalf("expected ID %d in results, got %v", expected, ids)
		}
	}
}

func TestGetVectorBlobs_RoundTrip(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	fileID, err := s.UpsertFile(FileRecord{
		Path: "/tmp/vec.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 100, ModifiedAt: now, IndexedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	vec1 := []float32{0.1, 0.2, 0.3}
	vec2 := []float32{0.4, 0.5, 0.6}
	_, err = s.InsertChunk(ChunkRecord{
		FileID: fileID, VectorID: "vec-blob-001", ChunkIndex: 0, VectorBlob: vecToBlob(vec1),
	})
	if err != nil {
		t.Fatalf("InsertChunk with blob failed: %v", err)
	}
	_, err = s.InsertChunk(ChunkRecord{
		FileID: fileID, VectorID: "vec-blob-002", ChunkIndex: 1, VectorBlob: vecToBlob(vec2),
	})
	if err != nil {
		t.Fatalf("InsertChunk with blob 2 failed: %v", err)
	}

	blobs, err := s.GetVectorBlobs([]int64{fileID})
	if err != nil {
		t.Fatalf("GetVectorBlobs failed: %v", err)
	}
	vecs, ok := blobs[fileID]
	if !ok {
		t.Fatalf("expected fileID %d in result map", fileID)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors for file, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 3 {
			t.Fatalf("vector %d: expected 3 dims, got %d", i, len(v))
		}
	}
}

func TestGetVectorBlobs_SkipsNullBlob(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	fileID, err := s.UpsertFile(FileRecord{
		Path: "/tmp/noblob.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 100, ModifiedAt: now, IndexedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert chunk with no VectorBlob (nil/empty)
	_, err = s.InsertChunk(ChunkRecord{
		FileID: fileID, VectorID: "vec-noblob-001", ChunkIndex: 0,
	})
	if err != nil {
		t.Fatalf("InsertChunk no-blob failed: %v", err)
	}
	// Insert chunk with a blob
	_, err = s.InsertChunk(ChunkRecord{
		FileID: fileID, VectorID: "vec-noblob-002", ChunkIndex: 1,
		VectorBlob: vecToBlob([]float32{1.0, 2.0}),
	})
	if err != nil {
		t.Fatalf("InsertChunk with blob failed: %v", err)
	}

	blobs, err := s.GetVectorBlobs([]int64{fileID})
	if err != nil {
		t.Fatalf("GetVectorBlobs failed: %v", err)
	}
	vecs := blobs[fileID]
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector (null blob skipped), got %d", len(vecs))
	}
}

func TestUpsertParsedQueryCache_RoundTrip(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	specJSON := `{"semantic_query":"find images","must":[{"field":"file_type","op":"eq","value":"image"}]}`
	err = s.UpsertParsedQueryCache("find images", specJSON)
	if err != nil {
		t.Fatalf("UpsertParsedQueryCache failed: %v", err)
	}

	got, err := s.GetParsedQueryCache("find images")
	if err != nil {
		t.Fatalf("GetParsedQueryCache failed: %v", err)
	}
	if got != specJSON {
		t.Fatalf("expected %q, got %q", specJSON, got)
	}
}

func TestUpsertParsedQueryCache_Miss_ReturnsEmpty(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, err := s.GetParsedQueryCache("nonexistent query")
	if err != nil {
		t.Fatalf("GetParsedQueryCache miss should return nil error, got: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string on miss, got %q", got)
	}
}

func TestEvictOldParsedQueryCache_LeavesRecent(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Seed 5 old entries by direct SQL
	oldTS := time.Now().Add(-31 * 24 * time.Hour).Unix()
	for i := 0; i < 5; i++ {
		_, err := s.db.Exec(`INSERT INTO parsed_query_cache (query_text_normalized, spec_json, created_at, last_used_at) VALUES (?,?,?,?)`,
			fmt.Sprintf("old query %d", i), `{}`, oldTS, oldTS)
		if err != nil {
			t.Fatalf("insert old entry %d failed: %v", i, err)
		}
	}

	// Seed 5 recent entries
	for i := 0; i < 5; i++ {
		err := s.UpsertParsedQueryCache(fmt.Sprintf("recent query %d", i), `{}`)
		if err != nil {
			t.Fatalf("UpsertParsedQueryCache recent %d failed: %v", i, err)
		}
	}

	err = s.EvictOldParsedQueryCache()
	if err != nil {
		t.Fatalf("EvictOldParsedQueryCache failed: %v", err)
	}

	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM parsed_query_cache`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("expected 5 remaining entries after eviction, got %d", count)
	}
}

func TestSearchFilenameContains_FindsMatch(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	paths := []string{
		"/home/user/documents/report_2025.pdf",
		"/home/user/pictures/vacation.jpg",
		"/home/user/documents/annual_report.docx",
		"/tmp/unrelated.txt",
	}
	for _, p := range paths {
		_, err := s.UpsertFile(FileRecord{
			Path: p, FileType: "text", Extension: ".txt",
			SizeBytes: 100, ModifiedAt: now, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile %s failed: %v", p, err)
		}
	}

	results, err := s.SearchFilenameContains("report")
	if err != nil {
		t.Fatalf("SearchFilenameContains failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results containing 'report', got %d: %v", len(results), func() []string {
			var ps []string
			for _, r := range results {
				ps = append(ps, r.Path)
			}
			return ps
		}())
	}
}

func TestCountFiles_ReturnsTotal(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	for i := 0; i < 7; i++ {
		_, err := s.UpsertFile(FileRecord{
			Path: fmt.Sprintf("/tmp/file%d.txt", i), FileType: "text", Extension: ".txt",
			SizeBytes: 100, ModifiedAt: now, IndexedAt: now,
		})
		if err != nil {
			t.Fatalf("UpsertFile %d failed: %v", i, err)
		}
	}

	count, err := s.CountFiles()
	if err != nil {
		t.Fatalf("CountFiles failed: %v", err)
	}
	if count != 7 {
		t.Fatalf("expected 7 files, got %d", count)
	}
}

func TestAlterTable_Idempotent(t *testing.T) {
	// Use a temp file to test idempotency across two NewStore calls
	dir := t.TempDir()
	dbPath := dir + "/test.db"

	s1, err := NewStore(dbPath, testLogger)
	if err != nil {
		t.Fatalf("first NewStore failed: %v", err)
	}
	s1.Close()

	s2, err := NewStore(dbPath, testLogger)
	if err != nil {
		t.Fatalf("second NewStore failed (ALTER TABLE not idempotent): %v", err)
	}
	s2.Close()
}

// TestSearchResult_HasDistanceField verifies the Distance field exists on SearchResult.
// This is a compile-time check — the test fails to compile if the field is absent.
func TestSearchResult_HasDistanceField(t *testing.T) {
	r := SearchResult{
		Distance: 0.25,
	}
	if r.Distance != 0.25 {
		t.Fatalf("expected Distance 0.25, got %v", r.Distance)
	}
}

func TestUpsertFile_UpdatesExisting(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	f := FileRecord{
		Path: "/tmp/test.pdf", FileType: "text", Extension: ".pdf",
		SizeBytes: 1024, ModifiedAt: now, IndexedAt: now, ContentHash: "abc123",
	}

	id1, _ := s.UpsertFile(f)

	f.SizeBytes = 2048
	f.ContentHash = "def456"
	id2, _ := s.UpsertFile(f)

	if id1 != id2 {
		t.Fatalf("expected same ID on upsert, got %d and %d", id1, id2)
	}

	got, _ := s.GetFileByPath("/tmp/test.pdf")
	if got.SizeBytes != 2048 {
		t.Fatalf("expected updated size 2048, got %d", got.SizeBytes)
	}
	if got.ContentHash != "def456" {
		t.Fatalf("expected updated hash def456, got %s", got.ContentHash)
	}
}
