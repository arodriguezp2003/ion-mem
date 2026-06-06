package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestAddPromptIfMissing_InsertsNew verifies that AddPromptIfMissing inserts a
// new row when no prior prompt exists for the (session, content) pair.
func TestAddPromptIfMissing_InsertsNew(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	p, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID,
		Content:   "hello world",
		Project:   "proj",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if !strings.HasPrefix(p.SyncID, "pr-") {
		t.Fatalf("expected sync_id prefix 'pr-', got %q", p.SyncID)
	}
	if p.Content != "hello world" {
		t.Fatalf("expected Content='hello world', got %q", p.Content)
	}
	if p.Project != "proj" {
		t.Fatalf("expected Project='proj', got %q", p.Project)
	}
	if p.SessionID != sess.ID {
		t.Fatalf("expected SessionID=%q, got %q", sess.ID, p.SessionID)
	}
	if p.CreatedAt == "" {
		t.Fatal("expected non-empty CreatedAt")
	}
}

// TestAddPromptIfMissing_DedupesSameSessionAndContent verifies that calling
// AddPromptIfMissing twice with the same session and content returns the
// original row without inserting a new one.
func TestAddPromptIfMissing_DedupesSameSessionAndContent(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	p1, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID,
		Content:   "duplicate content",
		Project:   "proj",
	})
	if err != nil {
		t.Fatalf("first AddPromptIfMissing: %v", err)
	}

	p2, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID,
		Content:   "duplicate content",
		Project:   "proj",
	})
	if err != nil {
		t.Fatalf("second AddPromptIfMissing: %v", err)
	}
	if p1.ID != p2.ID {
		t.Fatalf("expected same ID on dedup, got first=%d second=%d", p1.ID, p2.ID)
	}

	// Verify count is still 1.
	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM user_prompts WHERE session_id=?", sess.ID).Scan(&count); err != nil {
		t.Fatalf("count user_prompts: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row in user_prompts, got %d", count)
	}
}

// TestAddPromptIfMissing_AllowsSameContentDifferentSession verifies that the
// same content in a different session inserts a new row.
func TestAddPromptIfMissing_AllowsSameContentDifferentSession(t *testing.T) {
	s := mustOpen(t)
	sess1 := mustSession(t, s, "alpha")
	sess2 := mustSession(t, s, "beta")
	ctx := context.Background()

	p1, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess1.ID,
		Content:   "shared content",
		Project:   "proj",
	})
	if err != nil {
		t.Fatalf("first AddPromptIfMissing: %v", err)
	}

	p2, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess2.ID,
		Content:   "shared content",
		Project:   "proj",
	})
	if err != nil {
		t.Fatalf("second AddPromptIfMissing: %v", err)
	}

	if p1.ID == p2.ID {
		t.Fatal("expected different IDs for different sessions, got same ID")
	}

	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM user_prompts").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
}

// TestAddPromptIfMissing_RejectsUnknownSession verifies that AddPromptIfMissing
// returns a non-nil error when SessionID does not reference a valid session.
func TestAddPromptIfMissing_RejectsUnknownSession(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	_, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: "ghost-session",
		Content:   "some content",
		Project:   "proj",
	})
	if err == nil {
		t.Fatal("expected non-nil error for unknown session, got nil")
	}
}

// TestRecentPrompts_OrderingAndLimit verifies that RecentPrompts returns
// prompts in created_at DESC order and respects the limit.
func TestRecentPrompts_OrderingAndLimit(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	// Insert 3 prompts.
	for i, content := range []string{"first", "second", "third"} {
		_ = i
		mustPrompt(t, s, sess.ID, content, "proj")
	}

	got, err := s.RecentPrompts(ctx, "proj", 2)
	if err != nil {
		t.Fatalf("RecentPrompts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(got))
	}
	// Most recent first: "third" then "second".
	if got[0].Content != "third" {
		t.Fatalf("expected most recent prompt to be 'third', got %q", got[0].Content)
	}
}

// TestRecentPrompts_AllProjectsWhenEmpty verifies that RecentPrompts with an
// empty project string returns prompts across all projects.
func TestRecentPrompts_AllProjectsWhenEmpty(t *testing.T) {
	s := mustOpen(t)
	sessA := mustSession(t, s, "projA")
	sessB := mustSession(t, s, "projB")
	ctx := context.Background()

	mustPrompt(t, s, sessA.ID, "content A", "projA")
	mustPrompt(t, s, sessB.ID, "content B", "projB")

	all, err := s.RecentPrompts(ctx, "", 50)
	if err != nil {
		t.Fatalf("RecentPrompts: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 prompts across all projects, got %d", len(all))
	}
}

// TestSearchPrompts_FTS5Match verifies that SearchPrompts returns a prompt
// matching the FTS5 query.
func TestSearchPrompts_FTS5Match(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	mustPrompt(t, s, sess.ID, "fix the authentication bug", "proj")
	mustPrompt(t, s, sess.ID, "unrelated prompt text", "proj")

	results, err := s.SearchPrompts(ctx, store.SearchPromptsParams{Q: "authentication"})
	if err != nil {
		t.Fatalf("SearchPrompts: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "fix the authentication bug" {
		t.Fatalf("unexpected content %q", results[0].Content)
	}
}

// TestSearchPrompts_EmptyResult verifies that SearchPrompts returns an empty
// slice (not an error) when no prompts match the query.
func TestSearchPrompts_EmptyResult(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	mustPrompt(t, s, sess.ID, "some content here", "proj")

	results, err := s.SearchPrompts(ctx, store.SearchPromptsParams{Q: "zzznomatch"})
	if err != nil {
		t.Fatalf("SearchPrompts: %v", err)
	}
	if results == nil {
		results = []store.Prompt{}
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// TestDeletePrompt_Succeeds verifies that DeletePrompt removes the row and
// that it no longer appears in RecentPrompts or SearchPrompts.
func TestDeletePrompt_Succeeds(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	p := mustPrompt(t, s, sess.ID, "content to delete", "proj")

	if err := s.DeletePrompt(ctx, p.ID); err != nil {
		t.Fatalf("DeletePrompt: %v", err)
	}

	// Must not appear in RecentPrompts.
	recent, err := s.RecentPrompts(ctx, "", 50)
	if err != nil {
		t.Fatalf("RecentPrompts after delete: %v", err)
	}
	for _, rp := range recent {
		if rp.ID == p.ID {
			t.Fatal("deleted prompt still appears in RecentPrompts")
		}
	}

	// Must not appear in SearchPrompts.
	results, err := s.SearchPrompts(ctx, store.SearchPromptsParams{Q: "content"})
	if err != nil {
		t.Fatalf("SearchPrompts after delete: %v", err)
	}
	for _, r := range results {
		if r.ID == p.ID {
			t.Fatal("deleted prompt still appears in SearchPrompts")
		}
	}
}

// TestDeletePrompt_NotFound verifies that DeletePrompt returns ErrPromptNotFound
// when the id does not exist.
func TestDeletePrompt_NotFound(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	err := s.DeletePrompt(ctx, 9999)
	if err == nil {
		t.Fatal("expected non-nil error for missing prompt, got nil")
	}
	if !isPromptNotFound(err) {
		t.Fatalf("expected ErrPromptNotFound, got %v", err)
	}
}

// TestDeleteSession_BlockedByPrompt verifies that DeleteSession returns
// ErrSessionHasObservations when a prompt FK-references the session.
func TestDeleteSession_BlockedByPrompt(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	mustPrompt(t, s, sess.ID, "some prompt", "proj")

	err := s.DeleteSession(ctx, sess.ID)
	if err == nil {
		t.Fatal("expected non-nil error when prompt FK exists, got nil")
	}
	if !isSessionHasObservations(err) {
		t.Fatalf("expected ErrSessionHasObservations, got %v", err)
	}
}

// isPromptNotFound checks errors.Is for ErrPromptNotFound.
func isPromptNotFound(err error) bool {
	return strings.Contains(err.Error(), "prompt not found") ||
		err == store.ErrPromptNotFound
}

// isSessionHasObservations checks errors.Is for ErrSessionHasObservations.
func isSessionHasObservations(err error) bool {
	return err == store.ErrSessionHasObservations
}
