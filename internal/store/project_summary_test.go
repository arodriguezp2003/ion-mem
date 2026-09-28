package store_test

import (
	"context"
	"testing"
	"time"
)

// TestProjectSummaries verifies that ProjectSummaries returns per-project
// aggregates with observation count, session count, and last-activity time.
func TestProjectSummaries(t *testing.T) {
	ctx := context.Background()

	t.Run("empty store returns empty slice", func(t *testing.T) {
		s := mustOpen(t)
		summaries, err := s.ProjectSummaries(ctx)
		if err != nil {
			t.Fatalf("ProjectSummaries: %v", err)
		}
		if len(summaries) != 0 {
			t.Fatalf("expected 0 summaries, got %d", len(summaries))
		}
	})

	t.Run("single project with observations and session", func(t *testing.T) {
		s := mustOpen(t)
		sess := mustSession(t, s, "proj-a")
		mustObservationForProject(t, s, sess.ID, "proj-a")
		mustObservationForProject(t, s, sess.ID, "proj-a")

		summaries, err := s.ProjectSummaries(ctx)
		if err != nil {
			t.Fatalf("ProjectSummaries: %v", err)
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
		got := summaries[0]
		if got.Project != "proj-a" {
			t.Errorf("project = %q, want %q", got.Project, "proj-a")
		}
		if got.ObservationCount != 2 {
			t.Errorf("ObservationCount = %d, want 2", got.ObservationCount)
		}
		if got.SessionCount != 1 {
			t.Errorf("SessionCount = %d, want 1", got.SessionCount)
		}
		if got.LastActivity.IsZero() {
			t.Error("LastActivity should not be zero")
		}
	})

	t.Run("two projects are returned sorted alphabetically", func(t *testing.T) {
		s := mustOpen(t)
		sessB := mustSession(t, s, "proj-b")
		sessA := mustSession(t, s, "proj-a")
		mustObservationForProject(t, s, sessB.ID, "proj-b")
		mustObservationForProject(t, s, sessA.ID, "proj-a")
		mustObservationForProject(t, s, sessA.ID, "proj-a")

		summaries, err := s.ProjectSummaries(ctx)
		if err != nil {
			t.Fatalf("ProjectSummaries: %v", err)
		}
		if len(summaries) != 2 {
			t.Fatalf("expected 2 summaries, got %d", len(summaries))
		}
		if summaries[0].Project != "proj-a" {
			t.Errorf("first project = %q, want %q", summaries[0].Project, "proj-a")
		}
		if summaries[0].ObservationCount != 2 {
			t.Errorf("proj-a ObservationCount = %d, want 2", summaries[0].ObservationCount)
		}
		if summaries[1].Project != "proj-b" {
			t.Errorf("second project = %q, want %q", summaries[1].Project, "proj-b")
		}
		if summaries[1].ObservationCount != 1 {
			t.Errorf("proj-b ObservationCount = %d, want 1", summaries[1].ObservationCount)
		}
	})

	t.Run("soft-deleted observations are excluded from count", func(t *testing.T) {
		s := mustOpen(t)
		sess := mustSession(t, s, "proj-c")
		obs1 := mustObservationForProject(t, s, sess.ID, "proj-c")
		mustObservationForProject(t, s, sess.ID, "proj-c")

		// Soft-delete obs1.
		if err := s.DeleteObservation(ctx, obs1.ID, false); err != nil {
			t.Fatalf("DeleteObservation: %v", err)
		}

		summaries, err := s.ProjectSummaries(ctx)
		if err != nil {
			t.Fatalf("ProjectSummaries: %v", err)
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
		if summaries[0].ObservationCount != 1 {
			t.Errorf("ObservationCount after soft delete = %d, want 1", summaries[0].ObservationCount)
		}
	})

	t.Run("last activity reflects most recent observation updated_at", func(t *testing.T) {
		s := mustOpen(t)
		sess := mustSession(t, s, "proj-d")
		mustObservationForProject(t, s, sess.ID, "proj-d")

		summaries, err := s.ProjectSummaries(ctx)
		if err != nil {
			t.Fatalf("ProjectSummaries: %v", err)
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
		// LastActivity should be within a few seconds of now.
		if time.Since(summaries[0].LastActivity) > 5*time.Second {
			t.Errorf("LastActivity too old: %v", summaries[0].LastActivity)
		}
	})
}
