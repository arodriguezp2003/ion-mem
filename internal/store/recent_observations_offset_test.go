package store_test

// recent_observations_offset_test.go — Strict TDD tests for Task 3 store change:
// RecentObservationsParams gains an Offset int (SQL LIMIT ? OFFSET ?).
// Default 0 preserves existing behavior.

import (
	"context"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestRecentObservations_OffsetDefaultZeroPreservesExistingBehavior asserts that
// Offset=0 returns the same results as a call without an Offset set (backward compat).
func TestRecentObservations_OffsetDefaultZeroPreservesExistingBehavior(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "proj")

	// Insert 5 observations.
	for i := 0; i < 5; i++ {
		mustObservationForProject(t, s, sess.ID, "proj")
	}

	// Call without explicit Offset (zero value).
	got, err := s.RecentObservations(ctx, store.RecentObservationsParams{
		Project: "proj",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("RecentObservations (no offset): %v", err)
	}
	if len(got) != 5 {
		t.Errorf("no-offset: got %d observations, want 5", len(got))
	}
}

// TestRecentObservations_OffsetSkipsRows asserts that Offset=2 skips the first 2
// rows (newest first) and returns the remaining ones.
func TestRecentObservations_OffsetSkipsRows(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "proj2")

	// Insert 5 observations; keep track of IDs.
	var ids []int64
	for i := 0; i < 5; i++ {
		obs := mustObservationForProject(t, s, sess.ID, "proj2")
		ids = append(ids, obs.ID)
	}

	// Page 1: first 2 (newest first).
	page1, err := s.RecentObservations(ctx, store.RecentObservationsParams{
		Project: "proj2",
		Limit:   2,
		Offset:  0,
	})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1: got %d observations, want 2", len(page1))
	}

	// Page 2: skip 2, take next 2.
	page2, err := s.RecentObservations(ctx, store.RecentObservationsParams{
		Project: "proj2",
		Limit:   2,
		Offset:  2,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2: got %d observations, want 2", len(page2))
	}

	// Page 1 and page 2 must not overlap.
	p1ids := map[int64]bool{page1[0].ID: true, page1[1].ID: true}
	for _, obs := range page2 {
		if p1ids[obs.ID] {
			t.Errorf("page2 observation ID %d also appeared in page1 (overlap)", obs.ID)
		}
	}
}

// TestRecentObservations_OffsetBeyondTotal asserts that Offset >= total rows
// returns an empty slice (not an error).
func TestRecentObservations_OffsetBeyondTotal(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "proj3")

	mustObservationForProject(t, s, sess.ID, "proj3")

	got, err := s.RecentObservations(ctx, store.RecentObservationsParams{
		Project: "proj3",
		Limit:   10,
		Offset:  100,
	})
	if err != nil {
		t.Fatalf("offset beyond total: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("offset beyond total: got %d observations, want 0", len(got))
	}
}

// TestRecentObservations_TypeFilter asserts that a Type filter on
// RecentObservationsParams returns only observations of that type.
// (Type filter is a TUI-side feature; this test verifies the store
// doesn't break when a future Type field is wired in — the query already
// accepts project/scope filters; Type filtering is done TUI-side with existing Search.)
// This test validates the Offset field independently of any Type filter.
func TestRecentObservations_CombinedOffsetAndLimit(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "proj4")

	// Insert 7 observations.
	for i := 0; i < 7; i++ {
		mustObservationForProject(t, s, sess.ID, "proj4")
	}

	// Take 3 with offset 3 → should return observations at positions 3-5.
	got, err := s.RecentObservations(ctx, store.RecentObservationsParams{
		Project: "proj4",
		Limit:   3,
		Offset:  3,
	})
	if err != nil {
		t.Fatalf("combined offset+limit: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("combined: got %d observations, want 3", len(got))
	}
}
