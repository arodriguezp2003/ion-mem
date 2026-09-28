package store_test

import (
	"context"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// setupActiveAndSupersededPair creates two observations with identical
// content/title (so lexical/vector scoring is otherwise tied), marks the
// second one superseded by the first, and returns (active, superseded).
func setupActiveAndSupersededPair(t *testing.T, s *store.Store) (active, superseded store.Observation) {
	t.Helper()
	ctx := context.Background()
	sess := mustSession(t, s, "status-rank-proj")

	active, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "widget-cache-policy",
		Content: "use LRU cache eviction for the widget store", Project: "status-rank-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation active: %v", err)
	}
	superseded, err = s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "widget-cache-policy-old",
		Content: "use LRU cache eviction for the widget store", Project: "status-rank-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation superseded: %v", err)
	}
	if err := s.SetObservationStatus(ctx, superseded.ID, store.StatusSuperseded, &active.ID, "replaced"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}
	superseded, err = s.GetObservation(ctx, superseded.ID)
	if err != nil {
		t.Fatalf("GetObservation superseded: %v", err)
	}
	return active, superseded
}

func TestSearch_SupersededRanksBelowActiveWithEqualBM25(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	active, superseded := setupActiveAndSupersededPair(t, s)

	results, err := s.Search(ctx, store.SearchParams{Q: "widget cache eviction", Project: "status-rank-proj"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	idxOf := func(id int64) int {
		for i, r := range results {
			if r.Observation.ID == id {
				return i
			}
		}
		return -1
	}
	activeIdx := idxOf(active.ID)
	supersededIdx := idxOf(superseded.ID)
	if activeIdx == -1 || supersededIdx == -1 {
		t.Fatalf("expected both observations in results, got %+v", results)
	}
	if activeIdx > supersededIdx {
		t.Errorf("expected active observation (idx %d) to rank above superseded (idx %d)", activeIdx, supersededIdx)
	}
}

func TestSearch_StatusFilterExact(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	_, superseded := setupActiveAndSupersededPair(t, s)

	results, err := s.Search(ctx, store.SearchParams{
		Q: "widget cache eviction", Project: "status-rank-proj", Status: store.StatusSuperseded,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Observation.ID != superseded.ID {
		t.Fatalf("Search with Status filter = %+v, want exactly [%d]", results, superseded.ID)
	}
	if results[0].Status != store.StatusSuperseded {
		t.Errorf("SearchResult.Status = %q, want %q", results[0].Status, store.StatusSuperseded)
	}
}

func TestSearch_IncludeSupersededFalseHidesSuperseded(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	active, superseded := setupActiveAndSupersededPair(t, s)

	no := false
	results, err := s.Search(ctx, store.SearchParams{
		Q: "widget cache eviction", Project: "status-rank-proj", IncludeSuperseded: &no,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Observation.ID == superseded.ID {
			t.Fatalf("superseded observation %d present in results when IncludeSuperseded=false", superseded.ID)
		}
	}
	found := false
	for _, r := range results {
		if r.Observation.ID == active.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("active observation %d missing from results", active.ID)
	}
}

// TestSearch_IncludeSupersededFalseAlsoHidesObsolete verifies that
// IncludeSuperseded=false restricts results to status=active only — it
// hides BOTH superseded and obsolete rows, not just superseded ones.
func TestSearch_IncludeSupersededFalseAlsoHidesObsolete(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "obsolete-rank-proj")

	active, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "gadget policy",
		Content: "gadget rate limiting policy", Project: "obsolete-rank-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation active: %v", err)
	}
	obsolete, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "gadget policy retired",
		Content: "gadget rate limiting policy", Project: "obsolete-rank-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation obsolete: %v", err)
	}
	if err := s.SetObservationStatus(ctx, obsolete.ID, store.StatusObsolete, nil, "no longer relevant"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	no := false
	results, err := s.Search(ctx, store.SearchParams{
		Q: "gadget rate limiting", Project: "obsolete-rank-proj", IncludeSuperseded: &no,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Observation.ID == obsolete.ID {
			t.Fatalf("obsolete observation %d present in results when IncludeSuperseded=false", obsolete.ID)
		}
	}
	found := false
	for _, r := range results {
		if r.Observation.ID == active.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("active observation %d missing from results", active.ID)
	}
}

func TestVectorSearch_SupersededRanksBelowActiveWithEqualCosine(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "vec-status-proj")

	active, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "a", Content: "a", Project: "vec-status-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation active: %v", err)
	}
	superseded, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "b", Content: "b", Project: "vec-status-proj", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation superseded: %v", err)
	}
	if err := s.SetObservationStatus(ctx, superseded.ID, store.StatusSuperseded, &active.ID, ""); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	// Identical embeddings so cosine similarity ties; only the status penalty
	// should separate them.
	vec := []float32{1, 0, 0}
	if err := s.UpsertEmbedding(ctx, active.ID, "test-model", vec); err != nil {
		t.Fatalf("UpsertEmbedding active: %v", err)
	}
	if err := s.UpsertEmbedding(ctx, superseded.ID, "test-model", vec); err != nil {
		t.Fatalf("UpsertEmbedding superseded: %v", err)
	}

	results, err := s.VectorSearch(ctx, vec, store.SearchParams{Project: "vec-status-proj", Model: "test-model"})
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if results[0].Observation.ID != active.ID {
		t.Errorf("expected active observation ranked first, got %+v", results)
	}
	if results[0].Score >= results[1].Score {
		t.Errorf("expected active Score (%f) < superseded Score (%f)", results[0].Score, results[1].Score)
	}
}
