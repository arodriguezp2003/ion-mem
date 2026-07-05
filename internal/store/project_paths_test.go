package store_test

import (
	"context"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestProjectForDirectory verifies directory-to-project lookup by most-recent session.
func TestProjectForDirectory(t *testing.T) {
	ctx := context.Background()

	t.Run("no match returns false", func(t *testing.T) {
		s := mustOpen(t)
		_, ok, err := s.ProjectForDirectory(ctx, "/no/such/dir")
		if err != nil {
			t.Fatalf("ProjectForDirectory: %v", err)
		}
		if ok {
			t.Error("expected ok=false for unknown directory, got true")
		}
	})

	t.Run("exact match returns project", func(t *testing.T) {
		s := mustOpen(t)
		if _, err := s.CreateSession(ctx, store.CreateSessionParams{
			ID:        "s-match",
			Project:   "my-proj",
			Directory: "/home/user/my-proj",
		}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		proj, ok, err := s.ProjectForDirectory(ctx, "/home/user/my-proj")
		if err != nil {
			t.Fatalf("ProjectForDirectory: %v", err)
		}
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if proj != "my-proj" {
			t.Errorf("project = %q, want %q", proj, "my-proj")
		}
	})

	t.Run("multiple sessions for same dir: most recent project wins", func(t *testing.T) {
		s := mustOpen(t)
		if _, err := s.CreateSession(ctx, store.CreateSessionParams{
			ID:        "s-old",
			Project:   "old-proj",
			Directory: "/shared/dir",
		}); err != nil {
			t.Fatalf("CreateSession old: %v", err)
		}
		// Override started_at to make the old session visibly older.
		if _, err := s.DB().ExecContext(ctx,
			"UPDATE sessions SET started_at='2020-01-01T00:00:00Z' WHERE id='s-old'",
		); err != nil {
			t.Fatalf("update started_at: %v", err)
		}
		if _, err := s.CreateSession(ctx, store.CreateSessionParams{
			ID:        "s-new",
			Project:   "new-proj",
			Directory: "/shared/dir",
		}); err != nil {
			t.Fatalf("CreateSession new: %v", err)
		}

		proj, ok, err := s.ProjectForDirectory(ctx, "/shared/dir")
		if err != nil {
			t.Fatalf("ProjectForDirectory: %v", err)
		}
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if proj != "new-proj" {
			t.Errorf("project = %q, want %q (most-recent wins)", proj, "new-proj")
		}
	})

	t.Run("trailing slash normalised", func(t *testing.T) {
		s := mustOpen(t)
		if _, err := s.CreateSession(ctx, store.CreateSessionParams{
			ID:        "s-trail",
			Project:   "trail-proj",
			Directory: "/some/path",
		}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		// Lookup with trailing slash — should still match.
		proj, ok, err := s.ProjectForDirectory(ctx, "/some/path/")
		if err != nil {
			t.Fatalf("ProjectForDirectory: %v", err)
		}
		if !ok {
			t.Fatal("expected ok=true after normalisation, got false")
		}
		if proj != "trail-proj" {
			t.Errorf("project = %q, want %q", proj, "trail-proj")
		}
	})
}

// TestProjectDirectories verifies the distinct ordered list of directories for a project.
func TestProjectDirectories(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown project returns empty slice", func(t *testing.T) {
		s := mustOpen(t)
		dirs, err := s.ProjectDirectories(ctx, "ghost")
		if err != nil {
			t.Fatalf("ProjectDirectories: %v", err)
		}
		if len(dirs) != 0 {
			t.Errorf("expected 0 dirs, got %d: %v", len(dirs), dirs)
		}
	})

	t.Run("single directory returned", func(t *testing.T) {
		s := mustOpen(t)
		if _, err := s.CreateSession(ctx, store.CreateSessionParams{
			ID:        "sd-1",
			Project:   "proj-x",
			Directory: "/users/alice/proj-x",
		}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		dirs, err := s.ProjectDirectories(ctx, "proj-x")
		if err != nil {
			t.Fatalf("ProjectDirectories: %v", err)
		}
		if len(dirs) != 1 {
			t.Fatalf("expected 1 dir, got %d", len(dirs))
		}
		if dirs[0] != "/users/alice/proj-x" {
			t.Errorf("dir[0] = %q, want %q", dirs[0], "/users/alice/proj-x")
		}
	})

	t.Run("multiple directories deduplicated and ordered most-recent first", func(t *testing.T) {
		s := mustOpen(t)
		// Three sessions: two different dirs, one repeated.
		sessions := []struct {
			id        string
			dir       string
			startedAt string
		}{
			{"sd-a", "/old/clone", "2023-01-01T00:00:00Z"},
			{"sd-b", "/current/repo", "2024-06-01T00:00:00Z"},
			{"sd-c", "/old/clone", "2022-01-01T00:00:00Z"}, // duplicate dir, older
		}
		for _, ss := range sessions {
			if _, err := s.CreateSession(ctx, store.CreateSessionParams{
				ID:        ss.id,
				Project:   "multi-proj",
				Directory: ss.dir,
			}); err != nil {
				t.Fatalf("CreateSession %q: %v", ss.id, err)
			}
			if _, err := s.DB().ExecContext(ctx,
				"UPDATE sessions SET started_at=? WHERE id=?", ss.startedAt, ss.id,
			); err != nil {
				t.Fatalf("update started_at %q: %v", ss.id, err)
			}
		}

		dirs, err := s.ProjectDirectories(ctx, "multi-proj")
		if err != nil {
			t.Fatalf("ProjectDirectories: %v", err)
		}
		// Expect exactly 2 unique dirs, most-recent first.
		if len(dirs) != 2 {
			t.Fatalf("expected 2 dirs, got %d: %v", len(dirs), dirs)
		}
		if dirs[0] != "/current/repo" {
			t.Errorf("dirs[0] = %q, want %q", dirs[0], "/current/repo")
		}
		if dirs[1] != "/old/clone" {
			t.Errorf("dirs[1] = %q, want %q", dirs[1], "/old/clone")
		}
	})
}
