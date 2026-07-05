package handlers_test

import (
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

// TestSearch_SnippetPreview_UsesSnippetFieldWhenAvailable verifies that
// content_preview in the MCP response reflects the Snippet excerpt rather than
// the raw first-300-bytes of content when the match term is deep in the content.
func TestSearch_SnippetPreview_UsesSnippetFieldWhenAvailable(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sessID := "snippet-handler-session"
	st.CreateSession(ctx, store.CreateSessionParams{ID: sessID, Project: "myproj"})

	// Build content where the unique match term is far from the start (>300 bytes in).
	prefix := strings.Repeat("intro filler word sentence here again. ", 15) // ~600 bytes
	uniqueTerm := "sniphandlerterm"
	longContent := prefix + uniqueTerm + " context words after match"

	st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sessID,
		Type:      "manual",
		Title:     "long content doc for snippet handler test",
		Content:   longContent,
		Project:   "myproj",
		Scope:     "project",
	})

	res := callTool(t, ts, "ion_search", map[string]any{"query": uniqueTerm})
	env := decodeText(t, res)

	results, ok := env["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("expected results, got none")
	}

	row := results[0].(map[string]any)
	preview, _ := row["content_preview"].(string)

	// content_preview must contain the match term (snippet-based, not first-bytes).
	if !strings.Contains(preview, uniqueTerm) {
		t.Errorf("content_preview does not contain match term %q; got: %q", uniqueTerm, preview)
	}
}

// TestSearch_SnippetPreview_FallbackFor300WhenSnippetEmpty verifies that when
// Snippet is empty (e.g., short content), content_preview still works via truncation.
func TestSearch_SnippetPreview_FallbackFor300WhenSnippetEmpty(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	sessID := "snippet-fallback-session"
	st.CreateSession(ctx, store.CreateSessionParams{ID: sessID, Project: "myproj"})
	st.AddObservation(ctx, store.AddObservationParams{
		SessionID: sessID,
		Type:      "manual",
		Title:     "short content test",
		Content:   "short fallbackshortterm content",
		Project:   "myproj",
		Scope:     "project",
	})

	res := callTool(t, ts, "ion_search", map[string]any{"query": "fallbackshortterm"})
	env := decodeText(t, res)

	results, ok := env["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("expected at least 1 result")
	}

	row := results[0].(map[string]any)
	preview, _ := row["content_preview"].(string)
	if preview == "" {
		t.Error("content_preview must not be empty for short content")
	}
}
