package handlers_test

import (
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
)

func TestSuggestTopicKey_type_prefix_with_kebab_title(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_suggest_topic_key", map[string]any{
		"type":  "architecture",
		"title": "Auth Model",
	})
	env := decodeText(t, res)

	key, ok := env["topic_key"].(string)
	if !ok || key == "" {
		t.Fatal("ion_suggest_topic_key: missing or empty 'topic_key'")
	}
	if key != "architecture/auth-model" {
		t.Errorf("topic_key = %q, want %q", key, "architecture/auth-model")
	}
}

func TestSuggestTopicKey_no_type_returns_key_without_prefix(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_suggest_topic_key", map[string]any{
		"title": "My Decision",
	})
	env := decodeText(t, res)

	key, ok := env["topic_key"].(string)
	if !ok || key == "" {
		t.Fatal("ion_suggest_topic_key: missing or empty 'topic_key' when no type")
	}
	// With no type, should be "learning/my-decision" (default family) or just the slug
	// Per spec R-S2-STK-04: prefix with <type>/ if provided. No type = family from default.
	// The scenario S2-T-STK-02: no type → "my-decision"
	if key != "my-decision" {
		t.Errorf("topic_key = %q, want %q when no type provided", key, "my-decision")
	}
}

func TestSuggestTopicKey_special_chars_stripped_to_kebab(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_suggest_topic_key", map[string]any{
		"type":  "decision",
		"title": "Fix Auth Bug! #123",
	})
	env := decodeText(t, res)

	key, ok := env["topic_key"].(string)
	if !ok || key == "" {
		t.Fatal("ion_suggest_topic_key: missing or empty 'topic_key' with special chars")
	}
	// Only [a-z0-9-/] should remain.
	for _, c := range key {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '/') {
			t.Errorf("topic_key %q contains invalid char %q", key, string(c))
		}
	}
	if !strings.HasPrefix(key, "decision/") {
		t.Errorf("topic_key = %q, want prefix %q", key, "decision/")
	}
}

func TestSuggestTopicKey_is_pure_function_no_store_side_effects(t *testing.T) {
	st := mustStore(t)
	ctx := contextBG(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Call multiple times — store must remain empty.
	callTool(t, ts, "ion_suggest_topic_key", map[string]any{"type": "architecture", "title": "one"})
	callTool(t, ts, "ion_suggest_topic_key", map[string]any{"type": "decision", "title": "two"})
	callTool(t, ts, "ion_suggest_topic_key", map[string]any{"title": "three"})

	// Verify no observations were created.
	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalObservations != 0 {
		t.Errorf("TotalObservations = %d, want 0 (suggest is pure, no store writes)", stats.TotalObservations)
	}
}
