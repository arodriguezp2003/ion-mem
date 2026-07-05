package handlers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ionix/ion-mem/internal/mcp"
	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

// TestCurrentProject_known_directories_included verifies that the
// ion_current_project response includes a known_directories array populated
// from the store when directories exist for the resolved project.
func TestCurrentProject_known_directories_included(t *testing.T) {
	st := mustStore(t)
	ctx := context.Background()

	// Record two sessions so the project has known directories.
	dirs := []string{"/home/alice/repo", "/home/bob/repo"}
	for i, d := range dirs {
		sessID := fmt.Sprintf("sess-dirs-%d", i)
		if _, err := st.CreateSession(ctx, store.CreateSessionParams{
			ID:        sessID,
			Project:   "myproj",
			Directory: d,
		}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	det := project.DetectionResult{
		Project: "myproj",
		Source:  "git_remote",
		Path:    "/home/alice/repo",
	}
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return det, nil
	}))

	res := callTool(t, ts, "ion_current_project", map[string]any{})
	m := decodeText(t, res)

	rawDirs, ok := m["known_directories"]
	if !ok {
		t.Fatal("known_directories missing from ion_current_project response")
	}
	dirsSlice, ok := rawDirs.([]any)
	if !ok {
		t.Fatalf("known_directories is %T, want []any", rawDirs)
	}
	if len(dirsSlice) == 0 {
		t.Error("known_directories should be non-empty when sessions exist for the project")
	}
}

// TestCurrentProject_no_known_directories_when_no_sessions verifies that
// known_directories is absent (or empty) when no sessions exist for the project.
func TestCurrentProject_no_known_directories_when_no_sessions(t *testing.T) {
	st := mustStore(t) // empty store

	det := project.DetectionResult{
		Project: "fresh-proj",
		Source:  "git_remote",
		Path:    "/repo/fresh-proj",
	}
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return det, nil
	}))

	res := callTool(t, ts, "ion_current_project", map[string]any{})
	m := decodeText(t, res)

	// When the store has no sessions for the project, known_directories should
	// be absent (not included in the response body).
	if raw, ok := m["known_directories"]; ok {
		dirs, _ := raw.([]any)
		if len(dirs) > 0 {
			t.Errorf("expected no known_directories for fresh project, got %v", dirs)
		}
	}
}

// TestCurrentProject_known_directories_capped_at_5 verifies that at most 5
// directories are returned even when many sessions exist.
func TestCurrentProject_known_directories_capped_at_5(t *testing.T) {
	st := mustStore(t)
	ctx := context.Background()

	// Create 7 distinct directories.
	for i := 0; i < 7; i++ {
		dir := "/home/user/clone" + string(rune('A'+i))
		sessID := "sess-cap-" + string(rune('A'+i))
		if _, err := st.CreateSession(ctx, store.CreateSessionParams{
			ID:        sessID,
			Project:   "capped-proj",
			Directory: dir,
		}); err != nil {
			t.Fatalf("CreateSession %q: %v", sessID, err)
		}
	}

	det := project.DetectionResult{
		Project: "capped-proj",
		Source:  "git_remote",
		Path:    "/home/user/cloneA",
	}
	_, ts := mustTestServer(t, st, mcp.WithDetectFunc(func(_ string) (project.DetectionResult, error) {
		return det, nil
	}))

	res := callTool(t, ts, "ion_current_project", map[string]any{})
	m := decodeText(t, res)

	raw, ok := m["known_directories"]
	if !ok {
		t.Fatal("known_directories missing from response")
	}
	dirs, ok := raw.([]any)
	if !ok {
		t.Fatalf("known_directories is %T, want []any", raw)
	}
	if len(dirs) > 5 {
		t.Errorf("known_directories has %d entries, want at most 5", len(dirs))
	}
}
