package search

import (
	"testing"

	"universal-search/internal/query"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

// --- helpers ---

func makeVec(val float32, dim int) []float32 {
	v := make([]float32, dim)
	v[0] = val
	return v
}

func seedFileWithVec(t *testing.T, db *store.Store, idx *vectorstore.Index, path, fileType, vectorID string, vec []float32) int64 {
	t.Helper()
	fileID, err := db.UpsertFile(store.FileRecord{
		Path: path, FileType: fileType, Extension: ".txt", SizeBytes: 100,
	})
	if err != nil {
		t.Fatalf("UpsertFile %s: %v", path, err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: vectorID, ChunkIndex: 0})
	if err != nil {
		t.Fatalf("InsertChunk %s: %v", vectorID, err)
	}
	if err := idx.Add(vectorID, vec); err != nil {
		t.Fatalf("idx.Add %s: %v", vectorID, err)
	}
	return fileID
}

// --- TestPlanner_UnfilteredUsesHNSW ---

// TestPlanner_UnfilteredUsesHNSW verifies that an empty FilterSpec (no Must/MustNot)
// routes through the "semantic" path (calls index.Search directly).
func TestPlanner_UnfilteredUsesHNSW(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	vec := makeVec(1.0, 768)
	seedFileWithVec(t, db, idx, "/tmp/a.txt", "text", "vec-a", vec)

	planner := NewPlanner(db, idx, DefaultPlannerConfig())
	queryVec := makeVec(1.0, 768)
	results, strategy, plannerCount, err := planner.Plan(queryVec, query.FilterSpec{}, 5)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if strategy != "semantic" {
		t.Errorf("expected strategy=semantic, got %q", strategy)
	}
	if plannerCount != 0 {
		t.Errorf("expected plannerCount=0 for semantic path, got %d", plannerCount)
	}
	if len(results) == 0 {
		t.Errorf("expected at least 1 result, got 0")
	}
}

// TestPlanner_BruteForceWhenCountBelowThreshold verifies that when the filtered
// file count is below the threshold, brute-force cosine search is used.
func TestPlanner_BruteForceWhenCountBelowThreshold(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	// Seed 3 image files
	for i, name := range []string{"img1.png", "img2.png", "img3.png"} {
		vec := makeVec(float32(i+1)*0.1, 768)
		seedFileWithVec(t, db, idx, "/tmp/"+name, "image", "vec-img-"+name, vec)
	}
	// Seed 1 text file (should not match image filter)
	seedFileWithVec(t, db, idx, "/tmp/doc.txt", "text", "vec-doc", makeVec(0.9, 768))

	spec := query.FilterSpec{
		Must: []query.Clause{{Field: query.FieldFileType, Op: query.OpEq, Value: "image"}},
	}

	planner := NewPlanner(db, idx, PlannerConfig{BruteForceThreshold: 5000})
	queryVec := makeVec(1.0, 768)
	results, strategy, plannerCount, err := planner.Plan(queryVec, spec, 10)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if strategy != "brute_force" {
		t.Errorf("expected strategy=brute_force, got %q", strategy)
	}
	if plannerCount != 3 {
		t.Errorf("expected plannerCount=3 (image files), got %d", plannerCount)
	}
	// All results should be image files
	for _, r := range results {
		if r.File.FileType != "image" {
			t.Errorf("expected image file, got %q", r.File.FileType)
		}
	}
	_ = results
}

// TestPlanner_HNSWPostFilterWhenCountAboveThreshold verifies that when count >= threshold,
// the hnsw_post_filter path is chosen.
func TestPlanner_HNSWPostFilterWhenCountAboveThreshold(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	// Seed 2 image files
	for i, name := range []string{"img1.png", "img2.png"} {
		vec := makeVec(float32(i+1)*0.1, 768)
		seedFileWithVec(t, db, idx, "/tmp/"+name, "image", "vec-img-"+name, vec)
	}

	spec := query.FilterSpec{
		Must: []query.Clause{{Field: query.FieldFileType, Op: query.OpEq, Value: "image"}},
	}

	// threshold=1 so count(2) >= threshold → hnsw_post_filter
	planner := NewPlanner(db, idx, PlannerConfig{BruteForceThreshold: 1})
	queryVec := makeVec(1.0, 768)
	_, strategy, plannerCount, err := planner.Plan(queryVec, spec, 5)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if strategy != "hnsw_post_filter" {
		t.Errorf("expected strategy=hnsw_post_filter, got %q", strategy)
	}
	if plannerCount != 2 {
		t.Errorf("expected plannerCount=2, got %d", plannerCount)
	}
}

// TestPlanner_ReturnsEmptyWhenCountZero verifies that when the filter matches no files,
// empty results are returned.
func TestPlanner_ReturnsEmptyWhenCountZero(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	seedFileWithVec(t, db, idx, "/tmp/a.txt", "text", "vec-a", makeVec(1.0, 768))

	spec := query.FilterSpec{
		Must: []query.Clause{{Field: query.FieldFileType, Op: query.OpEq, Value: "video"}},
	}

	planner := NewPlanner(db, idx, PlannerConfig{BruteForceThreshold: 5000})
	queryVec := makeVec(1.0, 768)
	results, strategy, plannerCount, err := planner.Plan(queryVec, spec, 5)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if strategy != "brute_force" {
		t.Errorf("expected strategy=brute_force for zero-count, got %q", strategy)
	}
	if plannerCount != 0 {
		t.Errorf("expected plannerCount=0, got %d", plannerCount)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestBruteForceCosine_ExactRecall verifies that a query exactly matching vec1
// ranks vec1 first.
func TestBruteForceCosine_ExactRecall(t *testing.T) {
	// 3 unit vectors along different dimensions
	v1 := make([]float32, 768)
	v1[0] = 1.0
	v2 := make([]float32, 768)
	v2[1] = 1.0
	v3 := make([]float32, 768)
	v3[2] = 1.0

	blobs := map[int64][][]float32{
		1: {v1},
		2: {v2},
		3: {v3},
	}

	// Query matches v1 exactly
	queryVec := make([]float32, 768)
	queryVec[0] = 1.0

	results := bruteForceCosine(queryVec, blobs, 3)
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].File.ID != 1 {
		t.Errorf("expected file ID 1 (exact match) ranked first, got %d", results[0].File.ID)
	}
	// Distance should be ~0 for exact match
	if results[0].Distance > 0.01 {
		t.Errorf("expected near-zero distance for exact match, got %f", results[0].Distance)
	}
}

// TestEfCalculation verifies the ef clamp formula: k=20, total=100000, count=5000 → ef=500
func TestEfCalculation(t *testing.T) {
	k := 20
	total := 100000
	count := 5000
	ef := clampEf(k, total, count, 10)
	expected := 500 // min(max(20*10*100000/5000, 64), 500) = min(4000, 500) = 500
	if ef != expected {
		t.Errorf("expected ef=%d, got %d", expected, ef)
	}
}

// TestSearchWithSpec_FiltersResults verifies kind:image filter returns only image results.
func TestSearchWithSpec_FiltersResults(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewDefaultIndex(testLogger)

	// Seed one image and one text file with similar vectors
	imgVec := makeVec(1.0, 768)
	txtVec := makeVec(0.99, 768)
	seedFileWithVec(t, db, idx, "/tmp/photo.png", "image", "vec-img", imgVec)
	seedFileWithVec(t, db, idx, "/tmp/readme.txt", "text", "vec-txt", txtVec)

	// Use threshold=0 to force HNSW post-filter path, which joins full metadata.
	planner := NewPlanner(db, idx, PlannerConfig{})
	engine := NewWithPlanner(db, idx, testLogger, planner)

	spec := query.FilterSpec{
		Must: []query.Clause{{Field: query.FieldFileType, Op: query.OpEq, Value: "image"}},
	}
	queryVec := makeVec(1.0, 768)
	res, err := engine.SearchWithSpec(queryVec, spec, "image", 10)
	if err != nil {
		t.Fatalf("SearchWithSpec error: %v", err)
	}
	if len(res.Results) == 0 {
		t.Fatal("expected results, got none")
	}
	for _, r := range res.Results {
		if r.File.FileType != "image" {
			t.Errorf("expected only image results, got %q (%s)", r.File.FileType, r.File.Path)
		}
	}
}
