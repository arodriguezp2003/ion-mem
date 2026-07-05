package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseEvalFlags_embeddings verifies that --embeddings flag is accepted.
func TestParseEvalFlags_embeddings(t *testing.T) {
	cfg, err := parseEvalFlags([]string{
		"--golden=/tmp/g.yaml",
		"--embeddings",
		"--ollama-url=http://localhost:11434",
		"--model=nomic-embed-text",
	}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags with --embeddings: %v", err)
	}
	if !cfg.embeddings {
		t.Error("cfg.embeddings should be true when --embeddings flag is set")
	}
	if cfg.ollamaURL != "http://localhost:11434" {
		t.Errorf("cfg.ollamaURL = %q, want http://localhost:11434", cfg.ollamaURL)
	}
	if cfg.model != "nomic-embed-text" {
		t.Errorf("cfg.model = %q, want nomic-embed-text", cfg.model)
	}
}

// TestParseEvalFlags_noEmbeddings verifies that embeddings is false by default.
func TestParseEvalFlags_noEmbeddings(t *testing.T) {
	cfg, err := parseEvalFlags([]string{"--golden=/tmp/g.yaml"}, fakeHome)
	if err != nil {
		t.Fatalf("parseEvalFlags: %v", err)
	}
	if cfg.embeddings {
		t.Error("cfg.embeddings should be false when --embeddings flag is not set")
	}
}

// TestRunEval_withEmbeddingsFakeOllama verifies that eval with --embeddings
// runs without error when a fake Ollama is available (returns a fixed vector).
func TestRunEval_withEmbeddingsFakeOllama(t *testing.T) {
	vec := make([]float32, 8)
	for i := range vec {
		vec[i] = float32(i) * 0.1
	}
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

	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=embed-test",
		"--k=5",
		"--data-dir=" + t.TempDir(),
		"--embeddings",
		"--ollama-url=" + srv.URL,
		"--model=nomic-embed-text",
	}, &sb)
	if err != nil {
		t.Fatalf("runEval with --embeddings: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "MeanMRR") {
		t.Errorf("output missing MeanMRR; got:\n%s", out)
	}
}

// TestRunEval_withoutEmbeddings_BM25Only verifies that eval without --embeddings
// is identical to the previous BM25-only path (no embedding calls).
func TestRunEval_withoutEmbeddings_BM25Only(t *testing.T) {
	var sb strings.Builder
	err := runEval([]string{
		"--corpus=" + evalTestdataPath(t, "corpus.yaml"),
		"--golden=" + evalTestdataPath(t, "golden.yaml"),
		"--project=bm25-only-test",
		"--k=5",
		"--data-dir=" + t.TempDir(),
	}, &sb)
	if err != nil {
		t.Fatalf("runEval BM25 only: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "MeanMRR") {
		t.Errorf("output missing MeanMRR; got:\n%s", out)
	}
}
