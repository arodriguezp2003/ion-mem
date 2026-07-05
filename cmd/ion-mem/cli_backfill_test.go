package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── flag parsing ─────────────────────────────────────────────────────────────

func TestParseBackfillFlags_defaults(t *testing.T) {
	cfg, err := parseBackfillFlags([]string{}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackfillFlags: %v", err)
	}
	if cfg.batch <= 0 {
		t.Errorf("batch default = %d, want > 0", cfg.batch)
	}
}

func TestParseBackfillFlags_customBatch(t *testing.T) {
	cfg, err := parseBackfillFlags([]string{"--batch=10"}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackfillFlags: %v", err)
	}
	if cfg.batch != 10 {
		t.Errorf("batch = %d, want 10", cfg.batch)
	}
}

func TestParseBackfillFlags_customProject(t *testing.T) {
	cfg, err := parseBackfillFlags([]string{"--project=myproj"}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackfillFlags: %v", err)
	}
	if cfg.project != "myproj" {
		t.Errorf("project = %q, want myproj", cfg.project)
	}
}

// ─── run tests ────────────────────────────────────────────────────────────────

// TestRunBackfill_EmbeddingsNotEnabled verifies that backfill-embeddings errors
// clearly when embeddings.enabled is not set.
func TestRunBackfill_EmbeddingsNotEnabled(t *testing.T) {
	dir := t.TempDir()
	// Open and immediately close to create the DB.
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	st.Close()

	var sb strings.Builder
	err = runBackfill([]string{"--data-dir=" + dir}, &sb)
	if err == nil {
		t.Fatal("runBackfill: expected error when embeddings.enabled is not set")
	}
	if !strings.Contains(err.Error(), "embeddings") {
		t.Errorf("error %q should mention 'embeddings'", err.Error())
	}
}

// TestRunBackfill_WithFakeOllama verifies that backfill-embeddings runs end-to-end
// with a fake Ollama, embeds all seeded observations, and prints progress.
func TestRunBackfill_WithFakeOllama(t *testing.T) {
	vec := []float32{1.0, 0.0, 0.5}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]any{"embedding": vec}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	ctx := context.Background()
	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting enabled: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting url: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting model: %v", err)
	}

	// Seed 3 observations.
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "bf-sess", Project: "bf-proj", Directory: "/bf",
	})
	for i := 0; i < 3; i++ {
		_, err := st.AddObservation(ctx, store.AddObservationParams{
			SessionID: "bf-sess", Type: "manual",
			Title: fmt.Sprintf("obs-%d", i), Content: "content",
			Project: "bf-proj", Scope: "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}
	st.Close()

	var sb strings.Builder
	err = runBackfill([]string{
		"--data-dir=" + dir,
		"--project=bf-proj",
		"--batch=10",
	}, &sb)
	if err != nil {
		t.Fatalf("runBackfill: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "embedded") {
		t.Errorf("backfill output missing 'embedded': %q", out)
	}
}

// TestRunBackfill_OllamaAlways500 verifies that when Ollama always returns HTTP
// 500, backfill-embeddings terminates (does not loop forever) and returns a
// non-nil error that mentions the aborted condition.
func TestRunBackfill_OllamaAlways500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	ctx := context.Background()
	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting enabled: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting url: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting model: %v", err)
	}

	// Seed 3 observations so the first batch (size 2) is a full batch.
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "fail-sess", Project: "fail-proj", Directory: "/fail",
	})
	for i := 0; i < 3; i++ {
		_, err := st.AddObservation(ctx, store.AddObservationParams{
			SessionID: "fail-sess", Type: "manual",
			Title: fmt.Sprintf("fail-obs-%d", i), Content: "content",
			Project: "fail-proj", Scope: "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}
	st.Close()

	var sb strings.Builder
	err = runBackfill([]string{
		"--data-dir=" + dir,
		"--project=fail-proj",
		"--batch=2",
	}, &sb)
	if err == nil {
		t.Fatal("runBackfill: expected non-nil error when Ollama always returns 500")
	}
	if !strings.Contains(err.Error(), "aborted") {
		t.Errorf("error %q should mention 'aborted'", err.Error())
	}
}

// TestUsage_containsBackfill verifies that usage() mentions backfill-embeddings.
func TestUsage_containsBackfill(t *testing.T) {
	u := usage()
	if !strings.Contains(u, "backfill-embeddings") {
		t.Error("usage() does not mention backfill-embeddings command")
	}
}

// TestRouteCommand_backfillEmbeddings_routesToRun verifies that the router
// dispatches the backfill-embeddings command (errors are expected since no DB).
func TestRouteCommand_backfillEmbeddings_routesToRun(t *testing.T) {
	err := routeCommand([]string{"ion-mem", "backfill-embeddings"}, nil)
	// We expect an error (no DB configured) but NOT "unknown command".
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Errorf("backfill-embeddings not routed: %v", err)
	}
}
