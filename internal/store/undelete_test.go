package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestUndeleteObservation_RestoresSoftDeleted verifies that a soft-deleted
// observation becomes searchable again after UndeleteObservation.
func TestUndeleteObservation_RestoresSoftDeleted(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "recoverable-obs",
		Content:   "should come back",
		Project:   "p",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// Soft delete.
	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("DeleteObservation (soft): %v", err)
	}

	// Verify it is gone from GetObservation.
	if _, err := s.GetObservation(ctx, obs.ID); !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("GetObservation after soft-delete: expected ErrObservationNotFound, got %v", err)
	}

	// Undelete.
	if err := s.UndeleteObservation(ctx, obs.ID); err != nil {
		t.Fatalf("UndeleteObservation: %v", err)
	}

	// Now GetObservation must succeed.
	restored, err := s.GetObservation(ctx, obs.ID)
	if err != nil {
		t.Fatalf("GetObservation after undelete: %v", err)
	}
	if restored.ID != obs.ID {
		t.Errorf("restored ID: got %d, want %d", restored.ID, obs.ID)
	}
	if restored.DeletedAt != nil {
		t.Errorf("restored DeletedAt must be nil, got %v", *restored.DeletedAt)
	}
}

// TestUndeleteObservation_SearchableAfterRestore verifies that FTS search finds
// the observation after undelete (the update trigger re-indexes it).
func TestUndeleteObservation_SearchableAfterRestore(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "unique-undelete-search-term",
		Content:   "content for fts verification",
		Project:   "p",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// Soft delete then undelete.
	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := s.UndeleteObservation(ctx, obs.ID); err != nil {
		t.Fatalf("undelete: %v", err)
	}

	// FTS search must find it.
	results, err := s.Search(ctx, store.SearchParams{
		Q:     "unique-undelete-search-term",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("Search after undelete: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Observation.ID == obs.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("observation %d not found in search results after undelete", obs.ID)
	}
}

// TestUndeleteObservation_NonDeletedReturnsNotFound verifies that calling
// UndeleteObservation on a non-deleted observation returns ErrObservationNotFound.
func TestUndeleteObservation_NonDeletedReturnsNotFound(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "not-deleted",
		Content:   "should not be undeleted",
		Project:   "p",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	err = s.UndeleteObservation(ctx, obs.ID)
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Errorf("expected ErrObservationNotFound for non-deleted obs, got %v", err)
	}
}

// TestUndeleteObservation_HardDeletedReturnsNotFound verifies that a hard-deleted
// observation (row removed) also returns ErrObservationNotFound.
func TestUndeleteObservation_HardDeletedReturnsNotFound(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "hard-deleted",
		Content:   "gone forever",
		Project:   "p",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	if err := s.DeleteObservation(ctx, obs.ID, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}

	err = s.UndeleteObservation(ctx, obs.ID)
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Errorf("expected ErrObservationNotFound for hard-deleted obs, got %v", err)
	}
}
