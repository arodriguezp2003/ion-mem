package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// TestTimeline_MiddleAnchor_BeforeAndAfter verifies that Timeline returns
// before + anchor + after entries correctly when the anchor is in the middle.
func TestTimeline_MiddleAnchor_BeforeAndAfter(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	// Insert 5 observations with slight delays so created_at is distinct.
	var obs [5]store.Observation
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Millisecond)
		obs[i] = mustObservation(t, s, sess.ID)
	}

	// Anchor at obs[2] (3rd), request before=2, after=2.
	entries, err := s.Timeline(ctx, obs[2].ID, 2, 2)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	// All must be observations.
	for i, e := range entries {
		if e.Kind != "observation" {
			t.Fatalf("entry[%d]: expected kind='observation', got %q", i, e.Kind)
		}
		if e.Observation == nil {
			t.Fatalf("entry[%d]: Observation must not be nil", i)
		}
	}
	// Anchor must be at index 2 (chronological order: T0..T4).
	if entries[2].Observation.ID != obs[2].ID {
		t.Fatalf("expected anchor at index 2, got ID=%d", entries[2].Observation.ID)
	}
}

// TestTimeline_StartAnchor_EmptyBefore verifies that Timeline at the start of
// the session returns only the anchor + after entries (no padding for missing before).
func TestTimeline_StartAnchor_EmptyBefore(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	var obs [3]store.Observation
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Millisecond)
		obs[i] = mustObservation(t, s, sess.ID)
	}

	// Anchor at the first observation; before=2 but nothing precedes it.
	entries, err := s.Timeline(ctx, obs[0].ID, 2, 2)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (anchor + 2 after), got %d", len(entries))
	}
	if entries[0].Observation.ID != obs[0].ID {
		t.Fatalf("expected anchor (obs[0]) at index 0, got ID=%d", entries[0].Observation.ID)
	}
}

// TestTimeline_MixedObservationsAndPrompts verifies that Timeline returns
// entries from both observations and prompts, interleaved chronologically.
func TestTimeline_MixedObservationsAndPrompts(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	// T1: observation (anchor)
	time.Sleep(2 * time.Millisecond)
	obs1 := mustObservation(t, s, sess.ID)

	// T2: prompt
	time.Sleep(2 * time.Millisecond)
	p := mustPrompt(t, s, sess.ID, "some prompt text", "proj")
	_ = p

	// T3: observation
	time.Sleep(2 * time.Millisecond)
	obs3 := mustObservation(t, s, sess.ID)

	entries, err := s.Timeline(ctx, obs1.ID, 0, 5)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Kind != "observation" || entries[0].Observation.ID != obs1.ID {
		t.Fatalf("entry[0]: expected obs1 (observation), got kind=%q id=%d", entries[0].Kind, entries[0].Observation.ID)
	}
	if entries[1].Kind != "prompt" {
		t.Fatalf("entry[1]: expected prompt, got %q", entries[1].Kind)
	}
	if entries[2].Kind != "observation" || entries[2].Observation.ID != obs3.ID {
		t.Fatalf("entry[2]: expected obs3 (observation), got kind=%q id=%d", entries[2].Kind, entries[2].Observation.ID)
	}
}

// TestTimeline_ExcludesSoftDeleted verifies that soft-deleted observations are
// not included in timeline results.
func TestTimeline_ExcludesSoftDeleted(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	// T1: anchor
	time.Sleep(2 * time.Millisecond)
	obs1 := mustObservation(t, s, sess.ID)

	// T2: obs to be soft-deleted
	time.Sleep(2 * time.Millisecond)
	obs2 := mustObservation(t, s, sess.ID)

	// T3: obs
	time.Sleep(2 * time.Millisecond)
	obs3 := mustObservation(t, s, sess.ID)

	// Soft-delete obs2.
	if err := s.DeleteObservation(ctx, obs2.ID, false); err != nil {
		t.Fatalf("DeleteObservation soft: %v", err)
	}

	entries, err := s.Timeline(ctx, obs1.ID, 0, 5)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (obs2 excluded), got %d", len(entries))
	}
	for _, e := range entries {
		if e.Kind == "observation" && e.Observation.ID == obs2.ID {
			t.Fatal("soft-deleted obs2 must not appear in timeline")
		}
	}
	if entries[1].Observation.ID != obs3.ID {
		t.Fatalf("expected obs3 at index 1, got %d", entries[1].Observation.ID)
	}
}

// TestTimeline_MissingAnchorReturnsErr verifies that Timeline returns
// ErrObservationNotFound when the anchor does not exist.
func TestTimeline_MissingAnchorReturnsErr(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	_, err := s.Timeline(ctx, 9999, 2, 2)
	if err == nil {
		t.Fatal("expected non-nil error for missing anchor, got nil")
	}
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("expected ErrObservationNotFound, got %v", err)
	}
}

// TestStats_AccurateCounts verifies that Stats returns accurate aggregate counts.
func TestStats_AccurateCounts(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess1 := mustSession(t, s, "projA")
	sess2 := mustSession(t, s, "projA")

	// 5 non-deleted observations in projA (session 1 and 2).
	var liveObs [5]store.Observation
	for i := 0; i < 5; i++ {
		sid := sess1.ID
		if i >= 3 {
			sid = sess2.ID
		}
		liveObs[i] = mustObservationForProject(t, s, sid, "projA")
	}

	// 1 soft-deleted observation in projA.
	softObs := mustObservationForProject(t, s, sess1.ID, "projA")
	if err := s.DeleteObservation(ctx, softObs.ID, false); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// 3 prompts in projA.
	for i := 0; i < 3; i++ {
		mustPrompt(t, s, sess1.ID, "prompt content "+intToHex(i), "projA")
	}

	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if stats.TotalSessions != 2 {
		t.Fatalf("expected TotalSessions=2, got %d", stats.TotalSessions)
	}
	if stats.TotalObservations != 5 {
		t.Fatalf("expected TotalObservations=5, got %d", stats.TotalObservations)
	}
	if stats.TotalPrompts != 3 {
		t.Fatalf("expected TotalPrompts=3, got %d", stats.TotalPrompts)
	}

	// Find projA in ByProject.
	var found bool
	for _, ps := range stats.ByProject {
		if ps.Project == "projA" {
			found = true
			if ps.ObservationCount != 5 {
				t.Fatalf("projA ObservationCount: expected 5, got %d", ps.ObservationCount)
			}
			if ps.PromptCount != 3 {
				t.Fatalf("projA PromptCount: expected 3, got %d", ps.PromptCount)
			}
		}
	}
	if !found {
		t.Fatal("expected projA in ByProject, not found")
	}

	// Suppress unused variable warnings from liveObs.
	_ = liveObs
}
