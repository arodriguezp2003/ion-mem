package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ionix/ion-mem/internal/project"
	"github.com/ionix/ion-mem/internal/store"
)

// mustTempStore opens a fresh store in a test-managed temp directory.
func mustTempStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("mustTempStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestResolveProject_basename_overridden_by_path_mapping verifies that when
// detect returns Source=="dir_basename" AND the store has a known session for
// that directory, resolveProject replaces the result with Source=="path_mapping"
// and the project name from the store.
func TestResolveProject_basename_overridden_by_path_mapping(t *testing.T) {
	st := mustTempStore(t)
	ctx := context.Background()

	// Record a session so /code/ion-mem is known.
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID:        "sess-1",
		Project:   "ion-mem",
		Directory: "/code/ion-mem",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	basenameResult := project.DetectionResult{
		Project: "ion-mem", // same name, different source
		Source:  "dir_basename",
		Path:    "/code/ion-mem",
	}
	detect, _ := makeDetectFunc(basenameResult, nil)
	s := &Server{store: st, detect: detect}

	det, err := s.resolveProject("", "/code/ion-mem")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if det.Source != "path_mapping" {
		t.Errorf("source = %q, want %q", det.Source, "path_mapping")
	}
	if det.Project != "ion-mem" {
		t.Errorf("project = %q, want %q", det.Project, "ion-mem")
	}
	if det.Path != "/code/ion-mem" {
		t.Errorf("path = %q, want %q", det.Path, "/code/ion-mem")
	}
}

// TestResolveProject_basename_unknown_dir_not_overridden verifies that when
// detect returns Source=="dir_basename" but the directory is not in the store,
// the result is kept as-is (no path_mapping override).
func TestResolveProject_basename_unknown_dir_not_overridden(t *testing.T) {
	st := mustTempStore(t) // empty store — no sessions

	basenameResult := project.DetectionResult{
		Project: "unknown-dir",
		Source:  "dir_basename",
		Path:    "/tmp/unknown-dir",
	}
	detect, _ := makeDetectFunc(basenameResult, nil)
	s := &Server{store: st, detect: detect}

	det, err := s.resolveProject("", "/tmp/unknown-dir")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	// Must keep the original dir_basename result — do NOT override.
	if det.Source != "dir_basename" {
		t.Errorf("source = %q, want %q (should not override unknown dir)", det.Source, "dir_basename")
	}
	if det.Project != "unknown-dir" {
		t.Errorf("project = %q, want %q", det.Project, "unknown-dir")
	}
}

// TestResolveProject_strong_source_not_overridden verifies that a git_remote
// detection result is NEVER replaced by path_mapping even when the directory
// is known in the store.
func TestResolveProject_strong_source_not_overridden(t *testing.T) {
	st := mustTempStore(t)
	ctx := context.Background()

	// Record a session for the same dir under a different project name.
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID:        "sess-strong",
		Project:   "store-project",
		Directory: "/code/repo",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	strongResult := project.DetectionResult{
		Project: "git-project",
		Source:  "git_remote",
		Path:    "/code/repo",
	}
	detect, _ := makeDetectFunc(strongResult, nil)
	s := &Server{store: st, detect: detect}

	det, err := s.resolveProject("", "/code/repo")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	// Strong source must NOT be overridden.
	if det.Source != "git_remote" {
		t.Errorf("source = %q, want %q (strong sources must not be overridden)", det.Source, "git_remote")
	}
	if det.Project != "git-project" {
		t.Errorf("project = %q, want %q", det.Project, "git-project")
	}
}
