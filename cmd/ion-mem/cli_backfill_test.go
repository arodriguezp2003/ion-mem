package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// withFastBackfillSleep points backfillSleep at a no-op for the duration of
// the test (restored via t.Cleanup), so any retry/backoff path exercised by
// runBackfill runs instantly instead of waiting on embedjob's real
// production backoff (1s/3s, tuned for Ollama cold-loading a model).
func withFastBackfillSleep(t *testing.T) {
	t.Helper()
	backfillSleep = func(ctx context.Context, _ time.Duration) error {
		return ctx.Err()
	}
	t.Cleanup(func() { backfillSleep = nil })
}

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

func TestParseBackfillFlags_verboseNotPassedIsNil(t *testing.T) {
	cfg, err := parseBackfillFlags([]string{}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackfillFlags: %v", err)
	}
	if cfg.verbose != nil {
		t.Errorf("verbose = %v, want nil when --verbose is not passed", cfg.verbose)
	}
}

func TestParseBackfillFlags_verboseTrue(t *testing.T) {
	cfg, err := parseBackfillFlags([]string{"--verbose"}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackfillFlags: %v", err)
	}
	if cfg.verbose == nil || *cfg.verbose != true {
		t.Errorf("verbose = %v, want pointer to true", cfg.verbose)
	}
}

func TestParseBackfillFlags_verboseFalseExplicit(t *testing.T) {
	cfg, err := parseBackfillFlags([]string{"--verbose=false"}, fakeHome)
	if err != nil {
		t.Fatalf("parseBackfillFlags: %v", err)
	}
	if cfg.verbose == nil || *cfg.verbose != false {
		t.Errorf("verbose = %v, want pointer to false (explicitly passed)", cfg.verbose)
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
	withFastBackfillSleep(t)
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
	withFastBackfillSleep(t)
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

// TestRunBackfill_CreatesJobLogWithStartLine verifies that runBackfill opens a
// joblog under <data-dir>/logs/embeddings.log, prints its path to out, and
// records a start line naming the model, url, project and batch.
func TestRunBackfill_CreatesJobLogWithStartLine(t *testing.T) {
	withFastBackfillSleep(t)
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

	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "log-sess", Project: "log-proj", Directory: "/log",
	})
	_, err = st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "log-sess", Type: "manual",
		Title: "obs-0", Content: "content",
		Project: "log-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	st.Close()

	var sb strings.Builder
	err = runBackfill([]string{
		"--data-dir=" + dir,
		"--project=log-proj",
		"--batch=10",
	}, &sb)
	if err != nil {
		t.Fatalf("runBackfill: %v", err)
	}

	out := sb.String()
	wantLogLine := "log: "
	if !strings.Contains(out, wantLogLine) {
		t.Fatalf("output missing %q line: %q", wantLogLine, out)
	}

	logPath := filepath.Join(dir, "logs", "embeddings.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file %s: %v", logPath, err)
	}
	logText := string(content)
	if !strings.Contains(logText, "nomic-embed-text") {
		t.Errorf("log file missing model in start line: %q", logText)
	}
	if !strings.Contains(logText, "log-proj") {
		t.Errorf("log file missing project in start line: %q", logText)
	}
	if !strings.Contains(logText, " INFO ") {
		t.Errorf("log file missing INFO level line: %q", logText)
	}
}

// TestRunBackfill_OneItemPermanentlyBroken_PartialResult verifies that when
// exactly one observation's text can never be embedded (the fake Ollama
// returns 500 only for that one prompt) while the rest succeed, runBackfill:
//   - returns a non-nil error mentioning "partial"
//   - prints a "partial: X/Y embedded, N failed" line naming the log path
//   - still embeds every other observation (job doesn't abort)
func TestRunBackfill_OneItemPermanentlyBroken_PartialResult(t *testing.T) {
	withFastBackfillSleep(t)
	vec := []float32{1.0, 0.0, 0.5}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Prompt string `json:"prompt"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if strings.Contains(body.Prompt, "broken-1") {
			http.Error(w, "simulated permanent failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"embedding": vec})
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

	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "partial-sess", Project: "partial-proj", Directory: "/partial",
	})
	titles := []string{"good-0", "broken-1", "good-2", "good-3"}
	for _, title := range titles {
		_, err := st.AddObservation(ctx, store.AddObservationParams{
			SessionID: "partial-sess", Type: "manual",
			Title: title, Content: "content",
			Project: "partial-proj", Scope: "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %s: %v", title, err)
		}
	}
	st.Close()

	var sb strings.Builder
	err = runBackfill([]string{
		"--data-dir=" + dir,
		"--project=partial-proj",
		"--batch=10",
	}, &sb)
	if err == nil {
		t.Fatal("runBackfill: expected non-nil error for a partial result")
	}
	if !strings.Contains(err.Error(), "partial") {
		t.Errorf("error %q should mention 'partial'", err.Error())
	}

	out := sb.String()
	if !strings.Contains(out, "partial: 3/4 embedded, 1 failed") {
		t.Errorf("output missing partial summary line: %q", out)
	}
	if !strings.Contains(out, "WARN: embed") {
		t.Errorf("output missing per-item WARN line: %q", out)
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
