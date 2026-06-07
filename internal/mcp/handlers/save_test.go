package handlers_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
)

func TestSave_round_trip_stores_observation(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title":   "my note",
		"content": "some content",
	})
	env := decodeText(t, res)

	id, ok := env["id"]
	if !ok {
		t.Fatal("ion_save: missing 'id' in response")
	}
	if id.(float64) <= 0 {
		t.Errorf("id = %v, want > 0", id)
	}
	if env["project"] != "myproj" {
		t.Errorf("project = %q, want %q", env["project"], "myproj")
	}
}

func TestSave_with_buffered_prompt_attaches_it(t *testing.T) {
	st := mustStore(t)
	ionSrv, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Manually inject a prompt into the buffer for a specific session.
	sessionID := "test-session-attach"
	ionSrv.RecordPromptForTest(sessionID, "user prompt text")

	res := callTool(t, ts, "ion_save", map[string]any{
		"title":          "note with prompt",
		"content":        "content",
		"session_id":     sessionID,
		"capture_prompt": true,
	})
	env := decodeText(t, res)

	if env["prompt_attached"] != true {
		t.Errorf("prompt_attached = %v, want true", env["prompt_attached"])
	}
}

func TestSave_no_prompt_buffer_prompt_not_attached(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title":          "note without prompt",
		"content":        "content",
		"capture_prompt": true,
	})
	env := decodeText(t, res)

	if env["prompt_attached"] != false {
		t.Errorf("prompt_attached = %v, want false when no buffer", env["prompt_attached"])
	}
}

func TestSave_topic_key_upsert_returns_same_id(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// First save with a topic_key.
	res1 := callTool(t, ts, "ion_save", map[string]any{
		"title":     "Architecture doc",
		"content":   "v1",
		"topic_key": "architecture/auth",
	})
	env1 := decodeText(t, res1)
	id1 := env1["id"].(float64)

	// Second save with same topic_key — must return same id, revision_count incremented.
	res2 := callTool(t, ts, "ion_save", map[string]any{
		"title":     "Architecture doc",
		"content":   "v2",
		"topic_key": "architecture/auth",
	})
	env2 := decodeText(t, res2)
	id2 := env2["id"].(float64)

	if id1 != id2 {
		t.Errorf("topic_key upsert: id1=%v id2=%v, want same id", id1, id2)
	}
	rc := env2["revision_count"].(float64)
	if rc < 2 {
		t.Errorf("revision_count = %v, want >= 2 after upsert", rc)
	}
}

func TestSave_dedup_collision_increments_duplicate_count(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Save the same content twice — dedup by normalized_hash.
	callTool(t, ts, "ion_save", map[string]any{
		"title":   "Unique title",
		"content": "Identical content for dedup test",
		"type":    "manual",
	})
	res2 := callTool(t, ts, "ion_save", map[string]any{
		"title":   "Unique title",
		"content": "Identical content for dedup test",
		"type":    "manual",
	})
	env2 := decodeText(t, res2)

	dc := env2["duplicate_count"].(float64)
	if dc < 1 {
		t.Errorf("duplicate_count = %v, want >= 1 after dedup", dc)
	}
}

func TestSave_project_param_overrides_cached_project(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "default-proj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_save", map[string]any{
		"title":   "note",
		"content": "content",
		"project": "other-proj",
	})
	env := decodeText(t, res)

	if env["project"] != "other-proj" {
		t.Errorf("envelope project = %q, want %q", env["project"], "other-proj")
	}
}

func TestSave_unknown_session_id_auto_creates_session(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Supply a session_id that doesn't exist in the store — must NOT fail.
	res := callTool(t, ts, "ion_save", map[string]any{
		"title":      "auto-session note",
		"content":    "content",
		"session_id": "never-seen-before-session",
	})
	env := decodeText(t, res)

	id, ok := env["id"]
	if !ok {
		t.Fatal("ion_save: missing 'id' — unknown session_id should auto-create session")
	}
	if id.(float64) <= 0 {
		t.Errorf("id = %v, want > 0", id)
	}
}
