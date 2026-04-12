package search

import (
	"log/slog"
	"math"
	"sort"

	"universal-search/internal/query"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

// DefaultBruteForceThreshold is the file count below which brute-force cosine
// search is preferred over HNSW post-filtering.
const DefaultBruteForceThreshold = 5000

// PlannerStore is the subset of store.Store methods required by the Planner.
type PlannerStore interface {
	CountFiltered(spec store.FilterSpec) (int, error)
	FilterFileIDs(spec store.FilterSpec) ([]int64, error)
	GetVectorBlobs(fileIDs []int64) (map[int64][][]float32, error)
	GetChunksByVectorIDs(vectorIDs []string) ([]store.SearchResult, error)
	GetFilesByIDs(ids []int64) ([]store.FileRecord, error)
	CountFiles() (int, error)
}

// Planner routes search requests between brute-force cosine search and
// HNSW post-filtering based on the size of the filtered candidate set.
type Planner struct {
	store     PlannerStore
	index     *vectorstore.Index
	threshold int
	logger    *slog.Logger
}

// NewPlanner creates a new Planner. threshold is the file count below which
// brute-force cosine search is used instead of HNSW post-filtering.
func NewPlanner(s PlannerStore, idx *vectorstore.Index, threshold int) *Planner {
	return &Planner{store: s, index: idx, threshold: threshold, logger: slog.Default()}
}

// NewPlannerWithLogger creates a Planner with a custom logger.
func NewPlannerWithLogger(s PlannerStore, idx *vectorstore.Index, threshold int, logger *slog.Logger) *Planner {
	return &Planner{store: s, index: idx, threshold: threshold, logger: logger}
}

// Plan routes between brute-force and HNSW post-filter based on cardinality.
// Returns (results, chosenStrategy, plannerCount, error).
// chosenStrategy: "semantic" | "brute_force" | "hnsw_post_filter"
func (p *Planner) Plan(queryVec []float32, spec query.FilterSpec, k int) ([]store.SearchResult, string, int, error) {
	// Convert to store spec early so we can inspect what SQL filters are actually present.
	// convertToStoreSpec strips semantic_contains clauses (not SQL-computable).
	storeSpec := convertToStoreSpec(spec)

	// No SQL-computable filters → pure semantic HNSW search.
	if len(storeSpec.Must) == 0 && len(storeSpec.MustNot) == 0 {
		p.logger.Debug("planner: no SQL filters, using pure semantic HNSW search", "k", k)
		results, err := p.index.Search(queryVec, k*5)
		if err != nil {
			return nil, "semantic", 0, err
		}
		// Join with metadata via store.
		storeResults, err := p.joinHNSWResults(results, k)
		if err != nil {
			return nil, "semantic", 0, err
		}
		p.logger.Debug("planner: semantic search complete", "candidates", len(storeResults))
		return storeResults, "semantic", 0, nil
	}

	p.logger.Debug("planner: counting filtered files",
		"must_clauses", len(spec.Must),
		"must_not_clauses", len(spec.MustNot),
	)
	count, err := p.store.CountFiltered(storeSpec)
	if err != nil {
		return nil, "brute_force", 0, err
	}
	p.logger.Debug("planner: filter count result", "count", count, "threshold", p.threshold)
	if count == 0 {
		p.logger.Debug("planner: zero matching files for filters, returning empty")
		return nil, "brute_force", 0, nil
	}

	if count < p.threshold {
		p.logger.Debug("planner: using brute-force cosine path", "file_count", count)
		// Brute-force path: fetch vectors and compute cosine similarity directly.
		fileIDs, err := p.store.FilterFileIDs(storeSpec)
		if err != nil {
			return nil, "brute_force", count, err
		}
		blobs, err := p.store.GetVectorBlobs(fileIDs)
		if err != nil {
			return nil, "brute_force", count, err
		}
		results := bruteForceCosine(queryVec, blobs, k)
		// Hydrate results with full file metadata (bruteForceCosine only sets File.ID).
		rankedIDs := make([]int64, len(results))
		for i, r := range results {
			rankedIDs[i] = r.File.ID
		}
		fileRecords, err := p.store.GetFilesByIDs(rankedIDs)
		if err != nil {
			return nil, "brute_force", count, err
		}
		fileMap := make(map[int64]store.FileRecord, len(fileRecords))
		for _, f := range fileRecords {
			fileMap[f.ID] = f
		}
		for i, r := range results {
			if f, ok := fileMap[r.File.ID]; ok {
				results[i].File = f
			}
		}
		p.logger.Debug("planner: brute-force complete", "results", len(results))
		return results, "brute_force", count, nil
	}

	// HNSW post-filter path: over-fetch from HNSW then filter by allowed file IDs.
	totalFiles, err := p.store.CountFiles()
	if err != nil {
		return nil, "hnsw_post_filter", count, err
	}
	ef := clampEf(k, totalFiles, count)
	p.logger.Debug("planner: using HNSW post-filter path",
		"file_count", count,
		"total_files", totalFiles,
		"ef", ef,
	)

	hnswResults, err := p.index.Search(queryVec, ef)
	if err != nil {
		return nil, "hnsw_post_filter", count, err
	}

	// Build allowed file ID set.
	fileIDs, err := p.store.FilterFileIDs(storeSpec)
	if err != nil {
		return nil, "hnsw_post_filter", count, err
	}
	allowedIDs := make(map[int64]bool, len(fileIDs))
	for _, id := range fileIDs {
		allowedIDs[id] = true
	}

	// Extract vector IDs from HNSW results and fetch metadata.
	vectorIDs := make([]string, len(hnswResults))
	for i, r := range hnswResults {
		vectorIDs[i] = r.ID
	}
	chunks, err := p.store.GetChunksByVectorIDs(vectorIDs)
	if err != nil {
		return nil, "hnsw_post_filter", count, err
	}

	// Build distance map.
	distMap := make(map[string]float32, len(hnswResults))
	for _, r := range hnswResults {
		distMap[r.ID] = r.Distance
	}
	for i := range chunks {
		chunks[i].Distance = distMap[chunks[i].VectorID]
	}

	// Filter by allowed file IDs and deduplicate by file path (best chunk per file).
	best := make(map[int64]store.SearchResult)
	for _, r := range chunks {
		if !allowedIDs[r.File.ID] {
			continue
		}
		existing, seen := best[r.File.ID]
		if !seen || r.Distance < existing.Distance {
			best[r.File.ID] = r
		}
	}

	filtered := make([]store.SearchResult, 0, len(best))
	for _, r := range best {
		filtered = append(filtered, r)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Distance < filtered[j].Distance
	})
	if len(filtered) > k {
		filtered = filtered[:k]
	}
	p.logger.Debug("planner: HNSW post-filter complete",
		"hnsw_candidates", len(hnswResults),
		"after_filter", len(filtered),
	)
	return filtered, "hnsw_post_filter", count, nil
}

// joinHNSWResults converts vectorstore.SearchResult slice to store.SearchResult slice
// by fetching metadata, deduplicating by file path, and capping at k.
func (p *Planner) joinHNSWResults(hnswResults []vectorstore.SearchResult, k int) ([]store.SearchResult, error) {
	if len(hnswResults) == 0 {
		return nil, nil
	}
	vectorIDs := make([]string, len(hnswResults))
	for i, r := range hnswResults {
		vectorIDs[i] = r.ID
	}
	chunks, err := p.store.GetChunksByVectorIDs(vectorIDs)
	if err != nil {
		return nil, err
	}
	distMap := make(map[string]float32, len(hnswResults))
	for _, r := range hnswResults {
		distMap[r.ID] = r.Distance
	}
	for i := range chunks {
		chunks[i].Distance = distMap[chunks[i].VectorID]
	}

	// Build lookup by vectorID.
	byVecID := make(map[string]store.SearchResult, len(chunks))
	for _, r := range chunks {
		byVecID[r.VectorID] = r
	}

	// Deduplicate by file path, keeping best distance.
	best := make(map[string]store.SearchResult)
	for _, id := range vectorIDs {
		r, ok := byVecID[id]
		if !ok {
			continue
		}
		existing, seen := best[r.File.Path]
		if !seen || r.Distance < existing.Distance {
			best[r.File.Path] = r
		}
	}

	// Re-traverse HNSW order to emit in rank order, capped at k.
	emitted := make(map[string]bool)
	var deduped []store.SearchResult
	for _, id := range vectorIDs {
		if len(deduped) >= k {
			break
		}
		r, ok := byVecID[id]
		if !ok {
			continue
		}
		if emitted[r.File.Path] {
			continue
		}
		if kept := best[r.File.Path]; kept.VectorID == r.VectorID {
			emitted[r.File.Path] = true
			deduped = append(deduped, r)
		}
	}
	return deduped, nil
}

// bruteForceCosine computes cosine similarity between queryVec and all chunk
// vectors in blobs, returning the top-k results ordered by ascending distance.
// Each file contributes at most one result (best chunk).
func bruteForceCosine(queryVec []float32, blobs map[int64][][]float32, k int) []store.SearchResult {
	type candidate struct {
		fileID   int64
		distance float32
	}
	candidates := make([]candidate, 0, len(blobs))
	for fileID, vecs := range blobs {
		bestDist := float32(math.MaxFloat32)
		for _, vec := range vecs {
			d := cosineDist(queryVec, vec)
			if d < bestDist {
				bestDist = d
			}
		}
		candidates = append(candidates, candidate{fileID: fileID, distance: bestDist})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	results := make([]store.SearchResult, len(candidates))
	for i, c := range candidates {
		results[i] = store.SearchResult{
			File:     store.FileRecord{ID: c.fileID},
			Distance: c.distance,
		}
	}
	return results
}

// cosineDist returns the cosine distance between a and b: 1 - cosineSimilarity(a, b).
// Returns 1.0 (maximum distance) if either vector is empty, has zero magnitude,
// or the two vectors have different dimensions (incompatible embedding models).
func cosineDist(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 1.0 // incompatible or degenerate vectors
	}
	var dot, magA, magB float64
	n := len(a)
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0 || magB == 0 {
		return 1.0
	}
	sim := dot / (math.Sqrt(magA) * math.Sqrt(magB))
	// clamp to [-1, 1] to guard against floating-point drift
	if sim > 1 {
		sim = 1
	} else if sim < -1 {
		sim = -1
	}
	return float32(1 - sim)
}

// clampEf computes the ef (exploration factor) for HNSW over-fetching:
// ef = clamp(k * 10 * totalFiles / count, 64, 500).
func clampEf(k, totalFiles, count int) int {
	if count == 0 {
		return 64
	}
	ef := k * 10 * totalFiles / count
	if ef < 64 {
		ef = 64
	}
	if ef > 500 {
		ef = 500
	}
	return ef
}

// convertToStoreSpec converts a query.FilterSpec to a store.FilterSpec for SQL compilation.
// Only Must and MustNot clauses are included (Should clauses are for reranking, not SQL).
func convertToStoreSpec(spec query.FilterSpec) store.FilterSpec {
	return store.FilterSpec{
		SemanticQuery: spec.SemanticQuery,
		Must:          convertClauses(spec.Must),
		MustNot:       convertClauses(spec.MustNot),
	}
}

func convertClauses(clauses []query.Clause) []store.Clause {
	if len(clauses) == 0 {
		return nil
	}
	out := make([]store.Clause, 0, len(clauses))
	for _, c := range clauses {
		// Skip fields that don't exist in the SQL schema (e.g. semantic_contains).
		if c.Field == query.FieldSemanticContains {
			continue
		}
		out = append(out, store.Clause{
			Field: store.FieldEnum(c.Field),
			Op:    store.Op(c.Op),
			Value: c.Value,
			Boost: c.Boost,
		})
	}
	return out
}
