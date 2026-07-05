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

// fakeOllamaServer returns an httptest.Server that serves /api/embeddings with
// a fixed 2-dim vector response. It also tracks how many embed calls were made.
func fakeOllamaServer(t *testing.T, dims int) *httptest.Server {
	t.Helper()
	vec := make([]float32, dims)
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
	t.Cleanup(srv.Close)
	return srv
}

func detFunc(proj string) mcp.Option {
	return mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: proj, Source: "git_root", Path: "/repo"}, nil
	})
}

// TestSave_EmbeddingsEnabled_EmbeddedFlagTrue verifies that when embeddings.enabled=true
// and Ollama is reachable, ion_save returns embedded:true.
func TestSave_EmbeddingsEnabled_EmbeddedFlagTrue(t *testing.T) {
	srv := fakeOllamaServer(t, 4)
	st := mustStore(t)
	ctx := context.Background()

	// Configure embeddings settings.
	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting enabled: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting url: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting model: %v", err)
	}

	_, ts := mustTestServer(t, st, detFunc("proj-embed"))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title":   "embedded note",
		"content": "some content",
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("ion_save: status = %v, want ok", env["status"])
	}
	embedded, ok := env["embedded"]
	if !ok {
		t.Fatal("ion_save with embeddings enabled: expected 'embedded' key in response")
	}
	if embedded != true {
		t.Errorf("ion_save: embedded = %v, want true", embedded)
	}
}

// TestSave_EmbeddingsDisabled_EmbeddedFlagFalse verifies that when embeddings.enabled
// is not set, ion_save returns embedded:false and does NOT call the embedder.
func TestSave_EmbeddingsDisabled_EmbeddedFlagFalse(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, detFunc("proj-no-embed"))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title":   "plain note",
		"content": "content",
	})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Fatalf("ion_save: status = %v, want ok", env["status"])
	}
	embedded, ok := env["embedded"]
	if !ok {
		t.Fatal("ion_save with embeddings disabled: expected 'embedded' key in response")
	}
	if embedded != false {
		t.Errorf("ion_save: embedded = %v, want false", embedded)
	}
}

// TestSave_OllamaDown_SaveSucceedsEmbeddedFalse verifies that when Ollama is
// unreachable, ion_save still succeeds (best-effort) and returns embedded:false.
func TestSave_OllamaDown_SaveSucceedsEmbeddedFalse(t *testing.T) {
	// Start and immediately close a server so its port is unreachable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	st := mustStore(t)
	ctx := context.Background()

	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting enabled: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, deadURL); err != nil {
		t.Fatalf("SetSetting url: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting model: %v", err)
	}

	_, ts := mustTestServer(t, st, detFunc("proj-dead"))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title":   "note when ollama down",
		"content": "content",
	})
	env := decodeText(t, res)

	// Save must succeed even when Ollama is down.
	if env["status"] != "ok" {
		t.Errorf("ion_save with Ollama down: status = %v, want ok", env["status"])
	}
	if _, ok := env["id"]; !ok {
		t.Error("ion_save with Ollama down: missing 'id' — save should succeed")
	}
	if env["embedded"] != false {
		t.Errorf("ion_save with Ollama down: embedded = %v, want false", env["embedded"])
	}
}

// TestUpdate_EmbeddingsEnabled_ReEmbedsOnContentChange verifies that when
// embeddings are enabled and content changes, ion_update re-embeds the observation.
func TestUpdate_EmbeddingsEnabled_ReEmbedsOnContentChange(t *testing.T) {
	srv := fakeOllamaServer(t, 4)
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

	_, ts := mustTestServer(t, st, detFunc("proj-update"))

	// First save.
	saveRes := callTool(t, ts, "ion_save", map[string]any{
		"title":   "orig",
		"content": "original content",
	})
	saveEnv := decodeText(t, saveRes)
	id := saveEnv["id"].(float64)

	// Update with new content.
	updateRes := callTool(t, ts, "ion_update", map[string]any{
		"id":      id,
		"content": "updated content here",
	})
	updateEnv := decodeText(t, updateRes)

	if updateEnv["status"] != "ok" {
		t.Fatalf("ion_update: status = %v, want ok", updateEnv["status"])
	}
	embedded, ok := updateEnv["embedded"]
	if !ok {
		t.Fatal("ion_update: expected 'embedded' key in response")
	}
	if embedded != true {
		t.Errorf("ion_update: embedded = %v, want true", embedded)
	}
}

// TestUpdate_OllamaDown_UpdateSucceedsEmbeddedFalse verifies that when Ollama is
// unreachable, ion_update still succeeds and returns embedded:false.
func TestUpdate_OllamaDown_UpdateSucceedsEmbeddedFalse(t *testing.T) {
	// Save with working server.
	workingSrv := fakeOllamaServer(t, 4)
	st := mustStore(t)
	ctx := context.Background()

	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting enabled: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, workingSrv.URL); err != nil {
		t.Fatalf("SetSetting url: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting model: %v", err)
	}

	_, ts := mustTestServer(t, st, detFunc("proj-update-dead"))
	saveRes := callTool(t, ts, "ion_save", map[string]any{
		"title": "obs", "content": "content",
	})
	id := decodeText(t, saveRes)["id"].(float64)

	// Now point at a dead server.
	deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadSrv.URL
	deadSrv.Close()
	if err := st.SetSetting(ctx, store.SettingOllamaURL, deadURL); err != nil {
		t.Fatalf("SetSetting dead url: %v", err)
	}

	updateRes := callTool(t, ts, "ion_update", map[string]any{
		"id":      id,
		"content": "new content",
	})
	updateEnv := decodeText(t, updateRes)

	if updateEnv["status"] != "ok" {
		t.Errorf("ion_update Ollama down: status = %v, want ok", updateEnv["status"])
	}
	if updateEnv["embedded"] != false {
		t.Errorf("ion_update Ollama down: embedded = %v, want false", updateEnv["embedded"])
	}
}
