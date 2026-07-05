package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// topicUpsert is a helper that inserts/upserts a topic-key observation with
// distinct title and content each time. Returns the resulting observation.
func topicUpsert(t *testing.T, s *store.Store, sessID, topic, title, content string) store.Observation {
	t.Helper()
	obs, err := s.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sessID,
		Type:      "architecture",
		Title:     title,
		Content:   content,
		Project:   "revtest",
		Scope:     "project",
		TopicKey:  topic,
	})
	if err != nil {
		t.Fatalf("topicUpsert: %v", err)
	}
	return obs
}

// TestListRevisions_TopicUpsertTwice verifies that upserting the same topic key
// twice produces exactly 1 revision capturing the ORIGINAL title/content.
func TestListRevisions_TopicUpsertTwice(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	// First upsert: creates the row.
	topicUpsert(t, s, sess.ID, "revtest/topic", "Title v1", "Content v1")
	// Second upsert: should capture v1 as a revision.
	obs2 := topicUpsert(t, s, sess.ID, "revtest/topic", "Title v2", "Content v2")

	revs, err := s.ListRevisions(context.Background(), obs2.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("expected 1 revision after 2 upserts, got %d", len(revs))
	}
	if revs[0].Title != "Title v1" {
		t.Errorf("revision.Title = %q, want %q", revs[0].Title, "Title v1")
	}
	if revs[0].Content != "Content v1" {
		t.Errorf("revision.Content = %q, want %q", revs[0].Content, "Content v1")
	}
}

// TestListRevisions_TopicUpsertThreeTimes verifies that 3 upserts produce 2
// revisions ordered newest-first.
func TestListRevisions_TopicUpsertThreeTimes(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	topicUpsert(t, s, sess.ID, "revtest/multi", "Title v1", "Content v1")
	topicUpsert(t, s, sess.ID, "revtest/multi", "Title v2", "Content v2")
	obs3 := topicUpsert(t, s, sess.ID, "revtest/multi", "Title v3", "Content v3")

	revs, err := s.ListRevisions(context.Background(), obs3.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("expected 2 revisions after 3 upserts, got %d", len(revs))
	}
	// Newest-first: last archived should be v2 (archived when v3 was written).
	if revs[0].Title != "Title v2" {
		t.Errorf("revs[0].Title = %q, want %q (newest-first)", revs[0].Title, "Title v2")
	}
	if revs[1].Title != "Title v1" {
		t.Errorf("revs[1].Title = %q, want %q", revs[1].Title, "Title v1")
	}
}

// TestListRevisions_NoRevisions verifies that a newly created observation with no
// subsequent upserts returns an empty slice (not nil, not error).
func TestListRevisions_NoRevisions(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	obs := topicUpsert(t, s, sess.ID, "revtest/new", "Title v1", "Content v1")

	revs, err := s.ListRevisions(context.Background(), obs.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if revs == nil {
		t.Error("ListRevisions: expected non-nil empty slice, got nil")
	}
	if len(revs) != 0 {
		t.Errorf("ListRevisions: expected 0 revisions for fresh obs, got %d", len(revs))
	}
}

// TestListRevisions_NotFound verifies that ListRevisions returns ErrObservationNotFound
// when the observation ID does not exist.
func TestListRevisions_NotFound(t *testing.T) {
	s := mustOpen(t)
	_, err := s.ListRevisions(context.Background(), 99999)
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("expected ErrObservationNotFound, got %v", err)
	}
}

// TestListRevisions_UpdateObservationContentChange verifies that UpdateObservation
// with a content change captures a revision.
func TestListRevisions_UpdateObservationContentChange(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	obs := topicUpsert(t, s, sess.ID, "revtest/update", "Original Title", "Original Content")

	newContent := "Updated Content"
	_, err := s.UpdateObservation(context.Background(), obs.ID, store.UpdateObservationParams{
		Content: &newContent,
	})
	if err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}

	revs, err := s.ListRevisions(context.Background(), obs.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("expected 1 revision after content update, got %d", len(revs))
	}
	if revs[0].Content != "Original Content" {
		t.Errorf("revision.Content = %q, want original %q", revs[0].Content, "Original Content")
	}
}

// TestListRevisions_UpdateObservationNoOpNoCapture verifies that UpdateObservation
// with the same content does NOT produce a revision.
func TestListRevisions_UpdateObservationNoOpNoCapture(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	obs := topicUpsert(t, s, sess.ID, "revtest/noop", "Same Title", "Same Content")

	// Update with identical content — should be a no-op for revision capture.
	sameContent := "Same Content"
	sameTitle := "Same Title"
	sameType := "architecture"
	_, err := s.UpdateObservation(context.Background(), obs.ID, store.UpdateObservationParams{
		Content: &sameContent,
		Title:   &sameTitle,
		Type:    &sameType,
	})
	if err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}

	revs, err := s.ListRevisions(context.Background(), obs.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 0 {
		t.Errorf("expected 0 revisions for no-op update, got %d", len(revs))
	}
}

// TestListRevisions_DedupDoesNotCapture verifies that a dedup hit (same content)
// does NOT produce a revision.
func TestListRevisions_DedupDoesNotCapture(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	// Insert the same observation twice — second will be a dedup hit.
	params := store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "Dedup Title",
		Content:   "Dedup Content",
		Project:   "revtest",
		Scope:     "project",
	}
	first, err := s.AddObservation(context.Background(), params)
	if err != nil {
		t.Fatalf("first AddObservation: %v", err)
	}
	_, err = s.AddObservation(context.Background(), params)
	if err != nil {
		t.Fatalf("second AddObservation (dedup): %v", err)
	}

	revs, err := s.ListRevisions(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 0 {
		t.Errorf("expected 0 revisions for dedup hit, got %d", len(revs))
	}
}

// TestListRevisions_PruneKeepsNewest10 verifies that after 12 upserts, only the
// 10 newest revisions are retained per the revisionKeepCount constant.
func TestListRevisions_PruneKeepsNewest10(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	var obsID int64
	for i := 0; i < 12; i++ {
		o := topicUpsert(t, s, sess.ID, "revtest/prune",
			"Title v"+intToHex(i+1),
			"Content v"+intToHex(i+1),
		)
		obsID = o.ID
	}

	revs, err := s.ListRevisions(context.Background(), obsID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 10 {
		t.Errorf("expected 10 revisions after prune, got %d", len(revs))
	}
}

// TestListRevisions_HardDeleteCascades verifies that hard-deleting the parent
// observation also removes all its revisions.
func TestListRevisions_HardDeleteCascades(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "revtest")

	topicUpsert(t, s, sess.ID, "revtest/cascade", "Title v1", "Content v1")
	obs2 := topicUpsert(t, s, sess.ID, "revtest/cascade", "Title v2", "Content v2")
	obsID := obs2.ID

	// Confirm there is at least 1 revision before deletion.
	revs, err := s.ListRevisions(context.Background(), obsID)
	if err != nil {
		t.Fatalf("ListRevisions pre-delete: %v", err)
	}
	if len(revs) == 0 {
		t.Fatal("expected at least 1 revision before delete")
	}

	// Hard-delete the observation.
	if err := s.DeleteObservation(context.Background(), obsID, true); err != nil {
		t.Fatalf("DeleteObservation hard: %v", err)
	}

	// Revisions must be gone via CASCADE.
	var count int
	if err := s.DB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM observation_revisions WHERE observation_id=?", obsID,
	).Scan(&count); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 revisions after hard delete cascade, got %d", count)
	}
}
