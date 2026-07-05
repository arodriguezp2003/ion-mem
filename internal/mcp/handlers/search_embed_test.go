package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

// TestSearch_EmbeddingsDisabled_NoHybridFlag verifies that with embeddings
// disabled the search response does not include hybrid:true.
func TestSearch_EmbeddingsDisabled_NoHybridFlag(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "search-proj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_search", map[string]any{
		"query": "test query",
		"limit": 5,
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("ion_search: status = %v, want ok", env["status"])
	}
	// hybrid flag should be false when embeddings are disabled.
	if hybrid, ok := env["hybrid"]; ok && hybrid == true {
		t.Errorf("ion_search with embeddings disabled: hybrid = %v, want false", hybrid)
	}
}

// TestSearch_EmbeddingsEnabled_FakeOllama_HybridTrue verifies that when
// embeddings are enabled and Ollama is reachable, a search with a seeded
// vector returns hybrid:true.
func TestSearch_EmbeddingsEnabled_FakeOllama_HybridTrue(t *testing.T) {
	queryVec := []float32{1.0, 0.0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]any{"embedding": queryVec}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	st := mustStore(t)
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

	// Seed an observation and plant its embedding so VectorSearch returns it.
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "sch-sess", Project: "search-hybrid", Directory: "/test",
	})
	obs, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "sch-sess", Type: "manual",
		Title: "semantic target", Content: "no lexical overlap with query",
		Project: "search-hybrid", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	// Plant a vector close to the query vector.
	if err := st.UpsertEmbedding(ctx, obs.ID, "nomic-embed-text", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "search-hybrid", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_search", map[string]any{
		"query": "something unrelated",
		"limit": 10,
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("ion_search hybrid: status = %v, want ok", env["status"])
	}

	// hybrid must be true when embedder is active and vector search is used.
	hybrid, ok := env["hybrid"]
	if !ok {
		t.Fatal("ion_search with embeddings enabled: expected 'hybrid' key in response")
	}
	if hybrid != true {
		t.Errorf("ion_search: hybrid = %v, want true when embeddings enabled and vectors seeded", hybrid)
	}
}

// TestSearch_SemanticDocAppearsWithEmbeddings verifies the key hybrid guarantee:
// a doc with no lexical overlap with the query appears in results when its vector
// is close to the query vector.
func TestSearch_SemanticDocAppearsWithEmbeddings(t *testing.T) {
	queryVec := []float32{0.0, 1.0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]any{"embedding": queryVec}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	st := mustStore(t)
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
		ID: "sem-sess", Project: "semantic-proj", Directory: "/test",
	})
	semanticObs, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "sem-sess", Type: "manual",
		Title: "zzz completely unrelated zzz", Content: "xyz abc def ghi",
		Project: "semantic-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation semantic: %v", err)
	}
	// Plant vector identical to query — perfect cosine match.
	if err := st.UpsertEmbedding(ctx, semanticObs.ID, "nomic-embed-text", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "semantic-proj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_search", map[string]any{
		"query": "semantic query with no keyword match",
		"limit": 10,
	})
	env := decodeText(t, res)

	results := env["results"].([]any)
	found := false
	for _, r := range results {
		row := r.(map[string]any)
		if row["id"].(float64) == float64(semanticObs.ID) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ion_search hybrid: semantic doc (ID=%d) not found in results — hybrid fusion failed", semanticObs.ID)
	}
}
