package search

import (
	"testing"

	"universal-search/internal/query"
	"universal-search/internal/vectorstore"

	"universal-search/internal/store"
)

// TestSearchWithSpecResult_ReturnsStruct verifies the new SearchWithSpec signature
// returns a SearchWithSpecResult struct.
func TestSearchWithSpecResult_ReturnsStruct(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	fileID, err := db.UpsertFile(store.FileRecord{
		Path: "/tmp/phase5-test.txt", FileType: "text", Extension: ".txt", SizeBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: "vec-p5", ChunkIndex: 0})
	if err != nil {
		t.Fatal(err)
	}
	vec := make([]float32, 768)
	vec[0] = 1.0
	if err := idx.Add("vec-p5", vec); err != nil {
		t.Fatal(err)
	}

	planner := NewPlanner(db, idx, DefaultPlannerConfig())
	engine := NewWithPlanner(db, idx, testLogger, planner)

	queryVec := make([]float32, 768)
	queryVec[0] = 1.0

	res, err := engine.SearchWithSpec(queryVec, query.FilterSpec{}, "phase5", 5)
	if err != nil {
		t.Fatal(err)
	}
	if res.Strategy != "semantic" {
		t.Errorf("expected strategy=semantic, got %q", res.Strategy)
	}
	if len(res.Results) == 0 {
		t.Error("expected results, got none")
	}
}

// TestSearchWithSpecResult_FilenameMatchAtTop verifies that a filename match
// for the rawQuery is prepended to semantic results.
func TestSearchWithSpecResult_FilenameMatchAtTop(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	// Insert a semantic file
	semFileID, err := db.UpsertFile(store.FileRecord{
		Path: "/tmp/other.txt", FileType: "text", Extension: ".txt", SizeBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{FileID: semFileID, VectorID: "vec-sem", ChunkIndex: 0})
	if err != nil {
		t.Fatal(err)
	}
	semVec := make([]float32, 768)
	semVec[0] = 1.0
	if err := idx.Add("vec-sem", semVec); err != nil {
		t.Fatal(err)
	}

	// Insert a filename-matching file (but no vector)
	_, err = db.UpsertFile(store.FileRecord{
		Path: "/tmp/quarterly-report.pdf", FileType: "document", Extension: ".pdf", SizeBytes: 500,
	})
	if err != nil {
		t.Fatal(err)
	}

	planner := NewPlanner(db, idx, DefaultPlannerConfig())
	engine := NewWithPlanner(db, idx, testLogger, planner)

	queryVec := make([]float32, 768)
	queryVec[0] = 1.0

	res, err := engine.SearchWithSpec(queryVec, query.FilterSpec{}, "quarterly-report", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Find whether quarterly-report.pdf appears in the results
	found := false
	for _, r := range res.Results {
		if r.File.Path == "/tmp/quarterly-report.pdf" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected /tmp/quarterly-report.pdf in results via filename match")
	}

	// quarterly-report.pdf should appear first (lexical match)
	if len(res.Results) > 0 && res.Results[0].File.Path != "/tmp/quarterly-report.pdf" {
		t.Errorf("expected filename match at top, got %s", res.Results[0].File.Path)
	}
}

// TestSearchWithSpecResult_RelaxationBannerSet verifies that when filters are
// dropped, RelaxationBanner is non-empty.
func TestSearchWithSpecResult_RelaxationBannerSet(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	// Insert a file that will be found after relaxation (no date filter match)
	fileID, err := db.UpsertFile(store.FileRecord{
		Path: "/tmp/relaxed.txt", FileType: "text", Extension: ".txt", SizeBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: "vec-relax", ChunkIndex: 0})
	if err != nil {
		t.Fatal(err)
	}
	v := make([]float32, 768)
	v[0] = 1.0
	if err := idx.Add("vec-relax", v); err != nil {
		t.Fatal(err)
	}

	planner := NewPlanner(db, idx, DefaultPlannerConfig())
	engine := NewWithPlanner(db, idx, testLogger, planner)

	queryVec := make([]float32, 768)
	queryVec[0] = 1.0

	// Spec with an impossible modified_at filter (far future → no matches before relaxation)
	spec := query.FilterSpec{
		Must: []query.Clause{
			{Field: query.FieldModifiedAt, Op: query.OpGte, Value: "2099-01-01T00:00:00Z"},
		},
	}

	res, err := engine.SearchWithSpec(queryVec, spec, "relaxed", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.RelaxationBanner == "" {
		t.Error("expected non-empty RelaxationBanner when filter was dropped")
	}
}
