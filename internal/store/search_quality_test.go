package store_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/store"
)

// -----------------------------------------------------------------------------
// Task 1: BM25 column weights
// -----------------------------------------------------------------------------

// TestSearch_WeightedBM25_TitleBeatsContent verifies that the same query term
// appearing only in the title ranks higher than the same term in content only.
// Title weight is 8x vs content weight of 1x.
func TestSearch_WeightedBM25_TitleBeatsContent(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	// Title-only match: "architecture" in title, generic content.
	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "architecture design decision",
		Content: "this document covers a general design aspect",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation title-only: %v", err)
	}

	// Content-only match: generic title, "architecture" in content.
	_, err = s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "general note about systems",
		Content: "architecture is described here in the body of this document",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation content-only: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "architecture"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// BM25 is negative; more negative = more relevant.
	// ORDER BY score ASC → most relevant first.
	// Title doc should rank first due to 8x weight.
	if results[0].Observation.Title != "architecture design decision" {
		t.Errorf("expected title-match doc first; got title=%q (score=%f), second title=%q (score=%f)",
			results[0].Observation.Title, results[0].Score,
			results[1].Observation.Title, results[1].Score)
	}
}

// TestSearch_WeightedBM25_TopicKeyBeatsContent verifies that topic_key (weight 6x)
// beats content (weight 1x) for the same matching term.
func TestSearch_WeightedBM25_TopicKeyBeatsContent(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	// topic_key match: "refactor" appears in topic_key only.
	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:    "design update",
		Content:  "general update to the system",
		TopicKey: "sdd/refactor/design",
		Project:  "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation topic-key: %v", err)
	}

	// content-only: "refactor" only in content.
	_, err = s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "planning note",
		Content: "we should refactor the module at some point",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation content-only: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "refactor"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// Topic-key doc should rank first (6x > 1x).
	if results[0].Observation.TopicKey == nil || !strings.Contains(*results[0].Observation.TopicKey, "refactor") {
		t.Errorf("expected topic-key doc first; got title=%q score=%f, then title=%q score=%f",
			results[0].Observation.Title, results[0].Score,
			results[1].Observation.Title, results[1].Score)
	}
}

// -----------------------------------------------------------------------------
// Task 2: Recency decay — Go-side rescoring
// -----------------------------------------------------------------------------

// TestSearch_RecencyDecay_FreshBeatsStale verifies that two docs with identical
// BM25 relevance are reordered so the fresher one ranks higher.
func TestSearch_RecencyDecay_FreshBeatsStale(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	uniqueWord := "recencytestword"

	// Insert stale observation.
	staleObs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "stale document about " + uniqueWord,
		Content: uniqueWord + " appears in this old document",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation stale: %v", err)
	}

	// Insert fresh observation (identical text for equal raw BM25).
	freshObs, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "fresh document about " + uniqueWord,
		Content: uniqueWord + " appears in this new document",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation fresh: %v", err)
	}

	// Backdate the stale doc's last_seen_at to 90 days ago.
	staleTime := time.Now().UTC().Add(-90 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := s.DB().ExecContext(ctx,
		"UPDATE observations SET last_seen_at=? WHERE id=?", staleTime, staleObs.ID,
	); err != nil {
		t.Fatalf("backdate stale: %v", err)
	}
	// Keep fresh doc's last_seen_at at now (already set on insert).
	_ = freshObs

	results, err := s.Search(ctx, store.SearchParams{Q: uniqueWord})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// BM25 field must be preserved.
	rawBM25Diff := math.Abs(results[0].BM25 - results[1].BM25)
	if rawBM25Diff > 0.5 {
		t.Logf("raw BM25 values are not equal (diff=%f); test still valid if fresh ranks first", rawBM25Diff)
	}

	// Fresh doc must rank first after decay.
	if results[0].Observation.ID == staleObs.ID {
		t.Errorf("stale doc ranked first; fresh doc should win after recency decay. scores: [0]=%f [1]=%f",
			results[0].Score, results[1].Score)
	}

	// Final scores must differ: decay must have effect.
	if results[0].Score == results[1].Score {
		t.Error("final scores are equal; recency decay had no effect")
	}
}

// TestSearch_RecencyDecay_StrongFreshBeatsWeakFresh verifies that decay does not
// resurrect a poor match: a strong fresh match ranks above a weaker fresh match.
func TestSearch_RecencyDecay_StrongFreshBeatsWeakFresh(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	// Strong match: "benchmark" in title (weight 8x) + content.
	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "benchmark performance test benchmark",
		Content: "benchmark results show benchmark improvements benchmark metric",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation strong: %v", err)
	}

	// Weak match: "benchmark" appears once in content only.
	_, err = s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "a note about work",
		Content: "we did some benchmark testing briefly",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation weak: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "benchmark"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// Strong match must rank first.
	if results[0].Observation.Title != "benchmark performance test benchmark" {
		t.Errorf("strong match should rank first; got %q first", results[0].Observation.Title)
	}
}

// TestSearch_BM25FieldPreserved verifies that SearchResult.BM25 holds the raw
// SQLite BM25 value distinct from (and ≤ 0) while Score reflects the final decay.
func TestSearch_BM25FieldPreserved(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "raw bm25 check observation",
		Content: "raw bm25 value test content",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "raw"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	r := results[0]
	// BM25 from SQLite is always ≤ 0 (negative or zero).
	if r.BM25 > 0 {
		t.Errorf("BM25 should be ≤ 0, got %f", r.BM25)
	}
	// Score is the final decayed value; for a fresh doc it should be close to BM25.
	// Specifically: Score = BM25 * exp(-decay) where decay ≈ 0 for a just-inserted doc.
	// So Score should be negative (close to BM25).
	if r.Score > 0 {
		t.Errorf("Score should be ≤ 0 for a fresh doc, got %f", r.Score)
	}
}

// -----------------------------------------------------------------------------
// Task 3: snippet() content preview
// -----------------------------------------------------------------------------

// TestSearch_Snippet_ContainsTermFromMiddle verifies that Snippet surfaces
// the match context even when the match term is far from the start of content.
func TestSearch_Snippet_ContainsTermFromMiddle(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	// Build ~600 bytes of leading content before the match term.
	prefix := strings.Repeat("word filler content sentence here. ", 20) // ~700 chars
	uniqueTerm := "uniquesnippetterm"
	longContent := prefix + uniqueTerm + " more context words after the term here"

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "long content observation",
		Content: longContent,
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: uniqueTerm})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	r := results[0]
	// Snippet must contain the match term (not just the first bytes of content).
	if !strings.Contains(r.Snippet, uniqueTerm) {
		t.Errorf("Snippet does not contain match term %q; got Snippet=%q", uniqueTerm, r.Snippet)
	}
	// Snippet must NOT equal the raw content (it's a contextual excerpt).
	if r.Snippet == longContent {
		t.Error("Snippet is the full content; expected a short excerpt")
	}
}

// TestSearch_Snippet_ElipsisForNonStart verifies that the snippet contains the
// ellipsis separator ("…") when the match is not at the very start of content.
func TestSearch_Snippet_ElipsisForNonStart(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	filler := strings.Repeat("intro filler sentence here again. ", 15)
	uniqueTerm := "elipsistestword"
	content := filler + uniqueTerm + " conclusion text"

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "snippet elipsis test",
		Content: content,
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	results, err := s.Search(ctx, store.SearchParams{Q: uniqueTerm})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	if !strings.Contains(results[0].Snippet, "…") {
		t.Errorf("expected ellipsis in Snippet for non-start match; got %q", results[0].Snippet)
	}
}

// TestSearch_FallbackPathGetsSnippet verifies that the OR fallback path also
// returns Snippet via searchMatch.
func TestSearch_FallbackPathGetsSnippet(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	_, err := s.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID, Type: "decision",
		Title:   "fallback snippet test",
		Content: "snippet test content with fallbackterm present here",
		Project: "p", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	// "fallbackterm" matches, "nomatchwhatsoever" does not → AND fails → OR fallback.
	results, fuzzy, err := s.SearchWithFallback(ctx, store.SearchParams{Q: "fallbackterm nomatchwhatsoever"})
	if err != nil {
		t.Fatalf("SearchWithFallback: %v", err)
	}
	if !fuzzy {
		t.Error("expected fuzzy=true from OR fallback")
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result from OR fallback")
	}
	if results[0].Snippet == "" {
		t.Error("Snippet must be non-empty for OR fallback results")
	}
}

// -----------------------------------------------------------------------------
// Candidate fetch (3*limit) boundary test
// -----------------------------------------------------------------------------

// TestSearch_CandidatePool_LargeResultSet verifies that when there are more
// candidates than limit, the top `limit` by final score are returned.
func TestSearch_CandidatePool_LargeResultSet(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "p")

	// Insert 25 docs all containing "poolterm".
	for i := 0; i < 25; i++ {
		_, err := s.AddObservation(ctx, store.AddObservationParams{
			SessionID: sess.ID, Type: "decision",
			Title:   fmt.Sprintf("poolterm document number %d", i),
			Content: fmt.Sprintf("poolterm content body index %d", i),
			Project: "p", Scope: "project",
		})
		if err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}

	results, err := s.Search(ctx, store.SearchParams{Q: "poolterm", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 10 {
		t.Errorf("expected exactly 10 results (limit), got %d", len(results))
	}
}
