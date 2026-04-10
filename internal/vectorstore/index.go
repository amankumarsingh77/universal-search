package vectorstore

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/TFMV/hnsw"
)

// SearchResult represents a single search result with its string ID and distance score.
type SearchResult struct {
	ID       string
	Distance float32
}

// Index wraps an HNSW graph to provide string-ID-based vector operations.
type Index struct {
	mu      sync.RWMutex
	graph   *hnsw.Graph[int]
	idToKey map[string]int
	keyToID map[int]string
	nextKey int
	logger  *slog.Logger
}

// NewIndex creates a new HNSW vector index with default parameters:
// M=16, Ml=0.25, EfSearch=200, CosineDistance.
func NewIndex(logger *slog.Logger) *Index {
	log := logger.WithGroup("vectorstore")
	g, err := hnsw.NewGraphWithConfig[int](16, 0.25, 200, hnsw.CosineDistance)
	if err != nil {
		panic(fmt.Sprintf("failed to create HNSW graph: %v", err))
	}
	log.Info("created new vector index")
	return &Index{
		graph:   g,
		idToKey: make(map[string]int),
		keyToID: make(map[int]string),
		nextKey: 0,
		logger:  log,
	}
}

// Add inserts a vector with the given string ID into the index.
func (idx *Index) Add(id string, vec []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.graph == nil {
		return fmt.Errorf("vector index not initialized")
	}
	if len(vec) == 0 {
		return fmt.Errorf("empty vector")
	}

	key := idx.nextKey
	idx.nextKey++

	idx.idToKey[id] = key
	idx.keyToID[key] = id

	node := hnsw.MakeNode(key, vec)
	return idx.graph.Add(node)
}

// BatchAdd inserts multiple vectors into the index.
func (idx *Index) BatchAdd(ids []string, vecs [][]float32) error {
	if len(ids) != len(vecs) {
		return fmt.Errorf("ids and vecs must have the same length")
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	nodes := make([]hnsw.Node[int], len(ids))
	for i, id := range ids {
		key := idx.nextKey
		idx.nextKey++
		idx.idToKey[id] = key
		idx.keyToID[key] = id
		nodes[i] = hnsw.MakeNode(key, vecs[i])
	}

	return idx.graph.BatchAdd(nodes)
}

// Search finds the k nearest neighbors to the query vector.
// Results are ordered by distance (closest first).
func (idx *Index) Search(query []float32, k int) ([]SearchResult, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	nodes, err := idx.graph.Search(query, k)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, len(nodes))
	for i, node := range nodes {
		id, ok := idx.keyToID[node.Key]
		if !ok {
			id = fmt.Sprintf("unknown-%d", node.Key)
		}
		results[i] = SearchResult{
			ID:       id,
			Distance: hnsw.CosineDistance(query, node.Value),
		}
	}
	return results, nil
}

// Delete removes a vector by its string ID. Returns true if the ID existed.
func (idx *Index) Delete(id string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	key, ok := idx.idToKey[id]
	if !ok {
		return false
	}

	deleted := idx.graph.Delete(key)
	if deleted {
		delete(idx.idToKey, id)
		delete(idx.keyToID, key)
	}
	return deleted
}

// Has reports whether a vector with the given ID exists in the in-memory index.
func (idx *Index) Has(id string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	_, ok := idx.idToKey[id]
	return ok
}

// Save persists the index to disk atomically. Writes to .graph.tmp and .map.tmp
// then renames both to their final paths to avoid partial writes.
func (idx *Index) Save(path string) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	idx.logger.Info("saving index to disk", "path", path, "vectors", len(idx.idToKey))

	// Write graph to temp file, then rename atomically.
	tmpGraph := path + ".graph.tmp"
	sg := &hnsw.SavedGraph[int]{
		Graph: idx.graph,
		Path:  tmpGraph,
	}
	if err := sg.Save(); err != nil {
		return fmt.Errorf("saving graph: %w", err)
	}

	// Write map to temp file.
	tmpMap := path + ".map.tmp"
	f, err := os.Create(tmpMap)
	if err != nil {
		return fmt.Errorf("creating map file: %w", err)
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "%d\n", idx.nextKey)
	for id, key := range idx.idToKey {
		fmt.Fprintf(w, "%d\t%s\n", key, id)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("flushing map file: %w", err)
	}
	f.Close()

	// Atomic renames.
	if err := os.Rename(tmpGraph, path+".graph"); err != nil {
		return fmt.Errorf("renaming graph: %w", err)
	}
	if err := os.Rename(tmpMap, path+".map"); err != nil {
		return fmt.Errorf("renaming map: %w", err)
	}
	return nil
}

// LoadIndex loads a previously saved index from disk.
func LoadIndex(path string, logger *slog.Logger) (*Index, error) {
	log := logger.WithGroup("vectorstore")
	log.Info("loading index from disk", "path", path)

	// Load graph
	sg, err := hnsw.LoadSavedGraph[int](path + ".graph")
	if err != nil {
		return nil, fmt.Errorf("loading graph: %w", err)
	}

	// Load ID mappings
	f, err := os.Open(path + ".map")
	if err != nil {
		return nil, fmt.Errorf("opening map file: %w", err)
	}
	defer f.Close()

	idToKey := make(map[string]int)
	keyToID := make(map[int]string)
	var nextKey int

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		nextKey, err = strconv.Atoi(strings.TrimSpace(scanner.Text()))
		if err != nil {
			return nil, fmt.Errorf("parsing nextKey: %w", err)
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		key, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		id := parts[1]
		idToKey[id] = key
		keyToID[key] = id
	}

	log.Info("index loaded", "vectors", len(idToKey))
	return &Index{
		graph:   sg.Graph,
		idToKey: idToKey,
		keyToID: keyToID,
		nextKey: nextKey,
		logger:  log,
	}, nil
}
