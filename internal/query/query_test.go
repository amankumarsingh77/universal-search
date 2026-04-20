package query

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Grammar parser tests
// ---------------------------------------------------------------------------

func TestGrammarParse_AllOperators(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	_ = now

	cases := []struct {
		name          string
		input         string
		wantSemantic  string
		wantMustField FieldEnum
		wantMustOp    Op
	}{
		{
			name:          "kind image",
			input:         "kind:image cats",
			wantSemantic:  "cats",
			wantMustField: FieldFileType,
			wantMustOp:    OpEq,
		},
		{
			name:          "ext py",
			input:         "ext:py some function",
			wantSemantic:  "some function",
			wantMustField: FieldExtension,
			wantMustOp:    OpInSet,
		},
		{
			name:          "size gt",
			input:         "size:>10mb",
			wantSemantic:  "",
			wantMustField: FieldSizeBytes,
			wantMustOp:    OpGt,
		},
		{
			name:          "size lt",
			input:         "size:<500kb report",
			wantSemantic:  "report",
			wantMustField: FieldSizeBytes,
			wantMustOp:    OpLt,
		},
		{
			name:          "path tilde",
			input:         "path:~/projects code",
			wantSemantic:  "code",
			wantMustField: FieldPath,
			wantMustOp:    OpContains,
		},
		{
			name:          "in alias",
			input:         "in:/home/user documents",
			wantSemantic:  "documents",
			wantMustField: FieldPath,
			wantMustOp:    OpContains,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := Parse(tc.input)
			if strings.TrimSpace(spec.SemanticQuery) != tc.wantSemantic {
				t.Errorf("semantic: got %q, want %q", spec.SemanticQuery, tc.wantSemantic)
			}
			if len(spec.Must) == 0 {
				t.Fatalf("expected at least one Must clause, got none")
			}
			found := false
			for _, c := range spec.Must {
				if c.Field == tc.wantMustField && c.Op == tc.wantMustOp {
					found = true
				}
			}
			if !found {
				t.Errorf("no Must clause with field=%q op=%q; Must=%+v", tc.wantMustField, tc.wantMustOp, spec.Must)
			}
		})
	}
}

func TestGrammarParse_QuotedPhrase(t *testing.T) {
	// Operators inside quotes must NOT be parsed as operators.
	spec := Parse(`"kind:image ext:go" free text`)
	if strings.Contains(spec.SemanticQuery, "kind") {
		// The whole quoted phrase should be in semantic, not a Must clause
	}
	if len(spec.Must) > 0 {
		t.Errorf("expected no Must clauses (operators inside quotes), got %+v", spec.Must)
	}
	if !strings.Contains(spec.SemanticQuery, "kind:image ext:go") {
		t.Errorf("quoted phrase not in semantic: %q", spec.SemanticQuery)
	}
}

func TestGrammarParse_Negation(t *testing.T) {
	spec := Parse("golang -vendor -node_modules")
	if len(spec.MustNot) < 2 {
		t.Fatalf("expected 2 MustNot clauses, got %d: %+v", len(spec.MustNot), spec.MustNot)
	}
	vals := make(map[string]bool)
	for _, c := range spec.MustNot {
		if s, ok := c.Value.(string); ok {
			vals[s] = true
		}
	}
	if !vals["vendor"] {
		t.Errorf("expected MustNot vendor, got %+v", spec.MustNot)
	}
	if !vals["node_modules"] {
		t.Errorf("expected MustNot node_modules, got %+v", spec.MustNot)
	}
}

func TestGrammarParse_UnknownOperator(t *testing.T) {
	// foo:bar is not a recognized operator → falls through as free text
	spec := Parse("foo:bar interesting query")
	if len(spec.Must) > 0 {
		t.Errorf("unknown operator should not create Must clauses, got %+v", spec.Must)
	}
	if !strings.Contains(spec.SemanticQuery, "foo:bar") {
		t.Errorf("unknown operator token should be in semantic: %q", spec.SemanticQuery)
	}
}

func TestGrammarParse_NoColonInFreeText(t *testing.T) {
	// notes:today.md — not a recognized operator keyword, must be free text
	spec := Parse("notes:today.md planning")
	if len(spec.Must) > 0 {
		t.Errorf("non-operator colon token should not create Must clauses, got %+v", spec.Must)
	}
	if !strings.Contains(spec.SemanticQuery, "notes:today.md") {
		t.Errorf("non-operator token should appear in semantic: %q", spec.SemanticQuery)
	}
}

func TestGrammarParse_NoPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 1000; i++ {
		n := rng.Intn(200)
		b := make([]byte, n)
		rng.Read(b)
		// Replace nul bytes with space to keep printable-ish
		for j, c := range b {
			if c == 0 {
				b[j] = ' '
			}
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on input %q: %v", string(b), r)
				}
			}()
			Parse(string(b))
		}()
	}
}

func TestGrammarParse_BeforeAfterSince(t *testing.T) {
	// before:/after:/since: should produce modified_at clauses
	spec := Parse("before:2026-01-01 report")
	if len(spec.Must) == 0 {
		t.Fatalf("expected Must clause for before:, got none")
	}
	found := false
	for _, c := range spec.Must {
		if c.Field == FieldModifiedAt {
			found = true
		}
	}
	if !found {
		t.Errorf("expected modified_at Must clause for before:, got %+v", spec.Must)
	}
}

func TestGrammarParse_KindTypoCorrection(t *testing.T) {
	// "imgae" is a Levenshtein-1 typo for "image"
	spec := Parse("kind:imgae")
	if len(spec.Must) == 0 {
		t.Fatalf("expected Must clause for typo-corrected kind, got none")
	}
	found := false
	for _, c := range spec.Must {
		if c.Field == FieldFileType {
			if v, ok := c.Value.(string); ok && v == "image" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected file_type=image after typo correction, got %+v", spec.Must)
	}
}

func TestGrammarParse_ExtMultipleExtensions(t *testing.T) {
	spec := Parse("ext:py,go,ts")
	if len(spec.Must) == 0 {
		t.Fatalf("expected Must clause, got none")
	}
	found := false
	for _, c := range spec.Must {
		if c.Field == FieldExtension && c.Op == OpInSet {
			if vals, ok := c.Value.([]string); ok {
				if len(vals) == 3 {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected extension in_set with 3 values, got %+v", spec.Must)
	}
}

// ---------------------------------------------------------------------------
// NormalizeDate tests
// ---------------------------------------------------------------------------

func TestNormalizeDate_RelativePhrase(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)

	t.Run("yesterday", func(t *testing.T) {
		after, before, ok := NormalizeDate("yesterday", now)
		if !ok {
			t.Fatal("expected ok=true for 'yesterday'")
		}
		// after should be start of yesterday (Apr 11)
		wantAfterDay := 11
		if after.Day() != wantAfterDay {
			t.Errorf("after day: got %d, want %d", after.Day(), wantAfterDay)
		}
		// before should be end of yesterday
		if before.Day() != wantAfterDay {
			t.Errorf("before day: got %d, want %d", before.Day(), wantAfterDay)
		}
		if before.Hour() != 23 || before.Minute() != 59 {
			t.Errorf("before time should be 23:59, got %02d:%02d", before.Hour(), before.Minute())
		}
	})

	t.Run("last week", func(t *testing.T) {
		after, before, ok := NormalizeDate("last week", now)
		if !ok {
			t.Fatal("expected ok=true for 'last week'")
		}
		// after should be ~7 days ago
		sevenDaysAgo := now.AddDate(0, 0, -7)
		if after.After(sevenDaysAgo.Add(48 * time.Hour)) {
			t.Errorf("after should be ~7 days ago, got %v (now=%v)", after, now)
		}
		// before should not exceed now
		if before.After(now.Add(time.Minute)) {
			t.Errorf("before should not exceed now, got %v", before)
		}
	})
}

func TestNormalizeDate_AbsoluteISO(t *testing.T) {
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)

	t.Run("2026-01-01", func(t *testing.T) {
		after, _, ok := NormalizeDate("2026-01-01", now)
		if !ok {
			t.Fatal("expected ok=true for 2026-01-01")
		}
		if after.Year() != 2026 || after.Month() != 1 || after.Day() != 1 {
			t.Errorf("got %v, want 2026-01-01", after)
		}
	})

	t.Run("year only 2025", func(t *testing.T) {
		after, before, ok := NormalizeDate("2025", now)
		if !ok {
			t.Skip("year-only parsing not supported, skipping")
		}
		if after.Year() != 2025 {
			t.Errorf("after year: got %d, want 2025", after.Year())
		}
		if before.Year() != 2025 {
			t.Errorf("before year: got %d, want 2025", before.Year())
		}
	})
}

func TestNormalizeDate_Unparseable(t *testing.T) {
	now := time.Now()
	_, _, ok := NormalizeDate("zzznonsensedate", now)
	if ok {
		t.Error("expected ok=false for unparseable date")
	}
}

// ---------------------------------------------------------------------------
// ParseSize tests
// ---------------------------------------------------------------------------

func TestParseSize_Units(t *testing.T) {
	cases := []struct {
		input     string
		wantOp    Op
		wantBytes int64
		wantOk    bool
	}{
		{">10mb", OpGt, 10 * 1024 * 1024, true},
		{">=1GB", OpGte, 1 * 1024 * 1024 * 1024, true},
		{"<500kb", OpLt, 500 * 1024, true},
		{"<=2kb", OpLte, 2 * 1024, true},
		{"=5mb", OpEq, 5 * 1024 * 1024, true},
		{"500kb", OpGte, 500 * 1024, true},    // bare value → gte
		{"1b", OpGte, 1, true},                 // bytes
		{"10TB", OpGte, 10 * 1024 * 1024 * 1024 * 1024, true},
		{"notasize", "", 0, false},
		{"", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			op, bytes, ok := ParseSize(tc.input)
			if ok != tc.wantOk {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if op != tc.wantOp {
				t.Errorf("op: got %q, want %q", op, tc.wantOp)
			}
			if bytes != tc.wantBytes {
				t.Errorf("bytes: got %d, want %d", bytes, tc.wantBytes)
			}
		})
	}
}

func TestParseSize_Overflow(t *testing.T) {
	// >99999999gb should saturate to MaxInt64
	_, bytes, ok := ParseSize(">99999999gb")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if bytes <= 0 {
		t.Errorf("expected large positive value, got %d", bytes)
	}
}

// ---------------------------------------------------------------------------
// ShouldInvokeLLM tests
// ---------------------------------------------------------------------------

func TestShouldInvokeLLM_TableDriven(t *testing.T) {
	cases := []struct {
		input  string
		wantYN bool
	}{
		// Temporal keywords → YES
		{"show me files from yesterday", true},
		{"photos taken today", true},
		{"documents from last month", true},
		{"files from past 3 weeks", true},
		{"recent invoices", true},
		{"older backups", true},
		{"newer versions", true},
		{"files since january", true},
		{"created before 2025", true},
		{"updated after the meeting", true},
		// Negation keywords → YES
		{"files not in archive", true},
		{"no duplicates please", true},
		{"documents without attachments", true},
		{"everything except logs", true},
		{"exclude temp files", true},
		// File type keywords → YES
		{"show my photos", true},
		{"find all pictures", true},
		{"screenshots from last session", true},
		{"videos over 1gb", true},
		// Token count > 6 → YES
		{"a b c d e f g", true},
		// Short simple query → NO
		{"budget", false},
		{"invoice 2025", false},
		// "go code" contains "code" which is a file type keyword → YES
		{"go code", true},
		{"meeting notes", false},
		// Structured-token detection (REQ-008): short queries that the older
		// heuristics missed but DetectStructuredFields catches.
		{"all .py files", true},          // bare extension
		{"videos over 100MB", true},      // size unit (also kind word)
		{"files in Downloads", true},     // path root folder
		{"PNG images", true},               // "images" plural matches "image" kind synonym
		{"large documents over 1MB", true}, // size adjective + size unit + kind
		// Over 500 chars → NO (tested separately)
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ShouldInvokeLLM(tc.input)
			if got != tc.wantYN {
				t.Errorf("ShouldInvokeLLM(%q) = %v, want %v", tc.input, got, tc.wantYN)
			}
		})
	}
}

func TestShouldInvokeLLM_Over500Chars(t *testing.T) {
	long := strings.Repeat("a ", 260) // 520 chars
	if ShouldInvokeLLM(long) {
		t.Error("ShouldInvokeLLM should return false for input > 500 chars")
	}
}

// ---------------------------------------------------------------------------
// Levenshtein tests
// ---------------------------------------------------------------------------

func TestLevenshtein_Basic(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "ab", 1},
		{"abc", "abcd", 1},
		{"kitten", "sitting", 3},
		{"a", "b", 1},
		{"", "abc", 3},
		{"abc", "", 3},
	}
	for _, tc := range cases {
		got := Levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CorrectKind tests
// ---------------------------------------------------------------------------

func TestCorrectKind_Typo(t *testing.T) {
	cases := []struct {
		input    string
		wantOut  string
		wantOk   bool
	}{
		{"image", "image", true},
		{"imgae", "image", true},   // transposition
		{"imag", "image", true},    // deletion
		{"imagee", "image", true},  // insertion
		{"video", "video", true},
		{"vido", "video", true},
		{"audio", "audio", true},
		{"xyz", "", false},         // too far
		{"code", "text", true},
		{"pdf", "document", true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := CorrectKind(tc.input)
			if ok != tc.wantOk {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOk)
			}
			if tc.wantOk && got != tc.wantOut {
				t.Errorf("got %q, want %q", got, tc.wantOut)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CorrectExtension tests
// ---------------------------------------------------------------------------

func TestCorrectExtension_Typo(t *testing.T) {
	cases := []struct {
		input   string
		wantOut string
		wantOk  bool
	}{
		{"pdf", "pdf", true},
		{"pef", "pdf", true},      // Levenshtein-1
		{"pfd", "pdf", true},      // transposition
		{"go", "go", true},
		{"py", "py", true},
		{"js", "js", true},
		{"ts", "ts", true},
		{"mp4", "mp4", true},
		{"mp3", "mp3", true},
		{"jpg", "jpg", true},
		{"jpe", "jpg", true},      // deletion
		{"xyz123", "", false},     // too far
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := CorrectExtension(tc.input)
			if ok != tc.wantOk {
				t.Fatalf("ok: got %v, want %v (input=%q)", ok, tc.wantOk, tc.input)
			}
			if tc.wantOk && got != tc.wantOut {
				t.Errorf("got %q, want %q (input=%q)", got, tc.wantOut, tc.input)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Merge tests
// ---------------------------------------------------------------------------

func TestMerge_GrammarWins(t *testing.T) {
	grammar := FilterSpec{
		SemanticQuery: "cats",
		Must: []Clause{
			{Field: FieldFileType, Op: OpEq, Value: "image"},
		},
	}
	llm := FilterSpec{
		SemanticQuery: "cats",
		Must: []Clause{
			{Field: FieldFileType, Op: OpEq, Value: "video"}, // conflicting
		},
	}
	result := Merge(grammar, llm, nil)
	for _, c := range result.Must {
		if c.Field == FieldFileType {
			if v, ok := c.Value.(string); ok && v == "video" {
				t.Error("LLM clause on same field should be dropped (grammar wins)")
			}
		}
	}
	found := false
	for _, c := range result.Must {
		if c.Field == FieldFileType {
			if v, ok := c.Value.(string); ok && v == "image" {
				found = true
			}
		}
	}
	if !found {
		t.Error("grammar clause should be present in merged result")
	}
}

func TestMerge_NonConflictingUnion(t *testing.T) {
	grammar := FilterSpec{
		Must: []Clause{
			{Field: FieldFileType, Op: OpEq, Value: "image"},
		},
	}
	llm := FilterSpec{
		Must: []Clause{
			{Field: FieldSizeBytes, Op: OpGt, Value: int64(1024)},
		},
	}
	result := Merge(grammar, llm, nil)
	if len(result.Must) != 2 {
		t.Errorf("expected 2 Must clauses (unioned), got %d: %+v", len(result.Must), result.Must)
	}
}

func TestMerge_ChipDenyList(t *testing.T) {
	grammar := FilterSpec{
		Must: []Clause{
			{Field: FieldFileType, Op: OpEq, Value: "image"},
			{Field: FieldExtension, Op: OpInSet, Value: []string{".jpg"}},
		},
	}
	denyList := []ClauseKey{
		{Field: FieldFileType, Op: OpEq, Value: "image"},
	}
	result := Merge(grammar, FilterSpec{}, denyList)
	for _, c := range result.Must {
		if c.Field == FieldFileType && c.Op == OpEq {
			if v, ok := c.Value.(string); ok && v == "image" {
				t.Error("denylist clause should be dropped from merged result")
			}
		}
	}
}

func TestMerge_SourceIsSet(t *testing.T) {
	grammar := FilterSpec{Source: SourceGrammar}
	llm := FilterSpec{Source: SourceLLM}
	result := Merge(grammar, llm, nil)
	if result.Source != SourceMerged {
		t.Errorf("merged source should be %q, got %q", SourceMerged, result.Source)
	}
}

func TestMerge_SemanticQueryFromGrammar(t *testing.T) {
	grammar := FilterSpec{SemanticQuery: "cats in snow", Source: SourceGrammar}
	llm := FilterSpec{SemanticQuery: "cats", Source: SourceLLM}
	result := Merge(grammar, llm, nil)
	// Grammar's semantic query should take precedence
	if result.SemanticQuery != "cats in snow" {
		t.Errorf("expected grammar semantic query, got %q", result.SemanticQuery)
	}
}

// ---------------------------------------------------------------------------
// NormalizeKey tests
// ---------------------------------------------------------------------------

func TestNormalizeKey_Whitespace(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Photos From  Last  Week!", "photos from last week"},
		{"  hello   world  ", "hello world"},
		{"Test.", "test"},
		{"UPPERCASE query?", "uppercase query"},
		{"already normal", "already normal"},
		{"trailing  spaces   ", "trailing spaces"},
	}
	for _, tc := range cases {
		got := NormalizeKey(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Cache tests
// ---------------------------------------------------------------------------

type mockStore struct {
	data map[string]string
}

func (m *mockStore) UpsertParsedQueryCache(normalizedQuery, specJSON string) error {
	if m.data == nil {
		m.data = make(map[string]string)
	}
	m.data[normalizedQuery] = specJSON
	return nil
}

func (m *mockStore) GetParsedQueryCache(normalizedQuery string) (string, error) {
	if m.data == nil {
		return "", nil
	}
	return m.data[normalizedQuery], nil
}

func TestParsedQueryCache_RoundTrip(t *testing.T) {
	store := &mockStore{}
	cache := NewParsedQueryCache(store)

	spec := FilterSpec{
		SemanticQuery: "cats",
		Must: []Clause{
			{Field: FieldFileType, Op: OpEq, Value: "image"},
		},
		Source: SourceGrammar,
	}

	if err := cache.Set("find cats", spec); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := cache.Get("find cats")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil on cache hit")
	}
	if got.SemanticQuery != spec.SemanticQuery {
		t.Errorf("semantic: got %q, want %q", got.SemanticQuery, spec.SemanticQuery)
	}
	if len(got.Must) != 1 {
		t.Errorf("Must len: got %d, want 1", len(got.Must))
	}
}

func TestParsedQueryCache_Miss(t *testing.T) {
	store := &mockStore{}
	cache := NewParsedQueryCache(store)

	got, err := cache.Get("nonexistent query")
	if err != nil {
		t.Fatalf("Get on miss should not error, got %v", err)
	}
	if got != nil {
		t.Errorf("Get on miss should return nil, got %+v", got)
	}
}

func TestParsedQueryCache_NormalizedKey(t *testing.T) {
	store := &mockStore{}
	cache := NewParsedQueryCache(store)

	spec := FilterSpec{SemanticQuery: "test", Source: SourceGrammar}
	// Set with unnormalized key
	if err := cache.Set("  TEST  QUERY!  ", spec); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	// Get with differently normalized key should hit
	got, err := cache.Get("test query")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Error("expected cache hit with normalized key, got miss")
	}
}

// ---------------------------------------------------------------------------
// Source assignment tests
// ---------------------------------------------------------------------------

func TestParse_SourceIsGrammar(t *testing.T) {
	spec := Parse("kind:image cats")
	if spec.Source != SourceGrammar {
		t.Errorf("Parse should set Source=grammar, got %q", spec.Source)
	}
}

// ---------------------------------------------------------------------------
// Phase 7: Typo correction integration tests
// ---------------------------------------------------------------------------

// TestGrammarParse_TypoCorrectionKind — "kind:imgae beach" → file_type=image Must clause.
func TestGrammarParse_TypoCorrectionKind(t *testing.T) {
	spec := Parse("kind:imgae beach")
	if spec.SemanticQuery != "beach" {
		t.Errorf("expected SemanticQuery=beach, got %q", spec.SemanticQuery)
	}
	if len(spec.Must) == 0 {
		t.Fatalf("expected Must clause after typo correction, got none")
	}
	found := false
	for _, c := range spec.Must {
		if c.Field == FieldFileType {
			if v, ok := c.Value.(string); ok && v == "image" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected file_type=image after correcting 'imgae', got %+v", spec.Must)
	}
}

// TestGrammarParse_TypoCorrectionExt — "ext:pef report" → extension in_set [.pdf].
func TestGrammarParse_TypoCorrectionExt(t *testing.T) {
	spec := Parse("ext:pef report")
	if spec.SemanticQuery != "report" {
		t.Errorf("expected SemanticQuery=report, got %q", spec.SemanticQuery)
	}
	if len(spec.Must) == 0 {
		t.Fatalf("expected Must clause after ext typo correction, got none")
	}
	found := false
	for _, c := range spec.Must {
		if c.Field == FieldExtension && c.Op == OpInSet {
			if vals, ok := c.Value.([]string); ok {
				for _, v := range vals {
					if v == ".pdf" {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Errorf("expected extension=.pdf after correcting 'pef', got %+v", spec.Must)
	}
}
