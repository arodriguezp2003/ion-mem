package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// TestCreateSession_RoundTrip verifies that CreateSession inserts a row and
// GetSession returns the same data.
func TestCreateSession_RoundTrip(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, store.CreateSessionParams{
		ID:        "s1",
		Project:   "ionix",
		Directory: "/tmp/ionix",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID != "s1" {
		t.Errorf("ID: got %q, want %q", sess.ID, "s1")
	}
	if sess.Project != "ionix" {
		t.Errorf("Project: got %q, want %q", sess.Project, "ionix")
	}
	if sess.Directory != "/tmp/ionix" {
		t.Errorf("Directory: got %q, want %q", sess.Directory, "/tmp/ionix")
	}
	if sess.Status != "active" {
		t.Errorf("Status: got %q, want %q", sess.Status, "active")
	}
	if sess.EndedAt != nil {
		t.Errorf("EndedAt: expected nil, got %v", sess.EndedAt)
	}
	if sess.StartedAt.IsZero() {
		t.Error("StartedAt: expected non-zero time")
	}

	got, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("GetSession ID: got %q, want %q", got.ID, sess.ID)
	}
	if got.Status != "active" {
		t.Errorf("GetSession Status: got %q, want active", got.Status)
	}
	if got.EndedAt != nil {
		t.Errorf("GetSession EndedAt: expected nil, got %v", got.EndedAt)
	}
}

// TestCreateSession_DuplicateIDReturnsError verifies that inserting a session
// with an already-existing ID returns a non-nil error.
func TestCreateSession_DuplicateIDReturnsError(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	params := store.CreateSessionParams{ID: "s1", Project: "p", Directory: "/tmp/p"}
	if _, err := s.CreateSession(ctx, params); err != nil {
		t.Fatalf("first CreateSession: %v", err)
	}
	if _, err := s.CreateSession(ctx, params); err == nil {
		t.Fatal("expected error on duplicate ID, got nil")
	}

	// Confirm only one row exists.
	rows, err := s.DB().QueryContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id='s1'")
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	defer rows.Close()
	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	if count != 1 {
		t.Errorf("expected 1 row with id=s1, got %d", count)
	}
}

// TestGetSession_NotFoundReturnsSentinel verifies that GetSession returns
// ErrNotFound for a non-existent session.
func TestGetSession_NotFoundReturnsSentinel(t *testing.T) {
	s := mustOpen(t)
	_, err := s.GetSession(context.Background(), "ghost")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestEndSession_SetsEndedAtAndStatus verifies that EndSession updates the row
// with ended_at, status="ended", and summary.
func TestEndSession_SetsEndedAtAndStatus(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if _, err := s.CreateSession(ctx, store.CreateSessionParams{
		ID: "s1", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.EndSession(ctx, "s1", "done"); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	got, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Status != "ended" {
		t.Errorf("Status: got %q, want ended", got.Status)
	}
	if got.EndedAt == nil {
		t.Error("EndedAt: expected non-nil after EndSession")
	}
	if got.Summary == nil || *got.Summary != "done" {
		t.Errorf("Summary: got %v, want 'done'", got.Summary)
	}
}

// TestEndSession_NotFoundReturnsSentinel verifies that EndSession returns
// ErrNotFound for a non-existent session.
func TestEndSession_NotFoundReturnsSentinel(t *testing.T) {
	s := mustOpen(t)
	err := s.EndSession(context.Background(), "missing", "")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestEndSession_IsIdempotent verifies that calling EndSession twice is a no-op
// (last-write-wins) and returns nil both times.
func TestEndSession_IsIdempotent(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if _, err := s.CreateSession(ctx, store.CreateSessionParams{
		ID: "s1", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.EndSession(ctx, "s1", "first"); err != nil {
		t.Fatalf("first EndSession: %v", err)
	}
	// Small sleep so ended_at timestamps differ if last-write-wins is tested.
	time.Sleep(time.Millisecond)
	if err := s.EndSession(ctx, "s1", "second"); err != nil {
		t.Fatalf("second EndSession: %v", err)
	}

	got, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Summary == nil || *got.Summary != "second" {
		t.Errorf("Summary: got %v, want 'second' (last-write-wins)", got.Summary)
	}
}

// TestRecentSessions_OrderingAndLimit verifies DESC ordering and limit.
func TestRecentSessions_OrderingAndLimit(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	// Insert sessions with explicit started_at to control ordering.
	sessions := []struct {
		id        string
		startedAt string
	}{
		{"s1", "2024-01-01T00:00:00Z"},
		{"s2", "2024-01-02T00:00:00Z"},
		{"s3", "2024-01-03T00:00:00Z"},
	}
	for _, ss := range sessions {
		if _, err := s.CreateSession(ctx, store.CreateSessionParams{
			ID: ss.id, Project: "p", Directory: "/tmp/p",
		}); err != nil {
			t.Fatalf("CreateSession %q: %v", ss.id, err)
		}
		// Override started_at to control ordering.
		if _, err := s.DB().ExecContext(ctx,
			"UPDATE sessions SET started_at=? WHERE id=?", ss.startedAt, ss.id,
		); err != nil {
			t.Fatalf("update started_at %q: %v", ss.id, err)
		}
	}

	got, err := s.RecentSessions(ctx, "", 2)
	if err != nil {
		t.Fatalf("RecentSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}
	if got[0].ID != "s3" {
		t.Errorf("first result: got %q, want s3", got[0].ID)
	}
	if got[1].ID != "s2" {
		t.Errorf("second result: got %q, want s2", got[1].ID)
	}
}

// TestRecentSessions_AllProjectsWhenEmpty verifies that an empty project
// filter returns sessions from all projects.
func TestRecentSessions_AllProjectsWhenEmpty(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	for _, p := range []string{"A", "B"} {
		if _, err := s.CreateSession(ctx, store.CreateSessionParams{
			ID: "sess-" + p, Project: p, Directory: "/tmp/" + p,
		}); err != nil {
			t.Fatalf("CreateSession %q: %v", p, err)
		}
	}

	got, err := s.RecentSessions(ctx, "", 50)
	if err != nil {
		t.Fatalf("RecentSessions: %v", err)
	}
	projects := make(map[string]bool)
	for _, sess := range got {
		projects[sess.Project] = true
	}
	if !projects["A"] || !projects["B"] {
		t.Errorf("expected sessions from both projects, got projects: %v", projects)
	}
}

// TestDeleteSession_Succeeds verifies that DeleteSession removes the row.
func TestDeleteSession_Succeeds(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if _, err := s.CreateSession(ctx, store.CreateSessionParams{
		ID: "s1", Project: "p", Directory: "/tmp/p",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	_, err := s.GetSession(ctx, "s1")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// TestDeleteSession_NotFoundReturnsSentinel verifies that DeleteSession returns
// ErrNotFound for a non-existent session.
func TestDeleteSession_NotFoundReturnsSentinel(t *testing.T) {
	s := mustOpen(t)
	err := s.DeleteSession(context.Background(), "ghost")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
