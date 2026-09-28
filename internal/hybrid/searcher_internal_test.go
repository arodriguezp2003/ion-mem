package hybrid

import (
	"context"
	"errors"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// stubEmbedder is a minimal embed.Embedder for white-box tests in this
// package that need to construct a *Searcher directly (bypassing the
// public constructors) to inject a storeSearcher spy.
type stubEmbedder struct {
	vec   []float32
	model string
}

func (e *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return e.vec, nil
}

func (e *stubEmbedder) Model() string { return e.model }

// erroringSearchStore wraps a real *store.Store but forces Search to
// always fail, while SearchWithFallback and VectorSearch delegate to the
// real store unchanged. This is the "store spy" used to exercise
// searchHybridStrict's lexErr branch (the AND-only lexical call failing)
// independently of vector search succeeding — something that isn't
// reproducible via real database-level failure injection, since both calls
// share the same *sql.DB.
type erroringSearchStore struct {
	*store.Store
	searchErr error
}

func (s *erroringSearchStore) Search(_ context.Context, _ store.SearchParams) ([]store.SearchResult, error) {
	return nil, s.searchErr
}

// TestSearchHybridStrict_StoreSearchError_DegradesToVectorRanking is the
// regression test for the review fix: on a store.Search error,
// searchHybridStrict must degrade to the vector-only ranking
// (Degraded=true, Reason set, Effective=ModeVector) instead of returning
// nil results with the raw error.
func TestSearchHybridStrict_StoreSearchError_DegradesToVectorRanking(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	real, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = real.Close() })

	proj := "spy-proj"
	if _, err := real.CreateSession(ctx, store.CreateSessionParams{
		ID: "spy-sess", Project: proj, Directory: "/x",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obs, err := real.AddObservation(ctx, store.AddObservationParams{
		SessionID: "spy-sess", Type: "manual",
		Title: "semantic doc", Content: "unrelated content",
		Project: proj, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	queryVec := []float32{0.0, 1.0}
	if err := real.UpsertEmbedding(ctx, obs.ID, "m1", queryVec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	spy := &erroringSearchStore{Store: real, searchErr: errors.New("search: disk I/O error")}

	s := &Searcher{
		store:        spy,
		embeddr:      &stubEmbedder{vec: queryVec, model: "m1"},
		vectorWeight: DefaultVectorWeight,
		embedTimeout: defaultEmbedTimeout,
		mode:         ModeHybrid,
	}

	results, meta, err := s.Search(ctx, store.SearchParams{Q: "query", Project: proj, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !meta.Degraded {
		t.Error("meta.Degraded should be true when store.Search errors")
	}
	if meta.Mode != ModeHybrid {
		t.Errorf("meta.Mode = %q, want %q", meta.Mode, ModeHybrid)
	}
	if meta.Effective != ModeVector {
		t.Errorf("meta.Effective = %q, want %q (degraded to the vector-only ranking)", meta.Effective, ModeVector)
	}
	if meta.Reason == "" {
		t.Error("meta.Reason should explain the store.Search failure")
	}
	if meta.Hybrid {
		t.Error("meta.Hybrid should be false: no fusion happened")
	}

	found := false
	for _, r := range results {
		if r.Observation.ID == obs.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the vector-only ranking to include doc %d: %+v", obs.ID, results)
	}
}
