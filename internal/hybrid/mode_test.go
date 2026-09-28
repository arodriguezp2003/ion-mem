package hybrid_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// ─── ParseMode ────────────────────────────────────────────────────────────────

func TestParseMode_ValidValues(t *testing.T) {
	for _, s := range []string{"lexical", "vector", "hybrid"} {
		m, err := hybrid.ParseMode(s)
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error: %v", s, err)
		}
		if string(m) != s {
			t.Errorf("ParseMode(%q) = %q, want %q", s, m, s)
		}
	}
}

func TestParseMode_InvalidValue(t *testing.T) {
	_, err := hybrid.ParseMode("bm25")
	if err == nil {
		t.Fatal("ParseMode(\"bm25\"): expected error, got nil")
	}
}

// ─── failingEmbedder: records calls, always errors ──────────────────────────

// recordingEmbedder wraps fakeEmbedder-like behavior but records whether
// Embed was ever called, so lexical-mode tests can assert the vector path
// was never touched.
type recordingEmbedder struct {
	vectors map[string][]float32
	model   string
	called  bool
	failErr error
}

func (r *recordingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	r.called = true
	if r.failErr != nil {
		return nil, r.failErr
	}
	if v, ok := r.vectors[text]; ok {
		return v, nil
	}
	return []float32{1.0, 0.0}, nil
}

func (r *recordingEmbedder) Model() string { return r.model }

// ─── ModeLexical ──────────────────────────────────────────────────────────────

func TestSearcherWithMode_Lexical_NeverCallsEmbedder(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "mode-lexical-proj"

	lexicalID, _ := seedForHybridTest(t, st, proj)

	embedder := &recordingEmbedder{model: "test-model"}
	searcher := hybrid.NewSearcher(st, embedder).WithMode(hybrid.ModeLexical)

	results, meta, err := searcher.Search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if embedder.called {
		t.Error("ModeLexical must never call the embedder")
	}
	if meta.Mode != hybrid.ModeLexical || meta.Effective != hybrid.ModeLexical {
		t.Errorf("meta.Mode/Effective = %q/%q, want lexical/lexical", meta.Mode, meta.Effective)
	}
	if meta.Degraded {
		t.Error("ModeLexical is never degraded")
	}
	found := false
	for _, r := range results {
		if r.Observation.ID == lexicalID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected lexical doc %d in results: %+v", lexicalID, results)
	}
}

// ─── ModeVector ───────────────────────────────────────────────────────────────

func TestSearcherWithMode_Vector_NoBM25AtAll(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "mode-vector-proj"

	lexicalID, semanticID := seedForHybridTest(t, st, proj)

	// Only semanticID gets an embedding; lexicalID does not, so it can never
	// surface via VectorSearch — proving BM25 was not consulted.
	queryVec := []float32{0.0, 1.0}
	if err := st.UpsertEmbedding(ctx, semanticID, "test-model", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	embedder := &recordingEmbedder{vectors: map[string][]float32{"gopher": queryVec}, model: "test-model"}
	searcher := hybrid.NewSearcher(st, embedder).WithMode(hybrid.ModeVector)

	results, meta, err := searcher.Search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !embedder.called {
		t.Fatal("ModeVector must call the embedder")
	}
	if meta.Mode != hybrid.ModeVector || meta.Effective != hybrid.ModeVector {
		t.Errorf("meta.Mode/Effective = %q/%q, want vector/vector", meta.Mode, meta.Effective)
	}
	if meta.Degraded {
		t.Error("meta.Degraded should be false on a clean vector search")
	}
	for _, r := range results {
		if r.Observation.ID == lexicalID {
			t.Errorf("lexicalID %d must NOT appear — it has no embedding, so ModeVector's pure vector search can't find it (BM25 was not consulted)", lexicalID)
		}
	}
	foundSemantic := false
	for _, r := range results {
		if r.Observation.ID == semanticID {
			foundSemantic = true
			if r.Snippet == "" {
				t.Error("vector-only result must carry a non-empty Snippet")
			}
		}
	}
	if !foundSemantic {
		t.Errorf("expected semantic doc %d in results: %+v", semanticID, results)
	}
}

func TestSearcherWithMode_Vector_FallsBackOnEmbedError(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "mode-vector-fallback-proj"

	lexicalID, _ := seedForHybridTest(t, st, proj)

	embedder := &recordingEmbedder{model: "test-model", failErr: errors.New("embed: connection refused")}
	searcher := hybrid.NewSearcher(st, embedder).WithMode(hybrid.ModeVector)

	results, meta, err := searcher.Search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !meta.Degraded {
		t.Error("meta.Degraded should be true when the embed call fails")
	}
	if meta.Mode != hybrid.ModeVector || meta.Effective != hybrid.ModeLexical {
		t.Errorf("meta.Mode/Effective = %q/%q, want vector/lexical", meta.Mode, meta.Effective)
	}
	if !strings.Contains(meta.Reason, "connection refused") {
		t.Errorf("meta.Reason = %q, want it to mention the embed error", meta.Reason)
	}
	found := false
	for _, r := range results {
		if r.Observation.ID == lexicalID {
			found = true
		}
	}
	if !found {
		t.Errorf("degraded fallback should still find lexical doc %d via BM25: %+v", lexicalID, results)
	}
}

func TestSearcherWithMode_Vector_NilEmbedderDegrades(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "mode-vector-nil-proj"
	seedForHybridTest(t, st, proj)

	searcher := hybrid.NewSearcher(st, nil).WithMode(hybrid.ModeVector)
	_, meta, err := searcher.Search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !meta.Degraded || meta.Effective != hybrid.ModeLexical {
		t.Errorf("meta = %+v, want Degraded=true, Effective=lexical", meta)
	}
}

// ─── ModeHybrid (strict, AND-only) ────────────────────────────────────────────

func TestSearcherWithMode_Hybrid_NeverUsesORFallback(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "mode-hybrid-or-proj"

	// "widget" is present, "zzznonexistentterm" is not: the implicit AND
	// query matches nothing. Store.SearchWithFallback would retry with OR
	// and find the doc via "widget" alone (fuzzy=true) — ModeHybrid must
	// NEVER do that retry.
	lexID, semID := seedForFusionPolicyTest(t, st, proj, "widget")
	queryVec := []float32{0.0, 1.0}
	if err := st.UpsertEmbedding(ctx, semID, "m1", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	embedder := &recordingEmbedder{
		vectors: map[string][]float32{"widget zzznonexistentterm": queryVec},
		model:   "m1",
	}
	searcher := hybrid.NewSearcher(st, embedder).WithMode(hybrid.ModeHybrid)

	results, meta, err := searcher.Search(ctx, store.SearchParams{
		Q: "widget zzznonexistentterm", Project: proj, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !meta.LexicalSkipped {
		t.Error("meta.LexicalSkipped should be true when the AND-only lexical list is empty")
	}
	if meta.Hybrid {
		t.Error("meta.Hybrid should be false when the lexical list was skipped")
	}
	for _, r := range results {
		if r.Observation.ID == lexID {
			t.Errorf("lexical doc %d must NOT appear — it only matches via the OR fallback, which ModeHybrid never runs", lexID)
		}
	}
	if len(results) == 0 || results[0].Observation.ID != semID {
		t.Errorf("want semantic doc %d ranked first (vector-only ranking), got %+v", semID, results)
	}
}

func TestSearcherWithMode_Hybrid_FusesWhenANDMatches(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "mode-hybrid-fuse-proj"

	lexID, semID := seedForFusionPolicyTest(t, st, proj, "widget")
	queryVec := []float32{0.0, 1.0}
	if err := st.UpsertEmbedding(ctx, semID, "m1", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}
	embedder := &recordingEmbedder{vectors: map[string][]float32{"widget": queryVec}, model: "m1"}
	searcher := hybrid.NewSearcher(st, embedder).WithMode(hybrid.ModeHybrid)

	results, meta, err := searcher.Search(ctx, store.SearchParams{Q: "widget", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !meta.Hybrid {
		t.Error("meta.Hybrid should be true when the AND query matches and fusion runs")
	}
	if meta.LexicalSkipped {
		t.Error("meta.LexicalSkipped should be false when fusion ran")
	}
	found := false
	for _, r := range results {
		if r.Observation.ID == lexID {
			found = true
		}
	}
	if !found {
		t.Errorf("lexical doc %d should appear in fused results: %+v", lexID, results)
	}
}

func TestSearcherWithMode_Hybrid_FallsBackOnEmbedError(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "mode-hybrid-fallback-proj"
	lexicalID, _ := seedForHybridTest(t, st, proj)

	embedder := &recordingEmbedder{model: "test-model", failErr: errors.New("dial tcp: refused")}
	searcher := hybrid.NewSearcher(st, embedder).WithMode(hybrid.ModeHybrid)

	results, meta, err := searcher.Search(ctx, store.SearchParams{Q: "gopher", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !meta.Degraded || meta.Effective != hybrid.ModeLexical {
		t.Errorf("meta = %+v, want Degraded=true, Effective=lexical", meta)
	}
	found := false
	for _, r := range results {
		if r.Observation.ID == lexicalID {
			found = true
		}
	}
	if !found {
		t.Errorf("degraded fallback should still find lexical doc %d: %+v", lexicalID, results)
	}
}

// ─── NewSearcherFromSettings + Mode ───────────────────────────────────────────

func TestNewSearcherFromSettings_DefaultModeIsVector(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	// search.mode intentionally left unset.

	s := hybrid.NewSearcherFromSettings(ctx, st)
	_, meta, err := s.Search(ctx, store.SearchParams{Q: "anything", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if meta.Mode != hybrid.ModeVector {
		t.Errorf("meta.Mode = %q, want %q (default search.mode)", meta.Mode, hybrid.ModeVector)
	}
}

func TestNewSearcherFromSettings_EmbeddingsDisabled_NonLexicalMode_Degraded(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	if err := st.SetSetting(ctx, store.SettingSearchMode, "hybrid"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	// embeddings.enabled left unset (defaults to "false").

	s := hybrid.NewSearcherFromSettings(ctx, st)
	_, meta, err := s.Search(ctx, store.SearchParams{Q: "anything", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !meta.Degraded {
		t.Error("meta.Degraded should be true when embeddings are disabled but a non-lexical mode was requested")
	}
	if meta.Mode != hybrid.ModeHybrid || meta.Effective != hybrid.ModeLexical {
		t.Errorf("meta.Mode/Effective = %q/%q, want hybrid/lexical", meta.Mode, meta.Effective)
	}
	if meta.Reason != "embeddings disabled" {
		t.Errorf("meta.Reason = %q, want %q", meta.Reason, "embeddings disabled")
	}
}

func TestNewSearcherFromSettings_EmbeddingsDisabled_LexicalMode_NotDegraded(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	if err := st.SetSetting(ctx, store.SettingSearchMode, "lexical"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	// embeddings.enabled left unset (defaults to "false").

	s := hybrid.NewSearcherFromSettings(ctx, st)
	_, meta, err := s.Search(ctx, store.SearchParams{Q: "anything", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if meta.Degraded {
		t.Error("meta.Degraded should be false: lexical mode never needed embeddings")
	}
	if meta.Mode != hybrid.ModeLexical || meta.Effective != hybrid.ModeLexical {
		t.Errorf("meta.Mode/Effective = %q/%q, want lexical/lexical", meta.Mode, meta.Effective)
	}
}

func TestNewSearcherFromSettings_InvalidStoredModeFallsBackToDefault(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	// Simulate a hand-edited / corrupted setting bypassing config-set validation.
	if err := st.SetSetting(ctx, store.SettingSearchMode, "bogus-mode"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	s := hybrid.NewSearcherFromSettings(ctx, st)
	if s == nil {
		t.Fatal("NewSearcherFromSettings returned nil")
	}
	_, _, err := s.Search(ctx, store.SearchParams{Q: "anything", Limit: 5})
	if err != nil {
		t.Fatalf("Search should not error on a corrupted search.mode setting: %v", err)
	}
}
