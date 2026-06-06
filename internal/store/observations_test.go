package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestAddObservation_NewRow verifies that AddObservation inserts a new row
// and returns an Observation with RevisionCount=1, DuplicateCount=0,
// and a sync_id prefixed with "obs-".
func TestAddObservation_NewRow(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "Test title",
		Content:   "Test content",
		Project:   "p",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if !strings.HasPrefix(obs.SyncID, "obs-") {
		t.Errorf("SyncID: got %q, want prefix obs-", obs.SyncID)
	}
	if obs.RevisionCount != 1 {
		t.Errorf("RevisionCount: got %d, want 1", obs.RevisionCount)
	}
	if obs.DuplicateCount != 0 {
		t.Errorf("DuplicateCount: got %d, want 0", obs.DuplicateCount)
	}
	if obs.ID == 0 {
		t.Error("ID: expected non-zero")
	}

	// Round-trip via GetObservation.
	got, err := s.GetObservation(ctx, obs.ID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if got.ID != obs.ID {
		t.Errorf("GetObservation ID: got %d, want %d", got.ID, obs.ID)
	}
	if got.Content != "Test content" {
		t.Errorf("Content: got %q, want %q", got.Content, "Test content")
	}
}

// TestAddObservation_Deduplication verifies that inserting the same
// (content, project, scope, type, title) increments DuplicateCount and
// updates LastSeenAt without creating a new row.
func TestAddObservation_Deduplication(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	params := store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "Same title",
		Content:   "Same content",
		Project:   "p",
		Scope:     "project",
	}

	first, err := s.AddObservation(ctx, params)
	if err != nil {
		t.Fatalf("first AddObservation: %v", err)
	}

	second, err := s.AddObservation(ctx, params)
	if err != nil {
		t.Fatalf("second AddObservation: %v", err)
	}

	// Same row — same ID.
	if second.ID != first.ID {
		t.Errorf("dedup: expected same ID %d, got %d", first.ID, second.ID)
	}
	if second.DuplicateCount != 1 {
		t.Errorf("DuplicateCount: got %d, want 1", second.DuplicateCount)
	}

	// Count rows in DB — must still be exactly 1.
	var count int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE title='Same title'",
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after dedup, got %d", count)
	}
}

// TestAddObservation_TopicKeyUpsert_IncrementsRevision verifies that a
// topic-key upsert updates the existing row and increments revision_count.
func TestAddObservation_TopicKeyUpsert_IncrementsRevision(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	first, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "architecture",
		Title:     "Auth design v1",
		Content:   "content v1",
		Project:   "p",
		Scope:     "project",
		TopicKey:  "arch/auth",
	})
	if err != nil {
		t.Fatalf("first AddObservation: %v", err)
	}
	if first.RevisionCount != 1 {
		t.Errorf("initial RevisionCount: got %d, want 1", first.RevisionCount)
	}

	second, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "architecture",
		Title:     "Auth design v2",
		Content:   "content v2",
		Project:   "p",
		Scope:     "project",
		TopicKey:  "arch/auth",
	})
	if err != nil {
		t.Fatalf("second AddObservation: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("topic upsert: expected same ID %d, got %d", first.ID, second.ID)
	}
	if second.RevisionCount != 2 {
		t.Errorf("RevisionCount: got %d, want 2", second.RevisionCount)
	}
	if second.Content != "content v2" {
		t.Errorf("Content: got %q, want %q", second.Content, "content v2")
	}

	// Exactly one row in DB for this topic key.
	var count int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE topic_key='arch/auth'",
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row for topic_key=arch/auth, got %d", count)
	}
}

// TestAddObservation_TopicKeyUpsert_NoExistingRowInsertsNew verifies that
// specifying a topic_key when no matching row exists inserts a new row.
func TestAddObservation_TopicKeyUpsert_NoExistingRowInsertsNew(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "New topic",
		Content:   "content",
		Project:   "p",
		Scope:     "project",
		TopicKey:  "new/key",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if obs.RevisionCount != 1 {
		t.Errorf("RevisionCount: got %d, want 1", obs.RevisionCount)
	}
}

// TestAddObservation_RejectsUnknownSession verifies that AddObservation
// returns a non-nil error when the session_id does not exist.
func TestAddObservation_RejectsUnknownSession(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: "nonexistent",
		Type:      "decision",
		Title:     "T",
		Content:   "C",
		Project:   "p",
		Scope:     "project",
	})
	if err == nil {
		t.Fatal("expected non-nil error for unknown session, got nil")
	}
}

// TestGetObservation_NotFound verifies that GetObservation returns
// ErrObservationNotFound for a non-existent id.
func TestGetObservation_NotFound(t *testing.T) {
	s := mustOpen(t)
	_, err := s.GetObservation(context.Background(), 9999)
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("expected ErrObservationNotFound, got %v", err)
	}
}

// TestUpdateObservation_PartialUpdate verifies that UpdateObservation applies
// only the specified fields and always updates updated_at.
func TestUpdateObservation_PartialUpdate(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	obs := mustObservation(t, s, mustSession(t, s, "p").ID)

	newContent := "new content"
	updated, err := s.UpdateObservation(ctx, obs.ID, store.UpdateObservationParams{
		Content: &newContent,
	})
	if err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}
	if updated.Title != obs.Title {
		t.Errorf("Title: got %q, want %q (should be unchanged)", updated.Title, obs.Title)
	}
	if updated.Content != "new content" {
		t.Errorf("Content: got %q, want %q", updated.Content, "new content")
	}
}

// TestUpdateObservation_NotFound verifies that UpdateObservation returns
// ErrObservationNotFound when the id does not exist.
func TestUpdateObservation_NotFound(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	newContent := "x"
	_, err := s.UpdateObservation(ctx, 9999, store.UpdateObservationParams{Content: &newContent})
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("expected ErrObservationNotFound, got %v", err)
	}
}

// TestRecentObservations_ExcludesSoftDeleted verifies that RecentObservations
// does not return soft-deleted observations.
func TestRecentObservations_ExcludesSoftDeleted(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs := mustObservation(t, s, sess.ID)

	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("DeleteObservation soft: %v", err)
	}

	recent, err := s.RecentObservations(ctx, store.RecentObservationsParams{Project: "p"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	for _, r := range recent {
		if r.ID == obs.ID {
			t.Errorf("soft-deleted observation %d should not appear in RecentObservations", obs.ID)
		}
	}
}

// TestRecentObservations_ProjectAndScopeFilters verifies that project and
// scope filters are applied correctly.
func TestRecentObservations_ProjectAndScopeFilters(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sessA := mustSession(t, s, "A")
	sessB := mustSession(t, s, "B")

	// Insert into project A (scope=project) and project B (scope=personal).
	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sessA.ID, Type: "decision", Title: "in A", Content: "a",
		Project: "A", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation A: %v", err)
	}
	_, err = s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sessB.ID, Type: "decision", Title: "in B", Content: "b",
		Project: "B", Scope: "personal",
	})
	if err != nil {
		t.Fatalf("AddObservation B: %v", err)
	}

	// Filter by project A only.
	got, err := s.RecentObservations(ctx, store.RecentObservationsParams{Project: "A"})
	if err != nil {
		t.Fatalf("RecentObservations A: %v", err)
	}
	for _, o := range got {
		if o.Project != "A" {
			t.Errorf("expected only project A, got %q", o.Project)
		}
	}

	// Filter by scope=personal only.
	got, err = s.RecentObservations(ctx, store.RecentObservationsParams{Scope: "personal"})
	if err != nil {
		t.Fatalf("RecentObservations personal: %v", err)
	}
	for _, o := range got {
		if o.Scope != "personal" {
			t.Errorf("expected only scope=personal, got %q", o.Scope)
		}
	}
}

// TestDeleteObservation_SoftDelete verifies that soft-delete sets deleted_at.
func TestDeleteObservation_SoftDelete(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	obs := mustObservation(t, s, mustSession(t, s, "p").ID)

	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	// GetObservation should not find it (soft-deleted excluded by default).
	_, err := s.GetObservation(ctx, obs.ID)
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("expected ErrObservationNotFound for soft-deleted, got %v", err)
	}

	// Confirm deleted_at is set in the DB.
	var deletedAt *string
	if err := s.DB().QueryRowContext(ctx,
		"SELECT deleted_at FROM observations WHERE id=?", obs.ID,
	).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Error("deleted_at: expected non-null after soft delete")
	}
}

// TestDeleteObservation_HardDelete verifies that hard-delete removes the row
// from the observations table.
func TestDeleteObservation_HardDelete(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	obs := mustObservation(t, s, mustSession(t, s, "p").ID)

	if err := s.DeleteObservation(ctx, obs.ID, true); err != nil {
		t.Fatalf("DeleteObservation hard: %v", err)
	}

	var count int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE id=?", obs.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after hard delete, got %d", count)
	}
}

// TestDeleteObservation_NotFound verifies that DeleteObservation returns
// ErrObservationNotFound for a non-existent id.
func TestDeleteObservation_NotFound(t *testing.T) {
	s := mustOpen(t)
	err := s.DeleteObservation(context.Background(), 8888, false)
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("expected ErrObservationNotFound, got %v", err)
	}
}

// TestDeleteSession_BlockedByObservations verifies that DeleteSession returns
// ErrSessionHasObservations when a child observation exists.
func TestDeleteSession_BlockedByObservations(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")
	mustObservation(t, s, sess.ID)

	err := s.DeleteSession(ctx, sess.ID)
	if !errors.Is(err, store.ErrSessionHasObservations) {
		t.Fatalf("expected ErrSessionHasObservations, got %v", err)
	}

	// The session row must still exist.
	_, err = s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Errorf("session should still exist after blocked delete, got: %v", err)
	}
}
