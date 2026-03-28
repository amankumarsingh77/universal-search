package store

import (
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
