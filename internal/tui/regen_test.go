package tui

// regen_test.go — Tests for the regenerateAll loop helper.
//
// Uses a fake embed.Embedder (no Ollama) and a real temporary store to exercise
// the full DeleteAllEmbeddings → MissingEmbeddings → Embed → UpsertEmbedding
// loop without any network dependency.
//
// TDD cycle:
//  1. TestRegenerateAll_EmbedsMissingObservations — happy path: 2 obs embedded.
//  2. TestRegenerateAll_EmptyStore — no obs, done=0, total=0, no error.
//  3. TestRegenerateAll_EmbedderError — embedder always fails; done=0, no fatal error.
//  4. TestRegenerateAll_ClearsExistingEmbeddingsFirst — pre-seeded embeddings are removed.

import (
	"context"
	"errors"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── fake embedder ───────────────────────────────────────────────────────────

// fakeEmbedder is a no-Ollama embed.Embedder for unit tests.
// It returns a fixed vector for every call (or an error when errOnEmbed is set).
type fakeEmbedder struct {
	modelName  string
	vec        []float32
	errOnEmbed error
	callCount  int
}

func (f *fakeEmbedder) Model() string { return f.modelName }

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	f.callCount++
	if f.errOnEmbed != nil {
		return nil, f.errOnEmbed
	}
	return f.vec, nil
}

// ─── store helper ────────────────────────────────────────────────────────────

// openRegenStore opens a fresh store backed by t.TempDir and registers cleanup.
func openRegenStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("openRegenStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// seedRegenObs inserts a minimal observation and returns its ID.
func seedRegenObs(t *testing.T, st *store.Store, title, project string) int64 {
	t.Helper()
	ctx := context.Background()
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID:        "regen-sess-" + title,
		Project:   project,
		Directory: "/regen",
	})
	obs, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "regen-sess-" + title,
		Type:      "manual",
		Title:     title,
		Content:   "content for " + title,
		Project:   project,
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("seedRegenObs: %v", err)
	}
	return obs.ID
}

// ─── 1. Happy path ───────────────────────────────────────────────────────────

func TestRegenerateAll_EmbedsMissingObservations(t *testing.T) {
	ctx := context.Background()
	st := openRegenStore(t)

	id1 := seedRegenObs(t, st, "regen-obs-1", "proj-r")
	id2 := seedRegenObs(t, st, "regen-obs-2", "proj-r")

	embedder := &fakeEmbedder{
		modelName: "fake-model",
		vec:       []float32{0.1, 0.2, 0.3},
	}

	done, total, err := regenerateAll(ctx, st, embedder)
	if err != nil {
		t.Fatalf("regenerateAll: unexpected error: %v", err)
	}
	if done != 2 {
		t.Errorf("regenerateAll done = %d, want 2", done)
	}
	if total != 2 {
		t.Errorf("regenerateAll total = %d, want 2", total)
	}

	// Verify both observations now have embeddings via VectorSearch.
	results, err := st.VectorSearch(ctx, []float32{0.1, 0.2, 0.3}, store.SearchParams{Project: "proj-r", Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch after regen: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("VectorSearch: got %d results, want 2", len(results))
	}
	ids := map[int64]bool{}
	for _, r := range results {
		ids[r.Observation.ID] = true
	}
	if !ids[id1] || !ids[id2] {
		t.Errorf("VectorSearch: expected both id1=%d and id2=%d in results; got %v", id1, id2, ids)
	}
}

// ─── 2. Empty store ──────────────────────────────────────────────────────────

func TestRegenerateAll_EmptyStore(t *testing.T) {
	ctx := context.Background()
	st := openRegenStore(t)

	embedder := &fakeEmbedder{modelName: "fake-model", vec: []float32{0.1}}

	done, total, err := regenerateAll(ctx, st, embedder)
	if err != nil {
		t.Fatalf("regenerateAll on empty store: %v", err)
	}
	if done != 0 {
		t.Errorf("regenerateAll empty: done = %d, want 0", done)
	}
	if total != 0 {
		t.Errorf("regenerateAll empty: total = %d, want 0", total)
	}
	if embedder.callCount != 0 {
		t.Errorf("regenerateAll empty: embedder called %d times, want 0", embedder.callCount)
	}
}

// ─── 3. Embedder error (partial) ────────────────────────────────────────────

func TestRegenerateAll_EmbedderError(t *testing.T) {
	ctx := context.Background()
	st := openRegenStore(t)

	seedRegenObs(t, st, "err-obs-1", "proj-err")
	seedRegenObs(t, st, "err-obs-2", "proj-err")

	embedder := &fakeEmbedder{
		modelName:  "fake-model",
		errOnEmbed: errors.New("fake embed failure"),
	}

	done, total, err := regenerateAll(ctx, st, embedder)
	if err != nil {
		// regenerateAll itself should not return an error for per-obs embed failures.
		t.Fatalf("regenerateAll with all-fail embedder: unexpected error: %v", err)
	}
	// All embeds failed — done should be 0.
	if done != 0 {
		t.Errorf("regenerateAll all-fail: done = %d, want 0", done)
	}
	// total reflects the observations that exist (2), even though none were embedded.
	if total != 2 {
		t.Errorf("regenerateAll all-fail: total = %d, want 2", total)
	}
}

// ─── 4. Clears existing embeddings first ─────────────────────────────────────

func TestRegenerateAll_ClearsExistingEmbeddingsFirst(t *testing.T) {
	ctx := context.Background()
	st := openRegenStore(t)
	model := "fake-model"

	id1 := seedRegenObs(t, st, "clear-obs-1", "proj-c")

	// Pre-seed a stale embedding with a different vector.
	if err := st.UpsertEmbedding(ctx, id1, model, []float32{9.9, 9.9}); err != nil {
		t.Fatalf("UpsertEmbedding pre-seed: %v", err)
	}

	embedder := &fakeEmbedder{
		modelName: model,
		vec:       []float32{0.1, 0.2},
	}

	done, total, err := regenerateAll(ctx, st, embedder)
	if err != nil {
		t.Fatalf("regenerateAll: %v", err)
	}
	if done != 1 || total != 1 {
		t.Errorf("regenerateAll: done=%d total=%d, want 1/1", done, total)
	}

	// The stale embedding should have been cleared and replaced.
	// VectorSearch with the new vector (0.1, 0.2) should find id1.
	results, err := st.VectorSearch(ctx, []float32{0.1, 0.2}, store.SearchParams{Project: "proj-c", Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch after clear+regen: %v", err)
	}
	if len(results) == 0 || results[0].Observation.ID != id1 {
		t.Errorf("VectorSearch after clear+regen: expected id1=%d as top result, got %v", id1, results)
	}
}
