package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/eval"
	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
)

// ─── flag parsing ──────────────────────────────────────────────────────────

func TestParseEvalFlags_mode_validValues(t *testing.T) {
	for _, tc := range []struct {
		flag string
		want eval.Mode
	}{
		{"lexical", eval.ModeLexical},
		{"vector", eval.ModeVector},
		{"hybrid", eval.ModeHybrid},
	} {
		cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--mode=" + tc.flag}, fakeHome)
		if err != nil {
			t.Fatalf("parseEvalFlags --mode=%s: %v", tc.flag, err)
		}
		if cfg.mode != tc.want {
			t.Errorf("--mode=%s: cfg.mode = %q, want %q", tc.flag, cfg.mode, tc.want)
		}
	}
}

func TestParseEvalFlags_mode_invalidValueErrors(t *testing.T) {
	_, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--mode=bm25only"}, fakeHome)
	if err == nil {
		t.Fatal("expected error for invalid --mode")
	}
	if !strings.Contains(err.Error(), "bm25only") {
		t.Errorf("error %q should mention the invalid mode value", err.Error())
	}
}

func TestParseEvalFlags_mode_defaultsToLexical(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.mode != eval.ModeLexical {
		t.Errorf("cfg.mode = %q, want lexical", cfg.mode)
	}
}

func TestParseEvalFlags_mode_embeddingsFlagImpliesHybrid(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--embeddings"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.mode != eval.ModeHybrid {
		t.Errorf("--embeddings alone: cfg.mode = %q, want hybrid", cfg.mode)
	}
}

func TestParseEvalFlags_mode_explicitModeOverridesEmbeddingsFlag(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--embeddings", "--mode=lexical"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.mode != eval.ModeLexical {
		t.Errorf("explicit --mode=lexical should win over --embeddings: cfg.mode = %q", cfg.mode)
	}
}

func TestParseEvalFlags_vectorWeight_defaultsToHybridDefault(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.vectorWeight != hybrid.DefaultVectorWeight {
		t.Errorf("cfg.vectorWeight = %v, want %v", cfg.vectorWeight, hybrid.DefaultVectorWeight)
	}
}

func TestParseEvalFlags_vectorWeight_custom(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--vector-weight=5.5"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.vectorWeight != 5.5 {
		t.Errorf("cfg.vectorWeight = %v, want 5.5", cfg.vectorWeight)
	}
}

func TestParseEvalFlags_embedTimeout_defaultsToEvalDefault(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.embedTimeout != eval.DefaultEmbedTimeout {
		t.Errorf("cfg.embedTimeout = %v, want %v", cfg.embedTimeout, eval.DefaultEmbedTimeout)
	}
}

func TestParseEvalFlags_allProjects_clearsDefaultProject(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--all-projects"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.project != "" {
		t.Errorf("--all-projects: cfg.project = %q, want empty", cfg.project)
	}
}

func TestParseEvalFlags_allProjects_overridesExplicitProject(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--project=myproj", "--all-projects"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.project != "" {
		t.Errorf("--project=myproj --all-projects: cfg.project = %q, want empty (--all-projects wins)", cfg.project)
	}
}

func TestParseEvalFlags_emptyProjectFlag_meansNoFilter(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--project="}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.project != "" {
		t.Errorf("--project=\"\": cfg.project = %q, want empty", cfg.project)
	}
}

func TestParseEvalFlags_jsonPath(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml", "--json=/tmp/out.json"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.jsonPath != "/tmp/out.json" {
		t.Errorf("cfg.jsonPath = %q, want /tmp/out.json", cfg.jsonPath)
	}
}

// ─── --mode vector ─────────────────────────────────────────────────────────

func TestRunEval_ModeVector_OllamaUnavailable_ErrorsClearly(t *testing.T) {
	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--data-dir=" + t.TempDir(),
		"--mode=vector",
		"--ollama-url=http://127.0.0.1:1", // nothing listens here
	}, &sb)
	if err == nil {
		t.Fatal("--mode vector with unreachable Ollama: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("error %q should mention Ollama being unavailable", err.Error())
	}
}

func TestRunEval_ModeHybrid_OllamaUnavailable_ErrorsClearly(t *testing.T) {
	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--data-dir=" + t.TempDir(),
		"--mode=hybrid",
		"--ollama-url=http://127.0.0.1:1",
	}, &sb)
	if err == nil {
		t.Fatal("--mode hybrid with unreachable Ollama: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("error %q should mention Ollama being unavailable", err.Error())
	}
}

// fakeOllamaServer builds an httptest server answering /api/tags (Ping) and
// /api/embeddings with a fixed vector, so --mode vector/hybrid can run
// end-to-end without a real Ollama.
func fakeOllamaServer(vec []float32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
		case "/api/embeddings":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"embedding": vec})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunEval_ModeVector_WithFakeOllama_Succeeds(t *testing.T) {
	vec := []float32{0.5, 0.5, 0.1, 0.2}
	srv := fakeOllamaServer(vec)
	defer srv.Close()

	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=vector-mode-test",
		"--data-dir=" + t.TempDir(),
		"--mode=vector",
		"--ollama-url=" + srv.URL,
		"--model=fake-model",
	}, &sb)
	if err != nil {
		t.Fatalf("runEval --mode vector: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "mode=vector") {
		t.Errorf("output should mention mode=vector; got:\n%s", out)
	}
	if !strings.Contains(out, "MeanMRR") {
		t.Errorf("output missing MeanMRR; got:\n%s", out)
	}
}

// TestRunEval_ModeHybrid_PerQueryFallbackWarning verifies that a per-query
// embed failure (as opposed to the upfront Ping check) surfaces as a
// warning in the text output AND is listed in the JSON aggregate, without
// failing the whole run — this is hybrid mode's documented graceful
// degradation (see internal/hybrid.Searcher.Search).
func TestRunEval_ModeHybrid_PerQueryFallbackWarning(t *testing.T) {
	vec := []float32{0.5, 0.5, 0.1, 0.2}
	const failingQuery = "BM25 scoring" // matches golden.yaml Q03

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
		case "/api/embeddings":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["prompt"] == failingQuery {
				http.Error(w, "simulated embed failure", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"embedding": vec})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	jsonPath := filepath.Join(t.TempDir(), "report.json")

	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=hybrid-fallback-test",
		"--data-dir=" + t.TempDir(),
		"--mode=hybrid",
		"--ollama-url=" + srv.URL,
		"--model=fake-model",
		"--json=" + jsonPath,
	}, &sb)
	if err != nil {
		t.Fatalf("runEval: %v", err)
	}

	out := sb.String()
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "Q03") {
		t.Errorf("expected a WARNING mentioning Q03 in text output; got:\n%s", out)
	}

	data, readErr := os.ReadFile(jsonPath)
	if readErr != nil {
		t.Fatalf("read JSON file: %v", readErr)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	agg := parsed["aggregate"].(map[string]any)
	warnings, ok := agg["hybrid_fallback_warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("aggregate.hybrid_fallback_warnings missing or empty: %+v", agg)
	}
	found := false
	for _, w := range warnings {
		if w == "Q03" {
			found = true
		}
	}
	if !found {
		t.Errorf("hybrid_fallback_warnings = %v, want to contain Q03", warnings)
	}
}

func TestRunEval_ModeHybrid_ReportsHybridRan(t *testing.T) {
	vec := []float32{0.5, 0.5, 0.1, 0.2}
	srv := fakeOllamaServer(vec)
	defer srv.Close()

	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=hybrid-mode-test",
		"--data-dir=" + t.TempDir(),
		"--mode=hybrid",
		"--ollama-url=" + srv.URL,
		"--model=fake-model",
	}, &sb)
	if err != nil {
		t.Fatalf("runEval --mode hybrid: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "HybridRan:") {
		t.Errorf("hybrid mode output should include HybridRan; got:\n%s", out)
	}
}

// ─── --json ────────────────────────────────────────────────────────────────

func TestRunEval_JSONFile_ContainsExpectedFields(t *testing.T) {
	jsonPath := filepath.Join(t.TempDir(), "report.json")

	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=json-file-test",
		"--k=5",
		"--data-dir=" + t.TempDir(),
		"--json=" + jsonPath,
	}, &sb)
	if err != nil {
		t.Fatalf("runEval: %v", err)
	}

	// The text table must still be present alongside the file.
	if !strings.Contains(sb.String(), "MeanMRR") {
		t.Error("expected text table on stdout alongside --json=<path>")
	}

	data, readErr := os.ReadFile(jsonPath)
	if readErr != nil {
		t.Fatalf("read JSON file: %v", readErr)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON in report file: %v\n%s", err, data)
	}

	if parsed["mode"] != "lexical" {
		t.Errorf("mode = %v, want lexical", parsed["mode"])
	}
	if parsed["k"] != float64(5) {
		t.Errorf("k = %v, want 5", parsed["k"])
	}
	agg, ok := parsed["aggregate"].(map[string]any)
	if !ok {
		t.Fatalf("aggregate field missing or wrong type: %+v", parsed)
	}
	for _, key := range []string{"mean_mrr", "hit_at_1", "mean_p_at_k", "mean_recall_at_k", "mean_recall_at_10", "mean_ndcg_at_10", "latency_ms", "hybrid_ran", "queries"} {
		if _, ok := agg[key]; !ok {
			t.Errorf("aggregate missing key %q: %+v", key, agg)
		}
	}
	perQuery, ok := parsed["per_query"].([]any)
	if !ok || len(perQuery) == 0 {
		t.Fatalf("per_query missing or empty: %+v", parsed["per_query"])
	}
	first, ok := perQuery[0].(map[string]any)
	if !ok {
		t.Fatalf("per_query[0] wrong type: %+v", perQuery[0])
	}
	for _, key := range []string{"id", "query", "project", "expected", "expect_fail", "mrr", "hit1", "p_at_k", "recall_at_k", "recall_at_10", "ndcg_at_10", "latency_ms", "hybrid", "fuzzy", "top"} {
		if _, ok := first[key]; !ok {
			t.Errorf("per_query[0] missing key %q: %+v", key, first)
		}
	}
}

func TestRunEval_JSONStdout_SuppressesTextTable(t *testing.T) {
	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=json-stdout-test",
		"--data-dir=" + t.TempDir(),
		"--json=-",
	}, &sb)
	if err != nil {
		t.Fatalf("runEval: %v", err)
	}
	out := sb.String()
	if strings.Contains(out, "MeanMRR:") {
		t.Errorf("--json=- should suppress the text table; got:\n%s", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("--json=- output is not valid JSON: %v\n%s", err, out)
	}
}
