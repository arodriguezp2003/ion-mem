package handlers_test

import (
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

func TestSearch_ranked_results_for_matching_query(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	// Seed observations directly via store.
	ctx := contextBG(t)
	sessID := "search-test-session"
	st.CreateSession(ctx, store.CreateSessionParams{ID: sessID, Project: "myproj"})
	for _, title := range []string{"alpha observation", "beta observation", "gamma note"} {
		st.AddObservation(ctx, store.AddObservationParams{
			SessionID: sessID,
			Type:      "manual",
			Title:     title,
			Content:   title + " content",
			Project:   "myproj",
			Scope:     "project",
		})
	}

	// Search for "observation" — should match alpha and beta.
	res := callTool(t, ts, "ion_search", map[string]any{"query": "observation"})
	env := decodeText(t, res)

	results, ok := env["results"].([]any)
	if !ok {
		t.Fatalf("results is %T, want []any", env["results"])
	}
	if len(results) < 2 {
		t.Errorf("got %d results, want >= 2 for 'observation'", len(results))
	}
	count := env["count"].(float64)
	if count < 2 {
		t.Errorf("count = %v, want >= 2", count)
	}
}

func TestSearch_StatusOkOnSuccess(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_search", map[string]any{"query": "something"})
	env := decodeText(t, res)

	if env["status"] != "ok" {
		t.Errorf("status = %v, want %q", env["status"], "ok")
	}
	if _, hasCode := env["error_code"]; hasCode {
		t.Error("success envelope must not contain error_code")
	}
}

func TestSearch_empty_store_returns_empty_array_not_error(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "myproj", Source: "git_root", Path: "/repo"}, nil
	}))

	res := callTool(t, ts, "ion_search", map[string]any{"query": "nonexistent content xyz"})
	env := decodeText(t, res)

	if res.IsError {
		t.Error("ion_search returned isError=true for empty results, want false")
	}

	// results must be present — either [] or null (treated as empty).
	if _, hasKey := env["results"]; !hasKey {
		t.Fatal("ion_search: 'results' key missing from response")
	}
}

func TestSearch_project_override_scopes_results(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "proj-a", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-a", Project: "proj-a"})
	st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-b", Project: "proj-b"})
	st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "sess-a", Type: "manual", Title: "canary in proj-a", Content: "canary", Project: "proj-a", Scope: "project",
	})
	st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "sess-b", Type: "manual", Title: "canary in proj-b", Content: "canary", Project: "proj-b", Scope: "project",
	})

	// Search with project override "proj-b" — should only return proj-b result.
	res := callTool(t, ts, "ion_search", map[string]any{
		"query":   "canary",
		"project": "proj-b",
	})
	env := decodeText(t, res)
	results, ok := env["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatal("expected at least 1 result for proj-b")
	}
	for _, r := range results {
		row := r.(map[string]any)
		if row["project"] != "proj-b" {
			t.Errorf("got result from project %q, want proj-b only", row["project"])
		}
	}
}

func TestSearch_all_projects_true_returns_across_projects(t *testing.T) {
	st := mustStore(t)
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return project.DetectionResult{Project: "proj-a", Source: "git_root", Path: "/repo"}, nil
	}))

	ctx := contextBG(t)
	st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-a2", Project: "proj-a"})
	st.CreateSession(ctx, store.CreateSessionParams{ID: "sess-b2", Project: "proj-b"})
	st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "sess-a2", Type: "manual", Title: "multiproject canary A", Content: "multiproject", Project: "proj-a", Scope: "project",
	})
	st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "sess-b2", Type: "manual", Title: "multiproject canary B", Content: "multiproject", Project: "proj-b", Scope: "project",
	})

	res := callTool(t, ts, "ion_search", map[string]any{
		"query":        "multiproject",
		"all_projects": true,
	})
	env := decodeText(t, res)
	results, ok := env["results"].([]any)
	if !ok || len(results) < 2 {
		t.Errorf("all_projects=true: got %d results, want >= 2", len(results))
	}
}
