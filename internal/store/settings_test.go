package store_test

import (
	"context"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// ─── Setting constants ────────────────────────────────────────────────────────

func TestSettingKeys_DefaultValues(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	// embeddings.enabled defaults to "false" when not set
	got := s.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false")
	if got != "false" {
		t.Errorf("default for %q = %q, want %q", store.SettingEmbeddingsEnabled, got, "false")
	}

	// embeddings.ollama_url defaults to provided default
	got2 := s.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
	if got2 != "http://localhost:11434" {
		t.Errorf("default for %q = %q, want %q", store.SettingOllamaURL, got2, "http://localhost:11434")
	}

	// embeddings.model defaults to provided default
	got3 := s.SettingOrDefault(ctx, store.SettingEmbeddingsModel, "nomic-embed-text")
	if got3 != "nomic-embed-text" {
		t.Errorf("default for %q = %q, want %q", store.SettingEmbeddingsModel, got3, "nomic-embed-text")
	}

	// log.verbose defaults to "false" when not set
	got4 := s.SettingOrDefault(ctx, store.SettingLogVerbose, "false")
	if got4 != "false" {
		t.Errorf("default for %q = %q, want %q", store.SettingLogVerbose, got4, "false")
	}
}

func TestSettingLogVerbose_KeyName(t *testing.T) {
	if store.SettingLogVerbose != "log.verbose" {
		t.Errorf("SettingLogVerbose = %q, want %q", store.SettingLogVerbose, "log.verbose")
	}
}

func TestSettingSearchMode_KeyAndDefault(t *testing.T) {
	if store.SettingSearchMode != "search.mode" {
		t.Errorf("SettingSearchMode = %q, want %q", store.SettingSearchMode, "search.mode")
	}
	if store.DefaultSearchMode != "vector" {
		t.Errorf("DefaultSearchMode = %q, want %q", store.DefaultSearchMode, "vector")
	}

	s := mustOpen(t)
	ctx := context.Background()
	got := s.SettingOrDefault(ctx, store.SettingSearchMode, store.DefaultSearchMode)
	if got != "vector" {
		t.Errorf("default for %q = %q, want %q", store.SettingSearchMode, got, "vector")
	}
}

func TestSettingOllamaAutostart_KeyAndDefault(t *testing.T) {
	if store.SettingOllamaAutostart != "ollama.autostart" {
		t.Errorf("SettingOllamaAutostart = %q, want %q", store.SettingOllamaAutostart, "ollama.autostart")
	}
	if store.DefaultOllamaAutostart != "false" {
		t.Errorf("DefaultOllamaAutostart = %q, want %q", store.DefaultOllamaAutostart, "false")
	}

	s := mustOpen(t)
	ctx := context.Background()
	got := s.SettingOrDefault(ctx, store.SettingOllamaAutostart, store.DefaultOllamaAutostart)
	if got != "false" {
		t.Errorf("default for %q = %q, want %q", store.SettingOllamaAutostart, got, "false")
	}
}

// ─── GetSetting / SetSetting CRUD ────────────────────────────────────────────

func TestSettings_MissingKeyReturnsFalse(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	_, ok, err := s.GetSetting(ctx, "nonexistent.key")
	if err != nil {
		t.Fatalf("GetSetting unexpected error: %v", err)
	}
	if ok {
		t.Error("GetSetting with missing key should return ok=false")
	}
}

func TestSettings_SetAndGetRoundtrip(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, ok, err := s.GetSetting(ctx, store.SettingEmbeddingsEnabled)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok {
		t.Fatal("GetSetting: expected ok=true after SetSetting")
	}
	if val != "true" {
		t.Errorf("GetSetting = %q, want %q", val, "true")
	}
}

func TestSettings_UpsertOverwritesPreviousValue(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.SetSetting(ctx, store.SettingOllamaURL, "http://localhost:11434"); err != nil {
		t.Fatalf("first SetSetting: %v", err)
	}
	if err := s.SetSetting(ctx, store.SettingOllamaURL, "http://remote:11434"); err != nil {
		t.Fatalf("second SetSetting: %v", err)
	}

	val, ok, err := s.GetSetting(ctx, store.SettingOllamaURL)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok {
		t.Fatal("GetSetting: expected ok=true")
	}
	if val != "http://remote:11434" {
		t.Errorf("after upsert, GetSetting = %q, want %q", val, "http://remote:11434")
	}
}

func TestSettings_SettingOrDefaultReturnsStoredValue(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.SetSetting(ctx, store.SettingEmbeddingsModel, "mxbai-embed-large"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	got := s.SettingOrDefault(ctx, store.SettingEmbeddingsModel, "nomic-embed-text")
	if got != "mxbai-embed-large" {
		t.Errorf("SettingOrDefault = %q, want %q (stored value should win)", got, "mxbai-embed-large")
	}
}

// ─── Migration 0005 idempotency ───────────────────────────────────────────────

func TestMigration0005_CreatesSettingsTable(t *testing.T) {
	s := mustOpen(t)

	v := s.SchemaVersion()
	if v < 5 {
		t.Fatalf("expected SchemaVersion >= 5, got %d", v)
	}

	tables := sqliteMasterNames(t, s, "table")
	if !contains(tables, "settings") {
		t.Fatalf("expected settings table in sqlite_master, got: %v", tables)
	}
}

func TestMigration0005_Idempotent(t *testing.T) {
	dir := t.TempDir()

	s1, err := store.Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	v1 := s1.SchemaVersion()
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	v2 := s2.SchemaVersion()
	if v2 != v1 {
		t.Fatalf("SchemaVersion changed on second open: %d → %d", v1, v2)
	}

	var rowCount int
	if err := s2.DB().QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&rowCount); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if rowCount != v2 {
		t.Fatalf("expected %d rows in schema_version, got %d (duplicate migration?)", v2, rowCount)
	}
}
