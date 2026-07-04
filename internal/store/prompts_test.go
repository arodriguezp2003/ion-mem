package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// ---------------------------------------------------------------------------
// ConsumeLatestPrompt tests (Phase 2 — Slice 2)
// ---------------------------------------------------------------------------

// TestConsumeLatestPrompt_Found verifies that ConsumeLatestPrompt returns the
// unconsumed prompt and marks it consumed (consumed_at IS NOT NULL).
func TestConsumeLatestPrompt_Found(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	inserted := mustPrompt(t, s, sess.ID, "first prompt", "proj")

	p, found, err := s.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt: %v", err)
	}
	if !found {
		t.Fatal("expected found=true, got false")
	}
	if p.ID != inserted.ID {
		t.Fatalf("returned prompt ID=%d, want %d", p.ID, inserted.ID)
	}
	if p.Content != "first prompt" {
		t.Fatalf("returned Content=%q, want %q", p.Content, "first prompt")
	}

	// Direct SQL check: consumed_at must now be non-null.
	var consumedAt *string
	if err := s.DB().QueryRow(
		"SELECT consumed_at FROM user_prompts WHERE id=?", inserted.ID,
	).Scan(&consumedAt); err != nil {
		t.Fatalf("scan consumed_at: %v", err)
	}
	if consumedAt == nil {
		t.Fatal("expected consumed_at to be non-null after ConsumeLatestPrompt, got NULL")
	}
}

// TestConsumeLatestPrompt_NotFound verifies that ConsumeLatestPrompt returns
// (Prompt{}, false, nil) when no unconsumed row exists for the session.
func TestConsumeLatestPrompt_NotFound(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	// No prompt inserted — no unconsumed row.
	p, found, err := s.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt: unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false, got true with prompt %+v", p)
	}
	if p.ID != 0 {
		t.Fatalf("expected zero Prompt, got ID=%d", p.ID)
	}
}

// TestConsumeLatestPrompt_AlreadyConsumed verifies that a second call for the
// same session returns (Prompt{}, false, nil) — consumed row is not re-consumed.
func TestConsumeLatestPrompt_AlreadyConsumed(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	mustPrompt(t, s, sess.ID, "once prompt", "proj")

	// First consume — must succeed.
	_, found1, err := s.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("first ConsumeLatestPrompt: %v", err)
	}
	if !found1 {
		t.Fatal("first consume: expected found=true")
	}

	// Second consume — no unconsumed rows remain.
	p, found2, err := s.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("second ConsumeLatestPrompt: %v", err)
	}
	if found2 {
		t.Fatalf("second consume: expected found=false, got true with prompt %+v", p)
	}
}

// TestConsumeLatestPrompt_LatestSelected verifies that when two unconsumed rows
// exist, ConsumeLatestPrompt returns the one with the later created_at and leaves
// the older row unconsumed so a subsequent call returns it.
func TestConsumeLatestPrompt_LatestSelected(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	// Insert two distinct prompts; both are unconsumed.
	older := mustPrompt(t, s, sess.ID, "older prompt", "proj")
	newer := mustPrompt(t, s, sess.ID, "newer prompt", "proj")

	// First consume: must return the newest.
	p1, found1, err := s.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("first ConsumeLatestPrompt: %v", err)
	}
	if !found1 {
		t.Fatal("first consume: expected found=true")
	}
	if p1.ID != newer.ID {
		t.Fatalf("expected newest ID=%d consumed first, got ID=%d", newer.ID, p1.ID)
	}

	// Second consume: must return the older one (still unconsumed).
	p2, found2, err := s.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("second ConsumeLatestPrompt: %v", err)
	}
	if !found2 {
		t.Fatal("second consume: expected found=true (older prompt still unconsumed)")
	}
	if p2.ID != older.ID {
		t.Fatalf("expected older ID=%d on second consume, got ID=%d", older.ID, p2.ID)
	}
}

// TestMigration0004_Applies verifies that migration 0004 adds consumed_at to
// user_prompts and is recorded in schema_version.
func TestMigration0004_Applies(t *testing.T) {
	s := mustOpen(t)

	// schema_version must include 4.
	v := s.SchemaVersion()
	if v < 4 {
		t.Fatalf("expected SchemaVersion >= 4, got %d", v)
	}

	// Verify all four versions are recorded.
	rows, err := s.DB().Query("SELECT version FROM schema_version ORDER BY version")
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var ver int
		if err := rows.Scan(&ver); err != nil {
			rows.Close()
			t.Fatalf("scan schema_version: %v", err)
		}
		versions = append(versions, ver)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("schema_version rows.Err: %v", err)
	}
	for _, want := range []int{1, 2, 3, 4} {
		if !containsInt(versions, want) {
			t.Fatalf("expected version %d in schema_version, got: %v", want, versions)
		}
	}

	// PRAGMA table_info must include consumed_at.
	found := false
	infoRows, err := s.DB().Query("PRAGMA table_info(user_prompts)")
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer infoRows.Close()
	for infoRows.Next() {
		var cid int
		var name, colType, notnull, dflt string
		var pk int
		_ = infoRows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk)
		if name == "consumed_at" {
			found = true
		}
	}
	if !found {
		t.Fatal("PRAGMA table_info(user_prompts) did not include consumed_at after migration 0004")
	}
}

// TestMigration0004_ExistingRowsNullConsumedAt verifies that rows inserted
// before migration 0004 have consumed_at=NULL after the migration applies.
//
// We cannot easily simulate this in a unit test by inserting before migration 0004
// since store.Open runs all migrations. Instead we verify that new rows have
// consumed_at=NULL immediately after insert (no automatic timestamp set).
func TestMigration0004_ExistingRowsNullConsumedAt(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	inserted := mustPrompt(t, s, sess.ID, "pre-consume content", "proj")

	// Before any consume call, consumed_at must be NULL.
	var consumedAt *string
	if err := s.DB().QueryRow(
		"SELECT consumed_at FROM user_prompts WHERE id=?", inserted.ID,
	).Scan(&consumedAt); err != nil {
		t.Fatalf("scan consumed_at: %v", err)
	}
	if consumedAt != nil {
		t.Fatalf("expected consumed_at=NULL for freshly inserted prompt, got %q", *consumedAt)
	}

	// ConsumeLatestPrompt must NOT be called here — this test checks the default.
	_ = ctx
}

// TestConsumeLatestPrompt_RestartDurability verifies that a prompt written by
// one Store instance is consumable after Close+reopen on the same directory.
func TestConsumeLatestPrompt_RestartDurability(t *testing.T) {
	dir := t.TempDir()

	// First store instance: insert a session and a prompt.
	s1, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open s1: %v", err)
	}
	ctx := context.Background()
	sess, err := s1.CreateSession(ctx, store.CreateSessionParams{
		ID:        "durable-sess",
		Project:   "proj",
		Directory: "/tmp/proj",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	p1, err := s1.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID,
		Content:   "durable prompt",
		Project:   "proj",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close s1: %v", err)
	}

	// Second store instance (simulated restart): consume the prompt.
	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open s2: %v", err)
	}
	defer s2.Close()

	p2, found, err := s2.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt after restart: %v", err)
	}
	if !found {
		t.Fatal("expected found=true after restart, got false")
	}
	if p2.ID != p1.ID {
		t.Fatalf("expected prompt ID=%d after restart, got %d", p1.ID, p2.ID)
	}
}

// TestConsumeLatestPrompt_DedupAndConsumedInteraction verifies the spec scenario:
// AddPromptIfMissing twice same content → one row; after consume, re-add same
// content → still one row, stays consumed.
func TestConsumeLatestPrompt_DedupAndConsumedInteraction(t *testing.T) {
	s := mustOpen(t)
	sess := mustSession(t, s, "proj")
	ctx := context.Background()

	// Insert twice — dedup yields one row.
	p1, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "same content", Project: "proj",
	})
	if err != nil {
		t.Fatalf("first AddPromptIfMissing: %v", err)
	}
	p2, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "same content", Project: "proj",
	})
	if err != nil {
		t.Fatalf("second AddPromptIfMissing: %v", err)
	}
	if p1.ID != p2.ID {
		t.Fatalf("dedup: expected same ID, got %d and %d", p1.ID, p2.ID)
	}
	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM user_prompts WHERE session_id=?", sess.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after dedup, got %d", count)
	}

	// Consume the row.
	_, found, err := s.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt: %v", err)
	}
	if !found {
		t.Fatal("expected found=true, got false")
	}

	// Re-add same content — dedup returns existing row (consumed), no new insert.
	p3, err := s.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "same content", Project: "proj",
	})
	if err != nil {
		t.Fatalf("third AddPromptIfMissing: %v", err)
	}
	if p3.ID != p1.ID {
		t.Fatalf("expected same row returned by dedup after consume, got ID=%d", p3.ID)
	}

	// Still only one row in the table.
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM user_prompts WHERE session_id=?", sess.ID).Scan(&count); err != nil {
		t.Fatalf("count after re-add: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after re-add same content, got %d", count)
	}

	// consumed_at is still set (row remains consumed).
	var consumedAt *string
	if err := s.DB().QueryRow("SELECT consumed_at FROM user_prompts WHERE id=?", p1.ID).Scan(&consumedAt); err != nil {
		t.Fatalf("scan consumed_at: %v", err)
	}
	if consumedAt == nil {
		t.Fatal("expected consumed_at non-null after re-add same content, row should still be consumed")
	}
}

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
