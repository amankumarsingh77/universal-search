package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// REF-001: Resolve picks platform-specific config path.
func TestResolve_Platform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", `C:\FakeAppData`)
		got, err := Resolve()
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		want := filepath.Join(`C:\FakeAppData`, "universal-search", "config.toml")
		if got != want {
			t.Fatalf("Resolve() = %q, want %q", got, want)
		}
		return
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	got, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(fakeHome, ".config", "universal-search", "config.toml")
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

// REF-002: Missing file returns defaults, no warnings, no file created.
func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %+v", warnings)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Indexing.Workers != DefaultConfig().Indexing.Workers {
		t.Fatalf("expected default workers, got %d", cfg.Indexing.Workers)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no file to exist at %s, err=%v", path, err)
	}
}

// REF-003: Partial file merges over defaults.
func TestLoad_PartialOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "[indexing]\nworkers = 8\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if cfg.Indexing.Workers != 8 {
		t.Fatalf("Indexing.Workers = %d, want 8", cfg.Indexing.Workers)
	}
	def := DefaultConfig()
	if cfg.Indexing.SaveEveryN != def.Indexing.SaveEveryN {
		t.Fatalf("SaveEveryN merged wrong: got %d want %d", cfg.Indexing.SaveEveryN, def.Indexing.SaveEveryN)
	}
	if cfg.App.ShutdownTimeoutMs != def.App.ShutdownTimeoutMs {
		t.Fatalf("App.ShutdownTimeoutMs merged wrong: got %d want %d", cfg.App.ShutdownTimeoutMs, def.App.ShutdownTimeoutMs)
	}
	if cfg.Embedder.Model != def.Embedder.Model {
		t.Fatalf("Embedder.Model merged wrong: got %q want %q", cfg.Embedder.Model, def.Embedder.Model)
	}
	if cfg.HNSW.M != def.HNSW.M {
		t.Fatalf("HNSW.M merged wrong: got %d want %d", cfg.HNSW.M, def.HNSW.M)
	}
}

// REF-004: Unknown top-level keys produce warnings, not errors.
func TestLoad_UnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "[unknown]\nfoo = 1\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning for unknown key")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w.Key, "unknown") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings do not mention 'unknown': %+v", warnings)
	}
	def := DefaultConfig()
	if cfg.Indexing.Workers != def.Indexing.Workers {
		t.Fatalf("config should equal defaults when only unknown keys supplied")
	}
}

// REF-005: Malformed TOML produces a wrapped error with file path.
func TestLoad_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[[["), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for malformed TOML")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error message should contain file path %q; got %v", path, err)
	}
}

// REF-006: Schema version migrator runs and rewrites the file.
func TestLoad_SchemaMigration(t *testing.T) {
	prev := snapshotMigrators()
	t.Cleanup(func() { restoreMigrators(prev) })
	clearMigrators()

	RegisterMigrator(1, func(doc map[string]any) error {
		if v, ok := doc["foo"]; ok {
			doc["bar"] = v
			delete(doc, "foo")
		}
		return nil
	})
	prevCurrent := CurrentSchemaVersion
	setCurrentSchemaVersion(2)
	t.Cleanup(func() { setCurrentSchemaVersion(prevCurrent) })

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "schema_version = 1\nfoo = \"hello\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "schema_version = 2") {
		t.Fatalf("file should be rewritten with schema_version = 2, got:\n%s", content)
	}
	if !strings.Contains(content, "bar") {
		t.Fatalf("file should contain migrated 'bar' field, got:\n%s", content)
	}
	if strings.Contains(content, "foo =") {
		t.Fatalf("file should no longer contain 'foo =' field, got:\n%s", content)
	}
}

// REF-007: DefaultTOML() round-trips to DefaultConfig().
func TestDefaultTOML_ParsesBackToDefaults(t *testing.T) {
	s := DefaultTOML()
	if s == "" {
		t.Fatal("DefaultTOML() returned empty string")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load DefaultTOML: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("DefaultTOML produced warnings: %+v", warnings)
	}
	def := DefaultConfig()
	if cfg.App.ShutdownTimeoutMs != def.App.ShutdownTimeoutMs {
		t.Errorf("App.ShutdownTimeoutMs: got %d want %d", cfg.App.ShutdownTimeoutMs, def.App.ShutdownTimeoutMs)
	}
	if cfg.Indexing.Workers != def.Indexing.Workers {
		t.Errorf("Indexing.Workers: got %d want %d", cfg.Indexing.Workers, def.Indexing.Workers)
	}
	if cfg.Embedder.Model != def.Embedder.Model {
		t.Errorf("Embedder.Model: got %q want %q", cfg.Embedder.Model, def.Embedder.Model)
	}
	if cfg.Embedder.Dimensions != def.Embedder.Dimensions {
		t.Errorf("Embedder.Dimensions: got %d want %d", cfg.Embedder.Dimensions, def.Embedder.Dimensions)
	}
	if cfg.HNSW.M != def.HNSW.M {
		t.Errorf("HNSW.M: got %d want %d", cfg.HNSW.M, def.HNSW.M)
	}
	if cfg.HNSW.Ml != def.HNSW.Ml {
		t.Errorf("HNSW.Ml: got %v want %v", cfg.HNSW.Ml, def.HNSW.Ml)
	}
	if cfg.HNSW.EfSearch != def.HNSW.EfSearch {
		t.Errorf("HNSW.EfSearch: got %d want %d", cfg.HNSW.EfSearch, def.HNSW.EfSearch)
	}
	if cfg.Search.BruteForceThreshold != def.Search.BruteForceThreshold {
		t.Errorf("Search.BruteForceThreshold: got %d want %d", cfg.Search.BruteForceThreshold, def.Search.BruteForceThreshold)
	}
	if cfg.Search.RecencyBoostMultiplier != def.Search.RecencyBoostMultiplier {
		t.Errorf("Search.RecencyBoostMultiplier: got %v want %v", cfg.Search.RecencyBoostMultiplier, def.Search.RecencyBoostMultiplier)
	}
	if cfg.Query.LLMModel != def.Query.LLMModel {
		t.Errorf("Query.LLMModel: got %q want %q", cfg.Query.LLMModel, def.Query.LLMModel)
	}
	if cfg.Query.TriggerMinTokens != def.Query.TriggerMinTokens {
		t.Errorf("Query.TriggerMinTokens: got %d want %d", cfg.Query.TriggerMinTokens, def.Query.TriggerMinTokens)
	}
	if cfg.SchemaVersion != def.SchemaVersion {
		t.Errorf("SchemaVersion: got %d want %d", cfg.SchemaVersion, def.SchemaVersion)
	}
}

// REF-072: DefaultConfig().App.ShutdownTimeoutMs == 15000.
func TestDefaultConfig_ShutdownTimeoutIs15000(t *testing.T) {
	if got := DefaultConfig().App.ShutdownTimeoutMs; got != 15000 {
		t.Fatalf("DefaultConfig().App.ShutdownTimeoutMs = %d, want 15000", got)
	}
}

// TestPartialConfigMerge — REF-084 named alias. A TOML file that only sets a
// subset of keys merges over DefaultConfig() for everything else. Delegates to
// TestLoad_PartialOverride.
func TestPartialConfigMerge(t *testing.T) { TestLoad_PartialOverride(t) }

// TestCorruptConfigFailsLoud — REF-084: when the config file is malformed,
// Load returns a wrapped error that names the file path. The original REF-084
// draft named this test CorruptConfigFallback, but REF-005 specifies fail-loud
// behavior (do not silently fall back to defaults) — the name reflects the
// actual contract. Delegates to TestLoad_MalformedTOML.
func TestCorruptConfigFailsLoud(t *testing.T) { TestLoad_MalformedTOML(t) }

// TestCorruptConfigFallback — REF-084 named alias kept for completeness;
// behavior is fail-loud (per REF-005), so this asserts the same contract as
// TestCorruptConfigFailsLoud.
func TestCorruptConfigFallback(t *testing.T) { TestLoad_MalformedTOML(t) }
