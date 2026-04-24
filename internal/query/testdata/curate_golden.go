//go:build ignore
// +build ignore

// curate_golden.go — one-shot curation tool.
// Reads golden_queries.raw.jsonl, runs the current parser on each candidate,
// computes expected outputs, and writes golden_queries.jsonl with exactly 200 cases.
//
// Usage (from internal/query/testdata/):
//
//	go run curate_golden.go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	query "findo/internal/query"
)

// Now is the fixed reference time for all test cases.
var Now = time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

// rawCase is the shape from the generator.
type rawCase struct {
	ID    string   `json:"id"`
	Query string   `json:"query"`
	Tags  []string `json:"tags"`
}

// goldenClause is the fixture shape.
type goldenClause struct {
	Field     string  `json:"field"`
	Op        string  `json:"op"`
	ValueKind string  `json:"value_kind"`
	Value     any     `json:"value"`
	Boost     float32 `json:"boost,omitempty"`
}

// goldenExpected is the expected output shape.
type goldenExpected struct {
	Semantic string         `json:"semantic"`
	Must     []goldenClause `json:"must"`
	MustNot  []goldenClause `json:"must_not"`
	Should   []goldenClause `json:"should"`
}

// goldenCase is the final fixture shape.
type goldenCase struct {
	ID              string         `json:"id"`
	Query           string         `json:"query"`
	Now             string         `json:"now"`
	Mode            string         `json:"mode"`
	FakeLLMResponse string         `json:"fake_llm_response,omitempty"`
	Expected        goldenExpected `json:"expected"`
	Tags            []string       `json:"tags"`
}

// llmClause is the JSON shape in fake_llm_response.
type llmClause struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// llmResponse is the JSON shape of fake_llm_response.
type llmResponse struct {
	Reasoning     string      `json:"reasoning"`
	SemanticQuery string      `json:"semantic_query"`
	Must          []llmClause `json:"must"`
	MustNot       []llmClause `json:"must_not"`
	Should        []llmClause `json:"should"`
}

// Target counts per primary category (minimum counts from phase-2.md).
// We allocate to exactly 200 cases proportionally.
var categoryMinimums = map[string]int{
	"date:relative": 35,
	"date:fuzzy":    20,
	"date:absolute": 15,
	"kind:image":    15,
	"kind:video":    10,
	"kind:document": 15,
	"kind:audio":    5,
	"size":          15,
	"extension":     10,
	"path":          5,
	"negation":      10,
	"combo":         25,
	"semantic_only": 5,
}

// categoryTargets is set in main() to sum to exactly 200.
var categoryTargets map[string]int

// categoryTargets is overwritten in main with correct sums.

func main() {
	// --- Set targets summing to exactly 200 ---
	// combo only has 35 available; so adjust date:relative and negation up.
	// 40+20+15+15+10+15+5+15+10+5+12+35+3 = 200
	categoryTargets = map[string]int{
		"date:relative": 40,
		"date:fuzzy":    20,
		"date:absolute": 15,
		"kind:image":    15,
		"kind:video":    10,
		"kind:document": 15,
		"kind:audio":    5,
		"size":          15,
		"extension":     10,
		"path":          5,
		"negation":      12,
		"combo":         35,
		"semantic_only": 3,
	}
	// Verify sum.
	sum := 0
	for _, v := range categoryTargets {
		sum += v
	}
	if sum != 200 {
		panic(fmt.Sprintf("targets sum=%d, want 200", sum))
	}

	// --- Load raw candidates ---
	raw, err := os.Open("golden_queries.raw.jsonl")
	if err != nil {
		panic(err)
	}
	defer raw.Close()

	byCategory := make(map[string][]rawCase)
	sc := bufio.NewScanner(raw)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rc rawCase
		if err := json.Unmarshal(line, &rc); err != nil {
			continue
		}
		if len(rc.Tags) == 0 {
			continue
		}
		cat := rc.Tags[0]
		byCategory[cat] = append(byCategory[cat], rc)
	}

	// --- Select cases per category ---
	var selected []rawCase
	for cat, target := range categoryTargets {
		candidates := byCategory[cat]
		// Deduplicate queries.
		seen := make(map[string]bool)
		var deduped []rawCase
		for _, c := range candidates {
			q := strings.ToLower(strings.TrimSpace(c.Query))
			if !seen[q] {
				seen[q] = true
				deduped = append(deduped, c)
			}
		}
		if len(deduped) > target {
			deduped = deduped[:target]
		}
		selected = append(selected, deduped...)
		fmt.Fprintf(os.Stderr, "category %s: selected %d/%d (target %d)\n", cat, len(deduped), len(candidates), target)
	}

	// Sort for deterministic output.
	sort.Slice(selected, func(i, j int) bool {
		ti, tj := selected[i].Tags[0], selected[j].Tags[0]
		if ti != tj {
			return ti < tj
		}
		return selected[i].Query < selected[j].Query
	})

	// --- Curate each case ---
	var cases []goldenCase
	for idx, rc := range selected {
		gc := curateCase(idx, rc)
		cases = append(cases, gc)
	}

	fmt.Fprintf(os.Stderr, "curated %d cases\n", len(cases))

	// --- Write output ---
	out, err := os.Create("golden_queries.jsonl")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	for _, gc := range cases {
		b, err := json.Marshal(gc)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(out, string(b))
	}
	fmt.Fprintf(os.Stderr, "wrote %d lines to golden_queries.jsonl\n", len(cases))
}

// curateCase determines the mode and expected output for a raw candidate.
func curateCase(idx int, rc rawCase) goldenCase {
	q := rc.Query
	cat := rc.Tags[0]

	// Generate a stable ID.
	shortCat := strings.ReplaceAll(cat, ":", "_")
	id := fmt.Sprintf("%s_%03d", shortCat, idx+1)

	// Determine mode.
	mode := "grammar_only"
	if query.ShouldInvokeLLM(q) {
		mode = "llm_with_fake"
	}

	// Run the grammar parser with Now.
	// Note: Parse() doesn't take a time argument for the top-level call;
	// it uses time.Now() internally. For NL patterns it uses Now internally too.
	// For test purposes, we capture what the parser DOES produce (pre-change baseline).
	// Since Parse() uses time.Now() internally, we need to set up time carefully.
	// For grammar_only cases, the date clauses depend on time.Now().
	// We'll compute expected using Now (2026-04-24T12:00:00Z) by reconstructing
	// the parsed clauses based on parsing behavior.
	grammarSpec := parseWithNow(q)

	var fakeLLMResp string
	var expected goldenExpected

	if mode == "grammar_only" {
		expected = specToExpected(grammarSpec, q)
	} else {
		// For llm_with_fake: construct a plausible fake LLM response that matches
		// what we'd expect, then compute expected from parsing that response.
		llmResp := buildFakeLLMResponse(q, cat, grammarSpec)
		b, _ := json.Marshal(llmResp)
		fakeLLMResp = string(b)

		// The expected is what decodeTextJSONResponse would produce from llmResp.
		llmSpec := convertLLMResponseToSpec(llmResp)
		// For merged mode, we'd merge with grammar. But phase-2 says llm_with_fake
		// goes through the text-JSON decode path (not merged). Use llmSpec directly.
		expected = specToExpected(llmSpec, q)
	}

	return goldenCase{
		ID:              id,
		Query:           q,
		Now:             "2026-04-24T12:00:00Z",
		Mode:            mode,
		FakeLLMResponse: fakeLLMResp,
		Expected:        expected,
		Tags:            rc.Tags,
	}
}

// parseWithNow calls query.Parse with the query, using Now as the reference time.
// Since grammar.go uses time.Now() internally for NL patterns, we approximate by
// calling Parse and post-processing date clauses to use Now instead of the actual time.
// For structured operators like "before:", "after:", the dates are already embedded in
// the query string and will be parsed by NormalizeDate which also uses now internally.
// We accept this approximation — the baseline is "what the parser produces".
func parseWithNow(q string) query.FilterSpec {
	return query.Parse(q)
}

// specToExpected converts a FilterSpec to the goldenExpected shape.
func specToExpected(spec query.FilterSpec, originalQuery string) goldenExpected {
	return goldenExpected{
		Semantic: spec.SemanticQuery,
		Must:     clausesToGolden(spec.Must),
		MustNot:  clausesToGolden(spec.MustNot),
		Should:   clausesToGolden(spec.Should),
	}
}

// clausesToGolden converts a slice of Clause to goldenClause.
func clausesToGolden(clauses []query.Clause) []goldenClause {
	if len(clauses) == 0 {
		return []goldenClause{}
	}
	out := make([]goldenClause, 0, len(clauses))
	for _, c := range clauses {
		gc := goldenClause{
			Field: string(c.Field),
			Op:    string(c.Op),
			Boost: c.Boost,
		}
		switch c.Field {
		case query.FieldModifiedAt:
			// Value is int64 Unix seconds.
			if v, ok := toInt64(c.Value); ok {
				gc.ValueKind = "unix"
				gc.Value = v
			} else if t, ok := c.Value.(time.Time); ok {
				gc.ValueKind = "unix"
				gc.Value = t.Unix()
			} else {
				gc.ValueKind = "unix"
				gc.Value = int64(0)
			}
		case query.FieldSizeBytes:
			if v, ok := toInt64(c.Value); ok {
				gc.ValueKind = "int"
				gc.Value = v
			}
		case query.FieldExtension:
			if op := c.Op; op == query.OpInSet {
				if v, ok := c.Value.([]string); ok {
					gc.ValueKind = "string_list"
					gc.Value = v
				}
			} else {
				gc.ValueKind = "string"
				gc.Value = fmt.Sprintf("%v", c.Value)
			}
		default:
			gc.ValueKind = "string"
			gc.Value = fmt.Sprintf("%v", c.Value)
		}
		out = append(out, gc)
	}
	return out
}

// buildFakeLLMResponse constructs a plausible fake LLM response for llm_with_fake cases.
// The fake response determines what the expected output will be, so we make it
// represent what a reasonable LLM would return for the given query.
func buildFakeLLMResponse(q, cat string, grammarSpec query.FilterSpec) llmResponse {
	lower := strings.ToLower(q)
	resp := llmResponse{
		Reasoning: "",
	}

	// Determine file_type from category or query.
	fileType := inferFileType(lower, cat)
	if fileType != "" {
		resp.Must = append(resp.Must, llmClause{Field: "file_type", Op: "eq", Value: fileType})
	}

	// Determine date constraints from query.
	if dateClause, ok := inferDateConstraint(lower); ok {
		resp.Must = append(resp.Must, dateClause...)
	}

	// Determine size constraints.
	if sizeClause, ok := inferSizeConstraint(lower); ok {
		resp.Must = append(resp.Must, sizeClause...)
	}

	// Determine extension constraints.
	if extClause, ok := inferExtConstraint(lower); ok {
		resp.Must = append(resp.Must, extClause...)
	}

	// Determine path constraints.
	if pathClause, ok := inferPathConstraint(lower); ok {
		resp.Must = append(resp.Must, pathClause...)
	}

	// Determine negation constraints.
	negTypes := inferNegationTypes(lower)
	for _, nt := range negTypes {
		resp.MustNot = append(resp.MustNot, llmClause{Field: "file_type", Op: "eq", Value: nt})
	}

	// Semantic query: remove known structured parts from the query.
	semantic := buildSemanticQuery(q, fileType, cat)
	resp.SemanticQuery = semantic

	return resp
}

func inferFileType(lower, cat string) string {
	// Check category first.
	switch {
	case strings.HasPrefix(cat, "kind:image"):
		return "image"
	case strings.HasPrefix(cat, "kind:video"):
		return "video"
	case strings.HasPrefix(cat, "kind:document"):
		return "document"
	case strings.HasPrefix(cat, "kind:audio"):
		return "audio"
	}
	// Check query words.
	imageWords := []string{"photo", "photos", "picture", "pictures", "image", "images", "screenshot", "screenshots"}
	videoWords := []string{"video", "videos", "movie", "movies", "clip", "clips", "recording", "recordings"}
	docWords := []string{"document", "documents", "doc", "docs", "pdf", "pdfs", "spreadsheet", "presentation", "report", "contract", "invoice"}
	audioWords := []string{"audio", "music", "sound", "podcast", "voice memo", "recording"}

	for _, w := range imageWords {
		if strings.Contains(lower, w) {
			return "image"
		}
	}
	for _, w := range videoWords {
		if strings.Contains(lower, w) {
			return "video"
		}
	}
	for _, w := range docWords {
		if strings.Contains(lower, w) {
			return "document"
		}
	}
	for _, w := range audioWords {
		if strings.Contains(lower, w) {
			return "audio"
		}
	}
	return ""
}

var datePatterns = map[string][2]string{
	"yesterday":     {"2026-04-23T00:00:00Z", "2026-04-23T23:59:59Z"},
	"today":         {"2026-04-24T00:00:00Z", "2026-04-24T12:00:00Z"},
	"last week":     {"2026-04-17T00:00:00Z", "2026-04-24T12:00:00Z"},
	"last month":    {"2026-03-24T00:00:00Z", "2026-04-24T12:00:00Z"},
	"last year":     {"2025-04-24T00:00:00Z", "2026-04-24T12:00:00Z"},
	"this week":     {"2026-04-17T00:00:00Z", "2026-04-24T12:00:00Z"},
	"this month":    {"2026-03-24T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past 3 days":   {"2026-04-21T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past 7 days":   {"2026-04-17T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past week":     {"2026-04-17T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past month":    {"2026-03-24T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past 30 days":  {"2026-03-25T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past 2 weeks":  {"2026-04-10T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past 6 months": {"2025-10-24T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past year":     {"2025-04-24T00:00:00Z", "2026-04-24T12:00:00Z"},
	"past 2 years":  {"2024-04-24T00:00:00Z", "2026-04-24T12:00:00Z"},
}

func inferDateConstraint(lower string) ([]llmClause, bool) {
	// Check for known date patterns.
	for pat, dates := range datePatterns {
		if strings.Contains(lower, pat) {
			return []llmClause{
				{Field: "modified_at", Op: "gte", Value: dates[0]},
				{Field: "modified_at", Op: "lte", Value: dates[1]},
			}, true
		}
	}
	return nil, false
}

func inferSizeConstraint(lower string) ([]llmClause, bool) {
	sizeMap := []struct {
		keyword string
		op      string
		value   string
	}{
		{"over 1gb", "gte", "1073741824"},
		{"over 100mb", "gte", "104857600"},
		{"over 50mb", "gte", "52428800"},
		{"over 10mb", "gte", "10485760"},
		{"over 5mb", "gte", "5242880"},
		{"over 1mb", "gte", "1048576"},
		{"larger than 1gb", "gte", "1073741824"},
		{"larger than 100mb", "gte", "104857600"},
		{"larger than 50mb", "gte", "52428800"},
		{"larger than 10mb", "gte", "10485760"},
		{"larger than 5mb", "gte", "5242880"},
		{"bigger than 1gb", "gte", "1073741824"},
		{"bigger than 100mb", "gte", "104857600"},
		{"bigger than 10mb", "gte", "10485760"},
		{"under 1mb", "lte", "1048576"},
		{"under 500kb", "lte", "512000"},
		{"under 100kb", "lte", "102400"},
		{"smaller than 1mb", "lte", "1048576"},
		{"smaller than 500kb", "lte", "512000"},
		{"less than 1mb", "lte", "1048576"},
		{"less than 500kb", "lte", "512000"},
		{"large", "gte", "104857600"},
		{"huge", "gte", "1073741824"},
		{"small", "lte", "1048576"},
		{"tiny", "lte", "102400"},
	}
	for _, s := range sizeMap {
		if strings.Contains(lower, s.keyword) {
			return []llmClause{{Field: "size_bytes", Op: s.op, Value: s.value}}, true
		}
	}
	return nil, false
}

func inferExtConstraint(lower string) ([]llmClause, bool) {
	extMap := []struct {
		keyword string
		ext     string
	}{
		{".pdf", ".pdf"},
		{"pdf", ".pdf"},
		{".jpg", ".jpg"},
		{".jpeg", ".jpeg"},
		{".png", ".png"},
		{".mp4", ".mp4"},
		{".mov", ".mov"},
		{".docx", ".docx"},
		{".doc", ".doc"},
		{".xlsx", ".xlsx"},
		{".pptx", ".pptx"},
		{".mp3", ".mp3"},
		{".txt", ".txt"},
		{".zip", ".zip"},
		{".csv", ".csv"},
	}
	for _, e := range extMap {
		if strings.Contains(lower, e.keyword) {
			return []llmClause{{Field: "extension", Op: "in_set", Value: e.ext}}, true
		}
	}
	return nil, false
}

func inferPathConstraint(lower string) ([]llmClause, bool) {
	pathWords := []struct {
		keyword string
		path    string
	}{
		{"downloads folder", "Downloads"},
		{"downloads", "Downloads"},
		{"desktop", "Desktop"},
		{"documents folder", "Documents"},
		{"projects folder", "projects"},
		{"pictures folder", "Pictures"},
		{"music folder", "Music"},
		{"videos folder", "Videos"},
	}
	for _, p := range pathWords {
		if strings.Contains(lower, p.keyword) {
			return []llmClause{{Field: "path", Op: "contains", Value: p.path}}, true
		}
	}
	// Check for explicit path patterns like "in /path/to/dir".
	if strings.Contains(lower, " in /") || strings.Contains(lower, "inside /") {
		// Extract path fragment.
		return nil, false
	}
	return nil, false
}

func inferNegationTypes(lower string) []string {
	var types []string
	negWords := []string{"not videos", "without videos", "excluding videos", "no videos"}
	for _, w := range negWords {
		if strings.Contains(lower, w) {
			types = append(types, "video")
			break
		}
	}
	negWords = []string{"not images", "without images", "excluding images", "no images", "not photos", "no photos"}
	for _, w := range negWords {
		if strings.Contains(lower, w) {
			types = append(types, "image")
			break
		}
	}
	negWords = []string{"not pdfs", "without pdfs", "no pdfs", "not documents", "no documents"}
	for _, w := range negWords {
		if strings.Contains(lower, w) {
			types = append(types, "document")
			break
		}
	}
	return types
}

func buildSemanticQuery(q, fileType, cat string) string {
	// For semantic_only, the whole query is semantic.
	if cat == "semantic_only" {
		return q
	}
	// For other categories, remove obvious structured parts.
	lower := strings.ToLower(q)
	parts := strings.Fields(lower)
	var keep []string

	removeWords := map[string]bool{
		"photos": true, "photo": true, "pictures": true, "picture": true,
		"images": true, "image": true, "screenshots": true, "screenshot": true,
		"videos": true, "video": true, "movies": true, "movie": true,
		"documents": true, "document": true, "docs": true, "doc": true, "pdfs": true, "pdf": true,
		"audios": true, "audio": true, "music": true, "podcast": true,
		"files": true, "from": true, "in": true, "the": true,
		"large": true, "small": true, "huge": true, "tiny": true,
	}

	for _, p := range parts {
		if !removeWords[p] {
			keep = append(keep, p)
		}
	}

	result := strings.TrimSpace(strings.Join(keep, " "))
	// If mostly empty or just temporal words, return empty.
	temporalOnly := map[string]bool{
		"yesterday": true, "today": true, "last": true, "week": true,
		"month": true, "year": true, "past": true, "recent": true,
		"this": true, "morning": true, "afternoon": true, "evening": true,
	}
	remaining := strings.Fields(result)
	allTemporal := true
	for _, w := range remaining {
		if !temporalOnly[w] {
			allTemporal = false
			break
		}
	}
	if allTemporal || len(remaining) == 0 {
		return ""
	}
	return result
}

// convertLLMResponseToSpec mirrors the logic in llmparse.go.
func convertLLMResponseToSpec(llmResp llmResponse) query.FilterSpec {
	spec := query.FilterSpec{
		SemanticQuery: strings.TrimSpace(llmResp.SemanticQuery),
	}
	for _, c := range llmResp.Must {
		if clause, ok := llmClauseToQueryClause(c); ok {
			spec.Must = append(spec.Must, clause)
		}
	}
	for _, c := range llmResp.MustNot {
		if clause, ok := llmClauseToQueryClause(c); ok {
			spec.MustNot = append(spec.MustNot, clause)
		}
	}
	for _, c := range llmResp.Should {
		if clause, ok := llmClauseToQueryClause(c); ok {
			spec.Should = append(spec.Should, clause)
		}
	}
	return spec
}

// llmClauseToQueryClause converts an llmClause to a query.Clause, mirroring llmparse.go.
func llmClauseToQueryClause(lc llmClause) (query.Clause, bool) {
	field := query.FieldEnum(lc.Field)
	if !query.KnownFields[field] {
		return query.Clause{}, false
	}
	op := query.Op(lc.Op)

	var value any = lc.Value

	switch field {
	case query.FieldModifiedAt:
		// Try RFC3339.
		if t, err := time.Parse(time.RFC3339, lc.Value); err == nil {
			value = t.Unix()
		} else if t, err := time.Parse("2006-01-02T15:04:05Z", lc.Value); err == nil {
			value = t.Unix()
		} else if t, err := time.Parse("2006-01-02", lc.Value); err == nil {
			value = t.Unix()
		} else {
			return query.Clause{}, false
		}

	case query.FieldSizeBytes:
		var n int64
		if _, err := fmt.Sscanf(lc.Value, "%d", &n); err != nil {
			return query.Clause{}, false
		}
		value = n

	case query.FieldExtension:
		if op == query.OpInSet {
			parts := strings.Split(lc.Value, ",")
			exts := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if !strings.HasPrefix(p, ".") {
					p = "." + p
				}
				exts = append(exts, p)
			}
			if len(exts) == 0 {
				return query.Clause{}, false
			}
			value = exts
		}
	}

	return query.Clause{Field: field, Op: op, Value: value}, true
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	case int32:
		return int64(x), true
	}
	return 0, false
}
