// Package query — golden eval harness.
// PLACEHOLDER: 5-case fixture; grows to 200 cases in Phase 2.
package query

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Component weights — must sum to 1.0 exactly.
// ---------------------------------------------------------------------------

var componentWeights = map[string]float64{
	"file_type":   0.20,
	"modified_at": 0.30,
	"size_bytes":  0.15,
	"extension":   0.15,
	"path":        0.10,
	"semantic":    0.10,
}

const (
	// GoldenEvalOverallFloor is the minimum acceptable overall score.
	GoldenEvalOverallFloor = 0.90
	// GoldenEvalMaxCategoryRegression is the maximum allowed per-category drop
	// vs. committed baseline.
	GoldenEvalMaxCategoryRegression = 0.02
)

// fieldForComponent maps component names to their FieldEnum values.
var fieldForComponent = map[string]FieldEnum{
	"file_type":   FieldFileType,
	"modified_at": FieldModifiedAt,
	"size_bytes":  FieldSizeBytes,
	"extension":   FieldExtension,
	"path":        FieldPath,
}

// ---------------------------------------------------------------------------
// TestGoldenEval — main harness
// ---------------------------------------------------------------------------

func TestGoldenEval(t *testing.T) {
	cases := loadGoldenFixture(t, "testdata/golden_queries.jsonl")
	if len(cases) == 0 {
		t.Fatal("golden fixture is empty")
	}

	parser := &LLMParser{
		limiter:    alwaysAllowLimiter{},
		model:      DefaultLLMConfig().Model,
		maxRetries: 0, // single attempt; fake errors are terminal
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	parser.generate = fakeGenerate(cases)

	scores := make([]float64, len(cases))
	cScores := make([]caseScore, len(cases))

	for i, c := range cases {
		got, _ := runGoldenCase(t, c, parser)
		s := scoreCase(got, c.Expected)
		scores[i] = s
		cScores[i] = caseScore{id: c.ID, score: s}
		t.Logf("case %-30s score=%.3f", c.ID, s)
	}

	overall := overallMean(scores)
	t.Logf("overall score: %.3f (floor %.3f)", overall, GoldenEvalOverallFloor)

	// RECORDING_BASELINE=1 bypasses the floor gate so we can capture a pre-change
	// baseline even when overall score is below the floor.  This is a permanent
	// code change; the gate is real for all normal CI runs.
	if os.Getenv("RECORDING_BASELINE") != "1" && overall < GoldenEvalOverallFloor {
		worst := topNWorst(cScores, 5)
		ids := make([]string, len(worst))
		for i, w := range worst {
			ids[i] = fmt.Sprintf("%s(%.3f)", w.id, w.score)
		}
		t.Fatalf("overall score %.3f below floor %.3f; worst cases: %v",
			overall, GoldenEvalOverallFloor, ids)
	}

	perCat := categoryScores(cases, scores)
	baseline := loadBaseline(t, "testdata/golden_baseline.json")

	for cat, got := range perCat {
		if base, ok := baseline.PerCategory[cat]; ok {
			if got < base-GoldenEvalMaxCategoryRegression {
				t.Errorf("category %q regressed: baseline=%.3f current=%.3f delta=%.3f (max %.3f)",
					cat, base, got, base-got, GoldenEvalMaxCategoryRegression)
			}
		}
	}

	if os.Getenv("UPDATE_GOLDEN_BASELINE") == "1" {
		writeBaseline(t, filepath.Join("testdata", "golden_baseline.json"), goldenBaseline{
			Overall:     overall,
			PerCategory: perCat,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Notes:       os.Getenv("UPDATE_GOLDEN_BASELINE_NOTES"),
		})
		t.Logf("baseline updated: overall=%.3f", overall)
	}
}

// ---------------------------------------------------------------------------
// TestGoldenFixtureShape — structural validation
// ---------------------------------------------------------------------------

func TestGoldenFixtureShape(t *testing.T) {
	cases := loadGoldenFixture(t, "testdata/golden_queries.jsonl")
	if len(cases) == 0 {
		t.Fatal("golden fixture must have at least one case")
	}
	for _, c := range cases {
		if c.ID == "" {
			t.Errorf("case has empty id")
		}
		if c.Query == "" {
			t.Errorf("case %s has empty query", c.ID)
		}
		switch c.Mode {
		case "grammar_only", "llm_with_fake", "merged":
		default:
			t.Errorf("case %s has unknown mode %q", c.ID, c.Mode)
		}
	}
}

// ---------------------------------------------------------------------------
// TestGoldenBaselineShape — structural validation
// ---------------------------------------------------------------------------

func TestGoldenBaselineShape(t *testing.T) {
	baseline := loadBaseline(t, "testdata/golden_baseline.json")
	if math.IsNaN(baseline.Overall) || math.IsInf(baseline.Overall, 0) {
		t.Errorf("baseline.overall is not finite: %v", baseline.Overall)
	}
	if baseline.PerCategory == nil {
		t.Error("baseline.per_category must not be nil")
	}
}

// ---------------------------------------------------------------------------
// TestScorerWeights — weights must sum to 1.0
// ---------------------------------------------------------------------------

func TestScorerWeights(t *testing.T) {
	sum := 0.0
	for _, w := range componentWeights {
		sum += w
	}
	const eps = 1e-9
	if math.Abs(sum-1.0) > eps {
		t.Errorf("componentWeights sum = %.10f, want 1.0", sum)
	}
}

// ---------------------------------------------------------------------------
// TestScorerValueKinds — unit-test matching rules per value_kind
// ---------------------------------------------------------------------------

func TestScorerValueKinds(t *testing.T) {
	t.Run("unix_within_tolerance", func(t *testing.T) {
		g := Clause{Field: FieldModifiedAt, Op: OpGte, Value: int64(1776384001)}
		w := goldenClause{Field: "modified_at", Op: "gte", ValueKind: "unix", Value: int64(1776384000)}
		if !clauseValueMatches(g, w) {
			t.Error("expected unix ±1s to match")
		}
	})
	t.Run("unix_outside_tolerance", func(t *testing.T) {
		g := Clause{Field: FieldModifiedAt, Op: OpGte, Value: int64(1776384002)}
		w := goldenClause{Field: "modified_at", Op: "gte", ValueKind: "unix", Value: int64(1776384000)}
		if clauseValueMatches(g, w) {
			t.Error("expected unix >1s diff to not match")
		}
	})
	t.Run("size_bytes_exact", func(t *testing.T) {
		g := Clause{Field: FieldSizeBytes, Op: OpGte, Value: int64(104857600)}
		w := goldenClause{Field: "size_bytes", Op: "gte", ValueKind: "int", Value: int64(104857600)}
		if !clauseValueMatches(g, w) {
			t.Error("expected exact size_bytes match")
		}
	})
	t.Run("size_bytes_mismatch", func(t *testing.T) {
		g := Clause{Field: FieldSizeBytes, Op: OpGte, Value: int64(104857601)}
		w := goldenClause{Field: "size_bytes", Op: "gte", ValueKind: "int", Value: int64(104857600)}
		if clauseValueMatches(g, w) {
			t.Error("expected size_bytes mismatch to fail")
		}
	})
	t.Run("string_case_insensitive", func(t *testing.T) {
		g := Clause{Field: FieldFileType, Op: OpEq, Value: "image"}
		w := goldenClause{Field: "file_type", Op: "eq", ValueKind: "string", Value: "IMAGE"}
		if !clauseValueMatches(g, w) {
			t.Error("expected case-insensitive string match")
		}
	})
	t.Run("semantic_case_insensitive", func(t *testing.T) {
		if scoreSemantic("Tax Documents", "tax documents") != 1.0 {
			t.Error("expected case-insensitive semantic match")
		}
		if scoreSemantic("", "") != 1.0 {
			t.Error("expected empty-vs-empty to match")
		}
		if scoreSemantic("foo", "") != 0.0 {
			t.Error("expected non-empty vs empty to not match")
		}
	})
	t.Run("string_list_set_equality", func(t *testing.T) {
		g := Clause{Field: FieldExtension, Op: OpInSet, Value: []string{".jpg", ".png"}}
		w := goldenClause{Field: "extension", Op: "in_set", ValueKind: "string_list",
			Value: []any{".png", ".jpg"}}
		if !clauseValueMatches(g, w) {
			t.Error("expected set-equal string_list match")
		}
	})
}

// ---------------------------------------------------------------------------
// TestOverallFloor — fault-inject a failing case, verify gate logic fires.
// ---------------------------------------------------------------------------

func TestOverallFloor(t *testing.T) {
	// With 5 cases where one scores 0.0: mean = 0.80 < 0.90 → gate fires.
	cases := []goldenCase{
		{ID: "bad_case", Tags: []string{"edge"}},
		{ID: "good1", Tags: []string{"kind:image"}},
		{ID: "good2", Tags: []string{"kind:image"}},
		{ID: "good3", Tags: []string{"kind:image"}},
		{ID: "good4", Tags: []string{"kind:image"}},
	}
	scores := []float64{0.0, 1.0, 1.0, 1.0, 1.0}
	cScores := []caseScore{
		{id: "bad_case", score: 0.0},
		{id: "good1", score: 1.0},
		{id: "good2", score: 1.0},
		{id: "good3", score: 1.0},
		{id: "good4", score: 1.0},
	}

	overall := overallMean(scores)
	if overall >= GoldenEvalOverallFloor {
		t.Fatalf("fault injection broken: overall=%.3f should be < %.3f",
			overall, GoldenEvalOverallFloor)
	}

	worst := topNWorst(cScores, 5)
	if len(worst) == 0 {
		t.Fatal("topNWorst returned empty list")
	}
	if worst[0].id != "bad_case" {
		t.Errorf("expected bad_case to be worst, got %s", worst[0].id)
	}

	cats := categoryScores(cases, scores)
	if cats["edge"] != 0.0 {
		t.Errorf("edge category: got %.3f, want 0.0", cats["edge"])
	}
}

// ---------------------------------------------------------------------------
// TestCategoryRegression — fault-inject inflated baseline, verify gate fires.
// ---------------------------------------------------------------------------

func TestCategoryRegression(t *testing.T) {
	currentScores := map[string]float64{
		"kind": 0.85,
		"edge": 0.95,
	}
	// Inflated baseline: "kind" exceeds current by > GoldenEvalMaxCategoryRegression.
	inflatedBaseline := goldenBaseline{
		Overall: 1.0,
		PerCategory: map[string]float64{
			"kind": 0.95, // delta = 0.10 > 0.02 → regression
			"edge": 0.95,
		},
	}

	regressionFound := false
	for cat, got := range currentScores {
		if base, ok := inflatedBaseline.PerCategory[cat]; ok {
			if got < base-GoldenEvalMaxCategoryRegression {
				regressionFound = true
				t.Logf("correctly detected regression in %q: base=%.3f current=%.3f delta=%.3f",
					cat, base, got, base-got)
			}
		}
	}
	if !regressionFound {
		t.Fatal("expected regression to be detected with inflated baseline")
	}
}

// ---------------------------------------------------------------------------
// TestUpdateBaseline — verify writeBaseline round-trips correctly.
// ---------------------------------------------------------------------------

func TestUpdateBaseline(t *testing.T) {
	t.Run("read_placeholder_baseline", func(t *testing.T) {
		// After Phase 2 replaces the placeholder with a real baseline, this sub-test
		// only validates the file parses cleanly (not that it has Phase-1 placeholder values).
		b := loadBaseline(t, "testdata/golden_baseline.json")
		if b.PerCategory == nil {
			t.Error("baseline per_category must not be nil")
		}
	})

	t.Run("write_and_read_back", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "baseline.json")
		input := goldenBaseline{
			Overall:     0.95,
			PerCategory: map[string]float64{"kind": 0.90, "edge": 1.0},
			GeneratedAt: "2026-04-24T00:00:00Z",
			Notes:       "test write",
		}
		writeBaseline(t, path, input)

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("file not written: %v", err)
		}
		var got goldenBaseline
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if got.Overall != 0.95 {
			t.Errorf("overall: got %.3f, want 0.95", got.Overall)
		}
		if got.PerCategory["kind"] != 0.90 {
			t.Errorf("per_category kind: got %.3f, want 0.90", got.PerCategory["kind"])
		}
		if got.Notes != "test write" {
			t.Errorf("notes: got %q, want 'test write'", got.Notes)
		}
	})

	t.Run("update_env_not_set_by_default", func(t *testing.T) {
		if os.Getenv("UPDATE_GOLDEN_BASELINE") == "1" {
			t.Skip("UPDATE_GOLDEN_BASELINE=1 set; skipping non-update assertion")
		}
		// After Phase 2 records the real baseline, overall will be non-zero.
		// We only assert that the file parses as a valid goldenBaseline; the
		// placeholder-specific check (overall == 0.0) is intentionally removed here
		// because the baseline is rewritten in Phase 2.
		orig := loadBaseline(t, "testdata/golden_baseline.json")
		if orig.PerCategory == nil {
			t.Error("baseline per_category must not be nil")
		}
	})
}
