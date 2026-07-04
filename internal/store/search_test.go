package store_test

import (
	"context"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestSearch_BM25Ranking verifies that an observation containing the query term
// more frequently ranks higher (lower BM25 score) than one with fewer occurrences.
func TestSearch_BM25Ranking(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	// Insert a high-relevance observation: "sqlite" appears many times.
	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "sqlite sqlite sqlite high relevance",
		Content: "sqlite sqlite sqlite migration schema", Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation high: %v", err)
	}

	// Insert a lower-relevance observation: "sqlite" appears once.
	_, err = s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "low relevance document",
		Content: "sqlite just mentioned once here", Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation low: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "sqlite"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// BM25 score is negative in SQLite — less negative = more relevant.
	// ORDER BY score ASC puts most-relevant (most-negative) first.
	if results[0].Score > results[1].Score {
		t.Errorf("expected first result to have lower (more relevant) BM25 score: got %f > %f",
			results[0].Score, results[1].Score)
	}
}

// TestSearch_TypeFilter verifies that the Type filter restricts results.
func TestSearch_TypeFilter(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	for _, typ := range []string{"decision", "bugfix"} {
		_, err := s.AddObservation(ctx, store.AddObservationParams{
			SessionID: sess.ID, Type: typ, Title: "test " + typ,
			Content: "test content", Project: "p", Scope: "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %s: %v", typ, err)
		}
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "test", Type: "decision"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Observation.Type != "decision" {
			t.Errorf("expected only decision type, got %q", r.Observation.Type)
		}
	}
}

// TestSearch_ProjectFilter verifies that the Project filter restricts results.
func TestSearch_ProjectFilter(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sessA := mustSession(t, s, "alpha")
	sessB := mustSession(t, s, "beta")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sessA.ID, Type: "decision", Title: "alpha obs",
		Content: "test observation", Project: "alpha", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation alpha: %v", err)
	}
	_, err = s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sessB.ID, Type: "decision", Title: "beta obs",
		Content: "test observation", Project: "beta", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation beta: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "test", Project: "alpha"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Observation.Project != "alpha" {
			t.Errorf("expected only project=alpha, got %q", r.Observation.Project)
		}
	}
}

// TestSearch_ScopeFilter verifies that the Scope filter restricts results.
func TestSearch_ScopeFilter(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	for _, scope := range []string{"project", "personal"} {
		_, err := s.AddObservation(ctx, store.AddObservationParams{
			SessionID: sess.ID, Type: "decision", Title: "scope " + scope,
			Content: "test scope content", Project: "p", Scope: scope,
		})
		if err != nil {
			t.Fatalf("AddObservation scope=%s: %v", scope, err)
		}
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "test", Scope: "personal"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Observation.Scope != "personal" {
			t.Errorf("expected only scope=personal, got %q", r.Observation.Scope)
		}
	}
}

// TestSearch_EmptyResult verifies that Search returns an empty slice (not error)
// when no observations match.
func TestSearch_EmptyResult(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")
	mustObservation(t, s, sess.ID)

	results, err := s.Search(ctx, store.SearchParams{Q: "zzznomatch"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestSearch_ExcludesSoftDeleted verifies that soft-deleted observations are
// not returned by Search.
func TestSearch_ExcludesSoftDeleted(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	obs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "to be deleted",
		Content: "unique soft delete marker xyzzy", Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	if err := s.DeleteObservation(ctx, obs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "xyzzy"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Observation.ID == obs.ID {
			t.Errorf("soft-deleted observation %d should not appear in Search results", obs.ID)
		}
	}
}

// TestSearch_FTS5KebabFragment verifies that a fragment of a kebab-case
// topic_key is searchable via FTS5 tokenization (S2-T13).
// The FTS5 unicode61 tokenizer splits on hyphens, so "auth-model" in the
// topic_key is indexed as two tokens: "auth" and "model".
func TestSearch_FTS5KebabFragment(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "architecture", Title: "auth model design",
		Content:  "auth model architecture description",
		Project:  "p",
		Scope:    "project",
		TopicKey: "architecture/auth-model",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "auth"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("TestSearch_FTS5KebabFragment: expected at least 1 result for query 'auth', got 0")
	}
	found := false
	for _, r := range results {
		if r.Observation.TopicKey != nil && *r.Observation.TopicKey == "architecture/auth-model" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected observation with topic_key='architecture/auth-model' in results for query 'auth'")
	}
}

// TestSearch_FTS5KebabFull verifies that a full kebab-case segment in
// topic_key is searchable via FTS5 (S2-T14).
// The FTS5 tokenizer splits "local-store-mvp" into "local", "store", "mvp".
// Querying "local-store-mvp" is split into those tokens by FTS5 matching.
func TestSearch_FTS5KebabFull(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "architecture", Title: "local store mvp design",
		Content:  "local store mvp design document",
		Project:  "p",
		Scope:    "project",
		TopicKey: "sdd/local-store-mvp/design",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "local-store-mvp"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("TestSearch_FTS5KebabFull: expected at least 1 result for query 'local-store-mvp', got 0")
	}
	found := false
	for _, r := range results {
		if r.Observation.TopicKey != nil && *r.Observation.TopicKey == "sdd/local-store-mvp/design" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected observation with topic_key='sdd/local-store-mvp/design' in results for query 'local-store-mvp', got results: %+v", results)
	}
}

// TestAddObservation_ConcurrentSuccess verifies that two goroutines calling
// AddObservation concurrently on the same store both succeed (WAL + busy_timeout).
func TestAddObservation_ConcurrentSuccess(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	type result struct {
		obs store.Observation
		err error
	}
	ch := make(chan result, 2)

	for i := 0; i < 2; i++ {
		i := i
		go func() {
			obs, err := s.AddObservation(ctx, store.AddObservationParams{
				SessionID: sess.ID,
				Type:      "decision",
				Title:     "concurrent obs",
				Content:   "goroutine content unique" + string(rune('A'+i)),
				Project:   "p",
				Scope:     "project",
			})
			ch <- result{obs, err}
		}()
	}

	for i := 0; i < 2; i++ {
		r := <-ch
		if r.err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, r.err)
		}
	}
}

// TestSearchWithFallback_ORFallbackFindsPartialMatch verifies that when the
// implicit-AND query matches nothing, the OR fallback surfaces observations
// matching a subset of terms and flags them as fuzzy.
func TestSearchWithFallback_ORFallbackFindsPartialMatch(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "envelope error handling",
		Content: "structured envelope error design", Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, fuzzy, err := s.SearchWithFallback(ctx, store.SearchParams{Q: "envelope kubernetes"})
	if err != nil {
		t.Fatalf("SearchWithFallback: %v", err)
	}
	if !fuzzy {
		t.Error("expected fuzzy=true when results come from OR fallback")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 fallback result, got %d", len(results))
	}
}

// TestSearchWithFallback_ExactMatchNotFuzzy verifies that an AND match returns
// fuzzy=false and does not trigger the fallback.
func TestSearchWithFallback_ExactMatchNotFuzzy(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "envelope error handling",
		Content: "structured envelope error design", Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, fuzzy, err := s.SearchWithFallback(ctx, store.SearchParams{Q: "envelope error"})
	if err != nil {
		t.Fatalf("SearchWithFallback: %v", err)
	}
	if fuzzy {
		t.Error("expected fuzzy=false for a direct AND match")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestSearchWithFallback_SingleTermNoFallback verifies that a single-term miss
// returns empty without fuzzy (OR fallback is pointless for one term).
func TestSearchWithFallback_SingleTermNoFallback(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "envelope error handling",
		Content: "structured envelope error design", Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, fuzzy, err := s.SearchWithFallback(ctx, store.SearchParams{Q: "kubernetes"})
	if err != nil {
		t.Fatalf("SearchWithFallback: %v", err)
	}
	if fuzzy {
		t.Error("expected fuzzy=false when no fallback ran")
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// TestSearchWithFallback_AllTermsMissing verifies that when neither AND nor OR
// matches anything, the result is empty and not fuzzy.
func TestSearchWithFallback_AllTermsMissing(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision", Title: "envelope error handling",
		Content: "structured envelope error design", Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, fuzzy, err := s.SearchWithFallback(ctx, store.SearchParams{Q: "kubernetes docker"})
	if err != nil {
		t.Fatalf("SearchWithFallback: %v", err)
	}
	if fuzzy {
		t.Error("expected fuzzy=false when fallback found nothing")
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// TestSearchWithFallback_FiltersApplyToFallback verifies that type/project
// filters constrain the OR fallback query as well.
func TestSearchWithFallback_FiltersApplyToFallback(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	for _, typ := range []string{"decision", "bugfix"} {
		_, err := s.AddObservation(ctx, store.AddObservationParams{
			SessionID: sess.ID, Type: typ, Title: "envelope note " + typ,
			Content: "envelope content", Project: "p", Scope: "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %s: %v", typ, err)
		}
	}

	results, fuzzy, err := s.SearchWithFallback(ctx, store.SearchParams{Q: "envelope kubernetes", Type: "bugfix"})
	if err != nil {
		t.Fatalf("SearchWithFallback: %v", err)
	}
	if !fuzzy {
		t.Error("expected fuzzy=true from OR fallback")
	}
	if len(results) != 1 || results[0].Observation.Type != "bugfix" {
		t.Fatalf("expected exactly the bugfix observation, got %d results", len(results))
	}
}
