package indexer

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"findo/internal/chunker"
	"findo/internal/embedder"
	"findo/internal/store"
)

func TestCheckStale_Unchanged(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, _ := hashFile(path)
	info, _ := os.Stat(path)
	if _, err := s.UpsertFile(store.FileRecord{
		Path:        path,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	stale, gotInfo, gotHash, err := p.checkStale(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stale {
		t.Fatal("expected stale=false for matching hash")
	}
	if gotHash != hash {
		t.Fatalf("hash mismatch: got %q want %q", gotHash, hash)
	}
	if gotInfo == nil {
		t.Fatal("expected non-nil FileInfo")
	}
}

func TestCheckStale_ForceBypassesHash(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, _ := hashFile(path)
	info, _ := os.Stat(path)
	if _, err := s.UpsertFile(store.FileRecord{
		Path:        path,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	stale, _, _, err := p.checkStale(path, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Fatal("expected stale=true when force=true even if hash matches")
	}
}

func TestCheckStale_MissingFile(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)
	_, _, _, err := p.checkStale(filepath.Join(t.TempDir(), "nope.txt"), false)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestChunk_EmptyFile(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, _, err := p.chunk(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty file, got %d", len(chunks))
	}
}

func TestGenerateThumbnail_LogsOnError(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)

	// Write a file named .jpg with invalid bytes — thumbnail gen should fail
	// internally but not panic or return anything.
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jpg")
	if err := os.WriteFile(path, []byte("not a real jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("generateThumbnail panicked: %v", r)
		}
	}()
	// Return value is the thumb path; may be "" on failure — we don't care.
	_ = p.generateThumbnail(path, info, chunker.TypeImage)
}

func TestUpsertPending_WritesEmptyHash(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)

	fileID, err := p.upsertPending(path, info, chunker.TypeText, "")
	if err != nil {
		t.Fatalf("upsertPending: %v", err)
	}
	if fileID == 0 {
		t.Fatal("expected non-zero fileID")
	}
	rec, err := s.GetFileByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ContentHash != "" {
		t.Fatalf("expected empty ContentHash after upsertPending, got %q", rec.ContentHash)
	}
}

func TestEmbedBatched_QuotaError_SetsPaused(t *testing.T) {
	mock := &mockEmbedder{err: fmt.Errorf("all keys exhausted")}
	p, _, _ := newTestPipelineWithMock(t, mock, nil, 1)

	dir := t.TempDir()
	path := filepath.Join(dir, "q.txt")
	content := "some content for embedding"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	chunks := []chunker.Chunk{{Index: 0, Text: content}}
	_, _, err := p.embedBatched(context.Background(), path, 1, chunks, "hash")
	if err == nil {
		t.Fatal("expected quota error to propagate")
	}
	st := p.Status()
	if !st.QuotaPaused {
		t.Fatal("expected QuotaPaused=true after quota-exhausted error")
	}
	if st.QuotaResumeAt == "" {
		t.Fatal("expected QuotaResumeAt to be populated")
	}
}

func TestStoreChunks_PeriodicSave(t *testing.T) {
	calls := 0
	onDone := func() { calls++ }
	p, _, _ := newTestPipeline(t, onDone)
	p.saveEveryN = 3

	chunks := make([]chunker.Chunk, 6)
	vecs := make([][]float32, 6)
	for i := 0; i < 6; i++ {
		chunks[i] = chunker.Chunk{Index: i, Text: fmt.Sprintf("c%d", i)}
		vecs[i] = []float32{float32(i), 0.1, 0.2}
	}

	if err := p.storeChunks(1, chunks, vecs); err != nil {
		t.Fatalf("storeChunks: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected onJobDone called 2 times for 6 chunks at saveEveryN=3, got %d", calls)
	}
}

func TestCommit_SetsFinalHash(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	fileID, err := p.upsertPending(path, info, chunker.TypeText, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "deadbeef"
	if err := p.commit(fileID, want); err != nil {
		t.Fatalf("commit: %v", err)
	}
	rec, err := s.GetFileByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ContentHash != want {
		t.Fatalf("expected hash %q, got %q", want, rec.ContentHash)
	}
}

// genBumpingEmbedder bumps the pipeline's generation inside EmbedBatch so the
// coordinator's post-embed generation check aborts storeChunks/commit.
type genBumpingEmbedder struct {
	p *Pipeline
}

func (g *genBumpingEmbedder) ModelID() string        { return "mock" }
func (g *genBumpingEmbedder) Dimensions() int        { return 3 }
func (g *genBumpingEmbedder) PausedUntil() time.Time { return time.Time{} }
func (g *genBumpingEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{0, 0, 0}, nil
}
func (g *genBumpingEmbedder) EmbedBatch(_ context.Context, chunks []embedder.ChunkInput) ([][]float32, error) {
	g.p.ResetStatus()
	out := make([][]float32, len(chunks))
	for i := range chunks {
		out[i] = []float32{float32(i), 0.1, 0.2}
	}
	return out, nil
}

func TestIndexFile_GenerationAbortAfterEmbed(t *testing.T) {
	p, s, _ := newTestPipelineWithMock(t, &mockEmbedder{}, nil, 1)
	p.SetEmbedder(&genBumpingEmbedder{p: p})

	dir := t.TempDir()
	path := filepath.Join(dir, "abort.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("content for gen abort test. ", 20)), 0o644); err != nil {
		t.Fatal(err)
	}

	err := p.indexFile(p.ctx, path, true)
	if !errors.Is(err, errStaleGeneration) {
		t.Fatalf("expected errStaleGeneration, got: %v", err)
	}

	rec, dbErr := s.GetFileByPath(path)
	if dbErr == nil && rec.ContentHash != "" {
		t.Fatalf("expected empty ContentHash after stale-generation abort, got %q", rec.ContentHash)
	}

	chunks, err := s.GetAllChunks()
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks written on stale generation, got %d", len(chunks))
	}
}

// TestGenerationAbort — REF-084 named alias. Delegates to the full test.
func TestGenerationAbort(t *testing.T) { TestIndexFile_GenerationAbortAfterEmbed(t) }

// TestIndexFile_BodySizeUnder60Lines parses pipeline.go and asserts that the
// body of indexFile (between the opening and closing braces) is under 60 lines.
func TestIndexFile_BodySizeUnder60Lines(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "pipeline.go", nil, 0)
	if err != nil {
		t.Fatalf("parse pipeline.go: %v", err)
	}
	var found *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "indexFile" && fn.Recv != nil {
			found = fn
			break
		}
	}
	if found == nil || found.Body == nil {
		t.Fatal("indexFile not found in pipeline.go")
	}
	openLine := fset.Position(found.Body.Lbrace).Line
	closeLine := fset.Position(found.Body.Rbrace).Line
	bodyLines := closeLine - openLine - 1
	if bodyLines >= 60 {
		t.Fatalf("indexFile body is %d lines (want < 60)", bodyLines)
	}
}
