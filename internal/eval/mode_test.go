package eval_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/eval"
	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// fakeEmbedder is a minimal embed.Embedder for mode.go tests: deterministic,
// no network calls.
type fakeEmbedder struct {
	vectors map[string][]float32
	model   string
	err     error
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	if v, ok := f.vectors[text]; ok {
		return v, nil
	}
	return []float32{1.0, 0.0}, nil
}

func (f *fakeEmbedder) Model() string { return f.model }

// ─── ParseMode ────────────────────────────────────────────────────────────────

func TestParseMode_ValidValues(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"lexical", "vector", "hybrid"} {
		m, err := eval.ParseMode(s)
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error: %v", s, err)
		}
		if string(m) != s {
			t.Errorf("ParseMode(%q) = %q, want %q", s, m, s)
		}
	}
}

func TestParseMode_InvalidValue(t *testing.T) {
	t.Parallel()
	_, err := eval.ParseMode("bm25")
	if err == nil {
		t.Fatal("ParseMode(\"bm25\"): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bm25") {
		t.Errorf("error %q should mention the invalid value", err.Error())
	}
}

func TestParseMode_EmptyIsInvalid(t *testing.T) {
	t.Parallel()
	// ParseMode does not resolve the "" -> default mode decision; callers
	// (the CLI) must resolve a concrete mode before calling it.
	_, err := eval.ParseMode("")
	if err == nil {
		t.Fatal("ParseMode(\"\"): expected error, got nil")
	}
}

// ─── ModeSearchFn ─────────────────────────────────────────────────────────────

func mustOpenEvalStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestModeSearchFn_Lexical_UsesSearchWithFallback(t *testing.T) {
	ctx := context.Background()
	st := mustOpenEvalStore(t)
	proj := "mode-lexical"

	if _, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "s1", Project: proj, Directory: "/x"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "gopher doc", Content: "gopher content",
		Project: proj, Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	search, err := eval.ModeSearchFn(eval.ModeLexical, st, nil, 2.0, 0, hybrid.FusionAll)
	if err != nil {
		t.Fatalf("ModeSearchFn: %v", err)
	}

	results, meta, err := search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one lexical result for 'gopher'")
	}
	if meta.Hybrid {
		t.Error("lexical mode: Meta.Hybrid should always be false")
	}
}

func TestModeSearchFn_Lexical_NilEmbedderIsFine(t *testing.T) {
	st := mustOpenEvalStore(t)
	if _, err := eval.ModeSearchFn(eval.ModeLexical, st, nil, 2.0, 0, hybrid.FusionAll); err != nil {
		t.Errorf("lexical mode must not require an embedder: %v", err)
	}
}

func TestModeSearchFn_Vector_NilEmbedderErrors(t *testing.T) {
	st := mustOpenEvalStore(t)
	_, err := eval.ModeSearchFn(eval.ModeVector, st, nil, 2.0, 0, hybrid.FusionAll)
	if err == nil {
		t.Fatal("vector mode with nil embedder: expected error, got nil")
	}
}

func TestModeSearchFn_Hybrid_NilEmbedderErrors(t *testing.T) {
	st := mustOpenEvalStore(t)
	_, err := eval.ModeSearchFn(eval.ModeHybrid, st, nil, 2.0, 0, hybrid.FusionAll)
	if err == nil {
		t.Fatal("hybrid mode with nil embedder: expected error, got nil")
	}
}

func TestModeSearchFn_UnknownModeErrors(t *testing.T) {
	st := mustOpenEvalStore(t)
	_, err := eval.ModeSearchFn(eval.Mode("bogus"), st, nil, 2.0, 0, hybrid.FusionAll)
	if err == nil {
		t.Fatal("unknown mode: expected error, got nil")
	}
}

func TestModeSearchFn_Vector_EmbedsAndSearchesByModel(t *testing.T) {
	ctx := context.Background()
	st := mustOpenEvalStore(t)
	proj := "mode-vector"

	if _, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "s1", Project: proj, Directory: "/x"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obs, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "no lexical overlap", Content: "totally different words",
		Project: proj, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	queryVec := []float32{0.0, 1.0}
	if err := st.UpsertEmbedding(ctx, obs.ID, "m1", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	embedder := &fakeEmbedder{vectors: map[string][]float32{"gopher": queryVec}, model: "m1"}

	search, err := eval.ModeSearchFn(eval.ModeVector, st, embedder, 2.0, 0, hybrid.FusionAll)
	if err != nil {
		t.Fatalf("ModeSearchFn: %v", err)
	}

	results, meta, err := search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 || results[0].Observation.ID != obs.ID {
		t.Fatalf("vector mode: want obs %d ranked first via embedding, got %+v", obs.ID, results)
	}
	// VectorSearch never sets Meta.Hybrid (that's the hybrid.Searcher's job).
	if meta.Hybrid {
		t.Error("vector mode: Meta.Hybrid should be false")
	}
}

func TestModeSearchFn_Vector_EmbedErrorPropagates(t *testing.T) {
	ctx := context.Background()
	st := mustOpenEvalStore(t)

	embedErr := errors.New("boom")
	embedder := &fakeEmbedder{model: "m1", err: embedErr}

	search, err := eval.ModeSearchFn(eval.ModeVector, st, embedder, 2.0, 0, hybrid.FusionAll)
	if err != nil {
		t.Fatalf("ModeSearchFn: %v", err)
	}

	_, _, searchErr := search(ctx, store.SearchParams{Q: "gopher", Limit: 5})
	if searchErr == nil {
		t.Fatal("vector mode: embed error should propagate (no silent fallback), got nil error")
	}
}

func TestModeSearchFn_Hybrid_UsesConfiguredWeightAndTimeout(t *testing.T) {
	ctx := context.Background()
	st := mustOpenEvalStore(t)
	proj := "mode-hybrid"

	if _, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "s1", Project: proj, Directory: "/x"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "gopher doc", Content: "gopher content",
		Project: proj, Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	embedder := &fakeEmbedder{vectors: map[string][]float32{"gopher": {1.0, 0.0}}, model: "m1"}

	search, err := eval.ModeSearchFn(eval.ModeHybrid, st, embedder, 2.0, 5*time.Second, hybrid.FusionAll)
	if err != nil {
		t.Fatalf("ModeSearchFn: %v", err)
	}

	results, meta, err := search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one hybrid result")
	}
	if !meta.Hybrid {
		t.Error("hybrid mode: Meta.Hybrid should be true when the embedder succeeds")
	}
}

func TestModeSearchFn_Hybrid_DefaultEmbedTimeoutIsTenSeconds(t *testing.T) {
	if eval.DefaultEmbedTimeout != 10*time.Second {
		t.Errorf("eval.DefaultEmbedTimeout = %v, want 10s", eval.DefaultEmbedTimeout)
	}
}

// ─── FusionPolicy wiring ────────────────────────────────────────────────────

// TestModeSearchFn_Hybrid_FusionStrict_SkipsFuzzyLexical verifies that the
// fusionPolicy argument reaches the underlying hybrid.Searcher: with
// FusionStrict and a fuzzy OR-fallback lexical hit, the SearchFn must return
// SearchMeta{Hybrid: false, LexicalSkipped: true} and drop the lexical-only
// doc from the results, exactly like hybrid.Searcher does directly (see
// internal/hybrid's own FusionPolicy tests for the underlying mechanism).
func TestModeSearchFn_Hybrid_FusionStrict_SkipsFuzzyLexical(t *testing.T) {
	ctx := context.Background()
	st := mustOpenEvalStore(t)
	proj := "mode-fusion-strict"

	if _, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "s1", Project: proj, Directory: "/x"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	lexDoc, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "widget lexical doc", Content: "widget content here",
		Project: proj, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation lexDoc: %v", err)
	}
	semDoc, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "totally unrelated wording", Content: "no shared terms",
		Project: proj, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation semDoc: %v", err)
	}
	queryVec := []float32{0.0, 1.0}
	if err := st.UpsertEmbedding(ctx, semDoc.ID, "m1", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	// Two terms, one nonexistent: forces the OR fuzzy fallback (fuzzy=true).
	embedder := &fakeEmbedder{vectors: map[string][]float32{"widget zzznonexistentterm": queryVec}, model: "m1"}

	search, err := eval.ModeSearchFn(eval.ModeHybrid, st, embedder, 1.0, 0, hybrid.FusionStrict)
	if err != nil {
		t.Fatalf("ModeSearchFn: %v", err)
	}

	results, meta, err := search(ctx, store.SearchParams{Q: "widget zzznonexistentterm", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if meta.Hybrid {
		t.Error("FusionStrict + fuzzy: SearchMeta.Hybrid should be false")
	}
	if !meta.LexicalSkipped {
		t.Error("FusionStrict + fuzzy: SearchMeta.LexicalSkipped should be true")
	}
	for _, r := range results {
		if r.Observation.ID == lexDoc.ID {
			t.Errorf("FusionStrict + fuzzy: lexical doc must not appear: %+v", results)
		}
	}
}

// TestModeSearchFn_Lexical_NeverSetsLexicalSkipped verifies ModeLexical
// always returns LexicalSkipped=false — FusionPolicy only applies to hybrid.
func TestModeSearchFn_Lexical_NeverSetsLexicalSkipped(t *testing.T) {
	st := mustOpenEvalStore(t)
	search, err := eval.ModeSearchFn(eval.ModeLexical, st, nil, 2.0, 0, hybrid.FusionStrict)
	if err != nil {
		t.Fatalf("ModeSearchFn: %v", err)
	}
	_, meta, err := search(context.Background(), store.SearchParams{Q: "anything", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if meta.LexicalSkipped {
		t.Error("lexical mode: SearchMeta.LexicalSkipped should always be false")
	}
}
