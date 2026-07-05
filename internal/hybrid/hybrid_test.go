package hybrid_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/embed"
	"github.com/ionix/ion-mem/internal/hybrid"
	"github.com/ionix/ion-mem/internal/store"
)

// ─── RRF ─────────────────────────────────────────────────────────────────────

func TestRRF_DocInBothListsBeatsEither(t *testing.T) {
	// Design doc example: a doc that appears in both BM25 and vector results
	// at moderate ranks should beat a doc that appears only in one list at rank 1.
	//
	// list1: [A, B, C]   rank-1=A, rank-2=B, rank-3=C
	// list2: [B, D]      rank-1=B, rank-2=D
	//
	// B appears in both lists: score = 1/(60+1) + 1/(60+1) = 2/61 ≈ 0.0328
	// A appears only in list1:  score = 1/(60+1) ≈ 0.0164
	// D appears only in list2:  score = 1/(60+2) ≈ 0.0161
	// So B > A > D.
	list1 := []string{"A", "B", "C"}
	list2 := []string{"B", "D"}

	scores := hybrid.RRF(list1, list2)

	if scores["B"] <= scores["A"] {
		t.Errorf("RRF: B (both lists) should score higher than A (one list): B=%.6f, A=%.6f",
			scores["B"], scores["A"])
	}
	if scores["A"] <= scores["D"] {
		t.Errorf("RRF: A (rank 1 in list1) should score higher than D (rank 2 in list2): A=%.6f, D=%.6f",
			scores["A"], scores["D"])
	}
}

func TestRRF_SingleList(t *testing.T) {
	list := []string{"X", "Y", "Z"}
	scores := hybrid.RRF(list)

	// Score for rank-1 = 1/(60+1), rank-2 = 1/(60+2), rank-3 = 1/(60+3).
	if scores["X"] <= scores["Y"] {
		t.Errorf("RRF single list: rank-1 X should score > rank-2 Y: X=%.6f, Y=%.6f",
			scores["X"], scores["Y"])
	}
	if scores["Y"] <= scores["Z"] {
		t.Errorf("RRF single list: rank-2 Y should score > rank-3 Z: Y=%.6f, Z=%.6f",
			scores["Y"], scores["Z"])
	}
}

func TestRRF_EmptyLists(t *testing.T) {
	scores := hybrid.RRF([]string{}, []string{})
	if len(scores) != 0 {
		t.Errorf("RRF empty lists: expected empty map, got %d entries", len(scores))
	}
}

func TestRRF_KIsFixed60(t *testing.T) {
	// Verify the k=60 constant: score for rank-1 item in a single list = 1/(60+1).
	list := []string{"only"}
	scores := hybrid.RRF(list)
	want := 1.0 / (60.0 + 1.0)
	got := scores["only"]
	if got < want-1e-9 || got > want+1e-9 {
		t.Errorf("RRF k=60: score = %.9f, want %.9f (1/61)", got, want)
	}
}

// ─── NewSearcherFromSettings ──────────────────────────────────────────────────

func mustOpenStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("mustOpenStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestNewSearcherFromSettings_EmbeddingsDisabled(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	// embeddings.enabled not set (default false)
	s := hybrid.NewSearcherFromSettings(ctx, st)
	if s == nil {
		t.Fatal("NewSearcherFromSettings returned nil")
	}
	// With embedder nil, search should behave exactly like BM25.
	// Meta.Hybrid must be false.
	_, meta, err := s.Search(ctx, store.SearchParams{Q: "anything", Limit: 5})
	if err != nil {
		t.Fatalf("Search with disabled embeddings: %v", err)
	}
	if meta.Hybrid {
		t.Error("Search with disabled embeddings: Meta.Hybrid should be false")
	}
}

func TestNewSearcherFromSettings_EmbeddingsEnabled_OllamaDown(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	// Set embeddings.enabled = true but point at a dead server.
	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, "http://localhost:19999"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	s := hybrid.NewSearcherFromSettings(ctx, st)
	// Should still construct successfully even if Ollama is unavailable.
	if s == nil {
		t.Fatal("NewSearcherFromSettings returned nil when Ollama is down")
	}
	// Search should fall back to BM25 gracefully (embed error → fallback).
	_, meta, err := s.Search(ctx, store.SearchParams{Q: "test", Limit: 5})
	if err != nil {
		t.Fatalf("Search with Ollama down: should not error (BM25 fallback), got: %v", err)
	}
	if meta.Hybrid {
		t.Error("Search with Ollama down: Meta.Hybrid should be false (fallback to BM25)")
	}
}

// ─── Searcher.Search ─────────────────────────────────────────────────────────

// fakeEmbedder is a test Embedder that returns deterministic vectors without
// network calls. It maps text to pre-configured vectors or returns zeros.
type fakeEmbedder struct {
	vectors map[string][]float32
	model   string
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := f.vectors[text]; ok {
		return v, nil
	}
	// Return a unit vector in direction [1,0,0,...] for unknown texts.
	return []float32{1.0, 0.0}, nil
}

func (f *fakeEmbedder) Model() string { return f.model }

func seedForHybridTest(t *testing.T, st *store.Store, project string) (lexicalID, semanticID int64) {
	t.Helper()
	ctx := context.Background()
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "hybrid-sess", Project: project, Directory: "/test",
	})

	// This doc has the keyword "gopher" — BM25 will find it.
	lexical, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "hybrid-sess", Type: "manual",
		Title: "gopher programming language", Content: "go gopher language specification",
		Project: project, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation lexical: %v", err)
	}

	// This doc has NO lexical overlap with the query "gopher" but is semantically close
	// to a query vector we'll plant.
	semantic, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "hybrid-sess", Type: "manual",
		Title: "completely unrelated text xyz", Content: "no keywords match at all",
		Project: project, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation semantic: %v", err)
	}

	return lexical.ID, semantic.ID
}

func TestSearcher_HybridMode_SemanticDocRanked(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "hybrid-proj"

	lexicalID, semanticID := seedForHybridTest(t, st, proj)

	// Plant embedding for semanticID that is identical to the query vector.
	// The query vector [0,1] will be the embed of "gopher".
	queryVec := []float32{0.0, 1.0}    // what the embedder returns for "gopher"
	semanticVec := []float32{0.0, 1.0} // very close to query — high cosine similarity
	lexicalVec := []float32{-1.0, 0.0} // orthogonal to query — low cosine similarity

	if err := st.UpsertEmbedding(ctx, semanticID, "test-model", semanticVec); err != nil {
		t.Fatalf("UpsertEmbedding semanticID: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, lexicalID, "test-model", lexicalVec); err != nil {
		t.Fatalf("UpsertEmbedding lexicalID: %v", err)
	}

	embedder := &fakeEmbedder{
		vectors: map[string][]float32{"gopher": queryVec},
		model:   "test-model",
	}

	searcher := hybrid.NewSearcher(st, embedder)
	results, meta, err := searcher.Search(ctx, store.SearchParams{
		Q: "gopher", Project: proj, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search hybrid: %v", err)
	}
	if !meta.Hybrid {
		t.Error("Search: Meta.Hybrid should be true when embedder is present")
	}
	// semanticID should appear in results (via RRF fusion with vector search).
	found := false
	for _, r := range results {
		if r.Observation.ID == semanticID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Search hybrid: semanticID %d not found in results (hybrid fusion failed)", semanticID)
	}
}

func TestSearcher_NoEmbedder_IdenticalToBM25(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "bm25-only"

	seedForHybridTest(t, st, proj)

	searcher := hybrid.NewSearcher(st, nil) // nil embedder = BM25 only
	results, meta, err := searcher.Search(ctx, store.SearchParams{
		Q: "gopher", Project: proj, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search BM25-only: %v", err)
	}
	if meta.Hybrid {
		t.Error("Search BM25-only: Meta.Hybrid should be false when embedder is nil")
	}
	if len(results) == 0 {
		t.Error("Search BM25-only: expected at least one result for 'gopher'")
	}
}

// ─── Searcher wired through settings (httptest fake Ollama) ──────────────────

func TestNewSearcherFromSettings_EmbeddingsEnabled_FakeOllama(t *testing.T) {
	vec := []float32{1.0, 0.0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]any{"embedding": vec}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	st := mustOpenStore(t)

	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting enabled: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting url: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "nomic-embed-text"); err != nil {
		t.Fatalf("SetSetting model: %v", err)
	}

	s := hybrid.NewSearcherFromSettings(ctx, st)
	if s == nil {
		t.Fatal("NewSearcherFromSettings returned nil")
	}

	// With a working fake Ollama and a seeded store, Search should not error.
	// We don't assert results here (empty store) — just that it runs without error.
	_, _, err := s.Search(context.Background(), store.SearchParams{
		Q:     "test query",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("Search with fake Ollama: %v", err)
	}
}

// ─── VectorSearch model isolation in hybrid path ─────────────────────────────

// TestSearcher_HybridMode_StaleModelVectorExcluded verifies that when the
// embedder's Model() is "m1", a semantically-close document whose embedding
// was stored under "m2" does NOT appear via the vector branch of hybrid search.
func TestSearcher_HybridMode_StaleModelVectorExcluded(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "stale-model-proj"

	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID: "stale-sess", Project: proj, Directory: "/test",
	})

	// Seed a document that is semantically close to the query vector.
	closeDoc, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "stale-sess", Type: "manual",
		Title: "semantically close doc", Content: "matching content xyz",
		Project: proj, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation closeDoc: %v", err)
	}

	// Store its embedding under model "m2" (stale — embedder uses "m1").
	queryVec := []float32{1.0, 0.0}
	if err := st.UpsertEmbedding(ctx, closeDoc.ID, "m2", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding m2: %v", err)
	}

	// Fake embedder returns the same query vector but identifies as "m1".
	embedder := &fakeEmbedder{
		vectors: map[string][]float32{"query": queryVec},
		model:   "m1",
	}

	searcher := hybrid.NewSearcher(st, embedder)
	results, meta, err := searcher.Search(ctx, store.SearchParams{
		Q: "query", Project: proj, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	_ = meta

	// closeDoc must NOT appear via the vector branch (wrong model).
	// BM25 may or may not surface it — we only check vector-sourced promotion
	// by ensuring the doc is absent when BM25 can't find "query" in its content.
	for _, r := range results {
		if r.Observation.ID == closeDoc.ID {
			// If it appears only from BM25, Score/BM25 field will be non-zero from
			// BM25 side. The important thing: the m2 vector must not have produced
			// a VectorSearch hit. We can verify indirectly by confirming meta.Hybrid
			// is true but the doc ranking is not boosted by vector — but the simplest
			// observable invariant is that UpsertEmbedding under "m1" has zero rows,
			// so VectorSearch("m1") returns nothing — any appearance is from BM25 only.
			// Since "query" does not appear in closeDoc's title/content, BM25 won't
			// find it either. So it must be absent.
			t.Errorf("closeDoc (embedded under m2) appeared in results when embedder uses m1 — stale vector was not excluded")
		}
	}
}

// ─── embed.Client.Embed model forwarding ─────────────────────────────────────

func TestOllamaEmbedder_ModelForwardedInRequest(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		resp := map[string]any{"embedding": []float32{1.0, 2.0}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &embed.Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 3 * time.Second}}
	e := embed.NewOllamaEmbedder(c, "nomic-embed-text")
	_, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if capturedBody["model"] != "nomic-embed-text" {
		t.Errorf("model forwarded = %q, want %q", capturedBody["model"], "nomic-embed-text")
	}
}

// TestWeightedRRF_VectorBoostLetsSingleListWin verifies that the vector weight
// lets a top pure-semantic hit outrank a doc that appears DEEP in both lists.
// (Dual presence near the top legitimately wins regardless of weight — with
// k=60 a rank-2 dual doc scores 3/62, unreachable for any single-list hit.)
func TestWeightedRRF_VectorBoostLetsSingleListWin(t *testing.T) {
	// "both" sits at rank 40 in each list; "semantic" is rank 1 vector-only.
	// Plain RRF: both = 2/100 = 0.020 > semantic = 1/61 ≈ 0.0164 → both wins.
	// Weight 2.0: both = 3/100 = 0.030 < semantic = 2/61 ≈ 0.0328 → flips.
	deepList := func(prefix string, last string) []string {
		list := make([]string, 40)
		for i := 0; i < 39; i++ {
			list[i] = fmt.Sprintf("%s-filler-%d", prefix, i)
		}
		list[39] = last
		return list
	}
	bm25 := deepList("bm25", "both")
	vec := append([]string{"semantic"}, deepList("vec", "both")[1:]...)

	plain := hybrid.RRF(bm25, vec)
	if plain["semantic"] >= plain["both"] {
		t.Fatalf("precondition failed: plain RRF should favor the dual-list doc (semantic=%f both=%f)",
			plain["semantic"], plain["both"])
	}

	weighted := hybrid.WeightedRRF([]float64{1.0, 2.0}, bm25, vec)
	if weighted["semantic"] <= weighted["both"] {
		t.Errorf("weighted RRF should let the top pure-semantic hit win: semantic=%f both=%f",
			weighted["semantic"], weighted["both"])
	}
}

// TestWeightedRRF_MissingWeightsDefaultToOne verifies the defensive default.
func TestWeightedRRF_MissingWeightsDefaultToOne(t *testing.T) {
	got := hybrid.WeightedRRF(nil, []string{"a"}, []string{"a"})
	want := hybrid.RRF([]string{"a"}, []string{"a"})
	if got["a"] != want["a"] {
		t.Errorf("nil weights should equal unweighted RRF: got %f want %f", got["a"], want["a"])
	}
}
