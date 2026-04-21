package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"universal-search/internal/apperr"
	"universal-search/internal/config"
	"universal-search/internal/embedder"
	"universal-search/internal/query"
	"universal-search/internal/search"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

// failingEmbedder is an Embedder that always fails with a non-retriable error.
type failingEmbedder struct{ err error }

func (f *failingEmbedder) ModelID() string  { return "test-model" }
func (f *failingEmbedder) Dimensions() int  { return 768 }
func (f *failingEmbedder) EmbedBatch(_ context.Context, inputs []embedder.ChunkInput) ([][]float32, error) {
	return nil, f.err
}
func (f *failingEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return nil, f.err
}

// REF-063: Search-time embed failure surfaces as apperr.ErrEmbedFailed code.
func TestApp_EmbeddingFailureSurfacesAsErrEmbedFailed(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	a := &App{
		cfg:      config.DefaultConfig(),
		store:    s,
		logger:   slog.Default(),
		embedder: &failingEmbedder{err: errors.New("gemini exploded")},
		ctx:      context.Background(),
	}

	_, gotErr := a.Search("hello")
	if gotErr == nil {
		t.Fatalf("expected error, got nil")
	}
	var aerr *apperr.Error
	if !errors.As(gotErr, &aerr) {
		t.Fatalf("expected apperr.Error, got %T: %v", gotErr, gotErr)
	}
	if aerr.Code != apperr.ErrEmbedFailed.Code {
		t.Errorf("expected code %q, got %q", apperr.ErrEmbedFailed.Code, aerr.Code)
	}
}

// REF-063: AddFolder with an invalid path surfaces as apperr.ErrFolderDenied.
func TestApp_FolderAddError_SurfacesErrFolderDenied(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	a := &App{
		cfg:    config.DefaultConfig(),
		store:  s,
		logger: slog.Default(),
	}

	// Empty path - the store layer rejects it; AddFolder should wrap that.
	gotErr := a.AddFolder("")
	if gotErr == nil {
		t.Fatalf("expected error for empty path, got nil")
	}
	var aerr *apperr.Error
	if !errors.As(gotErr, &aerr) {
		t.Fatalf("expected apperr.Error, got %T: %v", gotErr, gotErr)
	}
	if aerr.Code != apperr.ErrFolderDenied.Code {
		t.Errorf("expected code %q, got %q", apperr.ErrFolderDenied.Code, aerr.Code)
	}
}

// REF-063: AddIgnoredFolder with empty pattern returns typed error.
func TestApp_AddIgnoredFolder_Empty_SurfacesErrConfigInvalid(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := &App{cfg: config.DefaultConfig(), store: s, logger: slog.Default()}

	gotErr := a.AddIgnoredFolder("")
	if gotErr == nil {
		t.Fatalf("expected error, got nil")
	}
	var aerr *apperr.Error
	if !errors.As(gotErr, &aerr) {
		t.Fatalf("expected apperr.Error, got %T: %v", gotErr, gotErr)
	}
	if aerr.Code != apperr.ErrConfigInvalid.Code {
		t.Errorf("expected code %q, got %q", apperr.ErrConfigInvalid.Code, aerr.Code)
	}
}

// TestApp_SearchWithFilters_ModelMismatch_SurfacesErrorCode (REF-062):
// When the engine returns ErrModelMismatch, SearchWithFilters returns nil
// Go error but populates SearchWithFiltersResult.ErrorCode =
// "ERR_MODEL_MISMATCH" so the frontend can render the reindex banner.
func TestApp_SearchWithFilters_ModelMismatch_SurfacesErrorCode(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	s, err := store.NewStore(dbPath, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// Seed: one file + chunk indexed under model "fake-a".
	fileID, err := s.UpsertFile(store.FileRecord{
		Path: "/tmp/a.txt", FileType: "text", Extension: ".txt", SizeBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "v1", ChunkIndex: 0,
		EmbeddingModel: "fake-a", EmbeddingDims: 4,
	}); err != nil {
		t.Fatal(err)
	}

	idx := vectorstore.NewDefaultIndex(slog.Default())
	queryVec := []float32{1, 0, 0, 0}
	if err := idx.Add("v1", queryVec); err != nil {
		t.Fatal(err)
	}

	cfg := search.DefaultEngineConfig()
	planner := search.NewPlannerWithLogger(s, idx, cfg.Planner, slog.Default())
	// Engine configured for "fake-b" so all fake-a chunks get filtered out.
	engine := search.NewWithModel(s, idx, slog.Default(), planner, cfg, "fake-b")

	a := &App{
		cfg:              config.DefaultConfig(),
		store:            s,
		logger:           slog.Default(),
		embedder:         embedder.NewFake("fake-b", 4),
		engine:           engine,
		index:            idx,
		parsedQueryCache: query.NewParsedQueryCache(s),
	}
	a.ctx = context.Background()

	res, gotErr := a.SearchWithFilters("hello", "", nil)
	if gotErr != nil {
		t.Fatalf("expected nil Go error on mismatch, got %v", gotErr)
	}
	if res.ErrorCode != apperr.ErrModelMismatch.Code {
		t.Errorf("ErrorCode = %q, want %q", res.ErrorCode, apperr.ErrModelMismatch.Code)
	}
	if len(res.Results) != 0 {
		t.Errorf("expected empty Results, got %d", len(res.Results))
	}
}
