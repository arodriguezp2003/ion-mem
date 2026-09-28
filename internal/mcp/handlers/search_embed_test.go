package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/mcp"
	"github.com/arodriguezp2003/ion-mem/internal/project"
	"github.com/arodriguezp2003/ion-mem/internal/store"
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

// TestSearch_EmbeddingsEnabled_FakeOllama_VectorMode verifies that when
// embeddings are enabled and Ollama is reachable, a search with a seeded
// vector uses the default search.mode ("vector"): the response reports
// mode/effective_mode "vector", degraded:false, and — since vector mode
// never runs BM25 fusion — hybrid:false (hybrid:true is reserved for
// search.mode=hybrid; see TestSearch_SearchModeHybrid_FusesResults).
func TestSearch_EmbeddingsEnabled_FakeOllama_VectorMode(t *testing.T) {
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

	if mode, _ := env["mode"].(string); mode != "vector" {
		t.Errorf("ion_search: mode = %v, want %q (default search.mode)", env["mode"], "vector")
	}
	if effMode, _ := env["effective_mode"].(string); effMode != "vector" {
		t.Errorf("ion_search: effective_mode = %v, want %q", env["effective_mode"], "vector")
	}
	if degraded, _ := env["degraded"].(bool); degraded {
		t.Errorf("ion_search: degraded = %v, want false (Ollama reachable, model present)", env["degraded"])
	}
	// hybrid is reserved for search.mode=hybrid's RRF fusion — vector mode
	// never sets it, even with embeddings enabled and a seeded vector.
	if hybrid, ok := env["hybrid"]; ok && hybrid == true {
		t.Errorf("ion_search: hybrid = %v, want false under search.mode=vector", hybrid)
	}
}

// TestSearch_SearchModeHybrid_FusesResults verifies that setting
// search.mode=hybrid switches ion_search to the AND-only fusion path and
// reports hybrid:true when the fused list is non-empty.
func TestSearch_SearchModeHybrid_FusesResults(t *testing.T) {
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
	if err := st.SetSetting(ctx, store.SettingSearchMode, "hybrid"); err != nil {
		t.Fatalf("SetSetting search.mode: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting url: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting model: %v", err)
	}

	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "sch-hybrid-sess", Project: "search-hybrid-mode", Directory: "/test",
	})
	obs, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "sch-hybrid-sess", Type: "manual",
		Title: "widget lexical match", Content: "widget content here",
		Project: "search-hybrid-mode", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, obs.ID, "nomic-embed-text", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "search-hybrid-mode", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_search", map[string]any{
		"query": "widget",
		"limit": 10,
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("ion_search: status = %v, want ok", env["status"])
	}
	if mode, _ := env["mode"].(string); mode != "hybrid" {
		t.Errorf("ion_search: mode = %v, want %q", env["mode"], "hybrid")
	}
	if effMode, _ := env["effective_mode"].(string); effMode != "hybrid" {
		t.Errorf("ion_search: effective_mode = %v, want %q", env["effective_mode"], "hybrid")
	}
	if hybrid, _ := env["hybrid"].(bool); !hybrid {
		t.Errorf("ion_search: hybrid = %v, want true (AND query matched, fusion ran)", env["hybrid"])
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
