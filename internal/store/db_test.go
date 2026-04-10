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
