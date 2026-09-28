package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunEval_ModeVector_PerQueryEmbedError_DoesNotAbortRun exercises the
// per-query error path end-to-end: --mode vector has no fallback, so a
// single query whose text fails to embed becomes a genuine per-query
// SearchFn error. The run must still complete (not abort), the failing
// query must be reported (WARN line, ERR marker in the table, and
// errors/error_ids in the JSON), and every other query must still be scored
// normally.
func TestRunEval_ModeVector_PerQueryEmbedError_DoesNotAbortRun(t *testing.T) {
	vec := []float32{0.3, 0.4, 0.1, 0.2}
	const failingQuery = "BM25 scoring" // golden.yaml Q03

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
		"--project=vector-per-query-error-test",
		"--data-dir=" + t.TempDir(),
		"--mode=vector",
		"--ollama-url=" + srv.URL,
		"--model=fake-model",
		"--json=" + jsonPath,
	}, &sb)
	if err != nil {
		t.Fatalf("runEval: a single failing query must not abort the run: %v", err)
	}

	out := sb.String()
	if !strings.Contains(out, "WARN: query Q03 failed:") {
		t.Errorf("expected a WARN line for Q03 in text output; got:\n%s", out)
	}
	if !strings.Contains(out, "ERR:") {
		t.Errorf("expected an ERR marker in the per-query table; got:\n%s", out)
	}
	if !strings.Contains(out, "MeanMRR") {
		t.Errorf("run should still complete and print aggregate metrics; got:\n%s", out)
	}

	data, readErr := os.ReadFile(jsonPath)
	if readErr != nil {
		t.Fatalf("read JSON file: %v", readErr)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	agg := parsed["aggregate"].(map[string]any)
	if agg["errors"] != float64(1) {
		t.Errorf("aggregate.errors = %v, want 1", agg["errors"])
	}
	errorIDs, _ := agg["error_ids"].([]any)
	if len(errorIDs) != 1 || errorIDs[0] != "Q03" {
		t.Errorf("aggregate.error_ids = %v, want [Q03]", errorIDs)
	}

	var foundErrEntry bool
	for _, raw := range parsed["per_query"].([]any) {
		qr := raw.(map[string]any)
		if qr["id"] == "Q03" {
			foundErrEntry = true
			if qr["err"] == nil || qr["err"] == "" {
				t.Errorf("Q03 per_query entry should have a non-empty err field: %+v", qr)
			}
			if qr["mrr"] != float64(0) {
				t.Errorf("Q03 mrr should be 0 on error, got %v", qr["mrr"])
			}
		} else {
			// Every other query should have scored normally (no err field).
			if errVal, ok := qr["err"]; ok && errVal != "" {
				t.Errorf("query %v should not have an err, got %v", qr["id"], errVal)
			}
		}
	}
	if !foundErrEntry {
		t.Error("expected a per_query entry for Q03")
	}
}
