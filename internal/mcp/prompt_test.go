package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// mustPromptSession creates a session in the store with a unique ID and returns it.
func mustPromptSession(t *testing.T, st *store.Store, project string) store.Session {
	t.Helper()
	sess, err := st.CreateSession(context.Background(), store.CreateSessionParams{
		ID:        "mcp-prompt-sess-" + t.Name(),
		Project:   project,
		Directory: "/tmp/" + project,
	})
	if err != nil {
		t.Fatalf("mustPromptSession: %v", err)
	}
	return sess
}

// TestConsumeFromStore_FoundAfterAdd verifies that a prompt added via
// AddPromptIfMissing is returned by ConsumeLatestPrompt and marked consumed.
func TestConsumeFromStore_FoundAfterAdd(t *testing.T) {
	st := mustTestStore(t)
	sess := mustPromptSession(t, st, "proj")
	ctx := context.Background()

	_, err := st.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID,
		Content:   "the user prompt",
		Project:   "proj",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}

	p, found, err := st.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt: %v", err)
	}
	if !found {
		t.Fatal("expected found=true, got false")
	}
	if p.Content != "the user prompt" {
		t.Fatalf("Content=%q, want %q", p.Content, "the user prompt")
	}
}

// TestConsumeFromStore_DoubleConsumeFalse verifies the single-consumption
// guarantee: a second ConsumeLatestPrompt on the same session returns false.
func TestConsumeFromStore_DoubleConsumeFalse(t *testing.T) {
	st := mustTestStore(t)
	sess := mustPromptSession(t, st, "proj")
	ctx := context.Background()

	_, err := st.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "only once", Project: "proj",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}

	_, found1, err := st.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("first ConsumeLatestPrompt: %v", err)
	}
	if !found1 {
		t.Fatal("first consume: expected found=true")
	}

	_, found2, err := st.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("second ConsumeLatestPrompt: %v", err)
	}
	if found2 {
		t.Fatal("second consume: expected found=false (already consumed)")
	}
}

// TestConsumeFromStore_RestartDurability verifies that a prompt written before
// a simulated process restart (close + reopen of the store) is still consumable
// afterward — consistent with spec R-TOOL-SAVE-04 process-restart scenario.
func TestConsumeFromStore_RestartDurability(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// First store: write the prompt.
	st1, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("Open st1: %v", err)
	}
	sess, err := st1.CreateSession(ctx, store.CreateSessionParams{
		ID:        "restart-sess",
		Project:   "proj",
		Directory: "/tmp/proj",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = st1.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "durable prompt", Project: "proj",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("Close st1: %v", err)
	}

	// Second store (simulated restart): consume the prompt.
	st2, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("Open st2: %v", err)
	}
	defer st2.Close()

	p, found, err := st2.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt after restart: %v", err)
	}
	if !found {
		t.Fatal("expected found=true after restart, got false")
	}
	if p.Content != "durable prompt" {
		t.Fatalf("Content=%q, want %q", p.Content, "durable prompt")
	}
}

// TestConsumeFromStore_DedupConsumedInteraction verifies that after a dedup
// collision with an already-consumed row, ConsumeLatestPrompt returns false —
// consistent with spec R-S2-SESSION-02.
func TestConsumeFromStore_DedupConsumedInteraction(t *testing.T) {
	st := mustTestStore(t)
	sess := mustPromptSession(t, st, "proj")
	ctx := context.Background()

	// Add and immediately consume a prompt.
	_, err := st.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "original", Project: "proj",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing: %v", err)
	}
	_, found, err := st.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt: %v", err)
	}
	if !found {
		t.Fatal("expected found=true on first consume")
	}

	// Re-add same content — dedup returns existing (consumed) row.
	_, err = st.AddPromptIfMissing(ctx, store.AddPromptParams{
		SessionID: sess.ID, Content: "original", Project: "proj",
	})
	if err != nil {
		t.Fatalf("AddPromptIfMissing (dedup): %v", err)
	}

	// ConsumeLatestPrompt must return false: only row is already consumed.
	_, found2, err := st.ConsumeLatestPrompt(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ConsumeLatestPrompt after dedup: %v", err)
	}
	if found2 {
		t.Fatal("expected found=false after dedup with consumed row, got true")
	}
}
