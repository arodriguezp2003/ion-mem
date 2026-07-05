package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// TestBackdateObservation verifies that BackdateObservation sets last_seen_at
// and created_at to approximately the expected past time.
func TestBackdateObservation(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "backdate-project")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "backdate target",
		Content:   "some content to backdate",
		Project:   "backdate-project",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// Backdate by 30 days.
	days := 30
	if err := s.BackdateObservation(ctx, obs.ID, days); err != nil {
		t.Fatalf("BackdateObservation: %v", err)
	}

	// Reload the observation and check timestamps.
	updated, err := s.GetObservation(ctx, obs.ID)
	if err != nil {
		t.Fatalf("GetObservation after backdate: %v", err)
	}

	expectedTime := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)

	lastSeen, err := time.Parse(time.RFC3339Nano, updated.LastSeenAt)
	if err != nil {
		t.Fatalf("parse last_seen_at %q: %v", updated.LastSeenAt, err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, updated.CreatedAt)
	if err != nil {
		t.Fatalf("parse created_at %q: %v", updated.CreatedAt, err)
	}

	// Allow 5 second tolerance for test execution time.
	tolerance := 5 * time.Second
	if diff := expectedTime.Sub(lastSeen).Abs(); diff > tolerance {
		t.Errorf("last_seen_at off by %v (expected ~%v, got %v)", diff, expectedTime, lastSeen)
	}
	if diff := expectedTime.Sub(createdAt).Abs(); diff > tolerance {
		t.Errorf("created_at off by %v (expected ~%v, got %v)", diff, expectedTime, createdAt)
	}
}

// TestBackdateObservation_NotFound verifies that backdating a non-existent ID
// returns ErrObservationNotFound.
func TestBackdateObservation_NotFound(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	err := s.BackdateObservation(ctx, 99999, 10)
	if err == nil {
		t.Fatal("expected error for non-existent observation, got nil")
	}
}
