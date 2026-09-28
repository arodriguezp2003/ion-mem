package store

// This file uses the internal (package store) test form, rather than the
// store_test convention used elsewhere in this package, specifically to
// reach the unexported sortSearchResultsByScore helper directly: an
// end-to-end test through SearchWithFallback/VectorSearch cannot construct
// an EXACT score tie (BM25 length-normalization and nanosecond-precision
// recency timestamps make two rows differ by a float64 epsilon even when
// "conceptually" identical), so the tie-breaking contract is verified here
// against the pure comparator instead.

import (
	"testing"
)

func TestSortSearchResultsByScore_EqualScoresKeepInsertionOrder(t *testing.T) {
	results := []SearchResult{
		{Observation: Observation{ID: 1}, Score: -5.0},
		{Observation: Observation{ID: 2}, Score: -5.0},
		{Observation: Observation{ID: 3}, Score: -5.0},
		{Observation: Observation{ID: 4}, Score: -5.0},
	}

	sortSearchResultsByScore(results)

	wantOrder := []int64{1, 2, 3, 4}
	for i, want := range wantOrder {
		if results[i].Observation.ID != want {
			t.Fatalf("index %d: ID = %d, want %d (insertion order not preserved for tied scores): %+v",
				i, results[i].Observation.ID, want, results)
		}
	}
}

func TestSortSearchResultsByScore_OrdersAscending(t *testing.T) {
	results := []SearchResult{
		{Observation: Observation{ID: 1}, Score: -1.0},
		{Observation: Observation{ID: 2}, Score: -9.0},
		{Observation: Observation{ID: 3}, Score: -5.0},
	}

	sortSearchResultsByScore(results)

	wantOrder := []int64{2, 3, 1} // most negative (best) first
	for i, want := range wantOrder {
		if results[i].Observation.ID != want {
			t.Fatalf("index %d: ID = %d, want %d: %+v", i, results[i].Observation.ID, want, results)
		}
	}
}

func TestSortSearchResultsByScore_MixedTiesAndDistinctScores(t *testing.T) {
	// Two ties at -5.0 (IDs 10, 11 in that insertion order) sandwiched between
	// distinct scores; verifies stability holds even when not all scores tie.
	results := []SearchResult{
		{Observation: Observation{ID: 20}, Score: -1.0},
		{Observation: Observation{ID: 10}, Score: -5.0},
		{Observation: Observation{ID: 11}, Score: -5.0},
		{Observation: Observation{ID: 30}, Score: -9.0},
	}

	sortSearchResultsByScore(results)

	wantOrder := []int64{30, 10, 11, 20}
	for i, want := range wantOrder {
		if results[i].Observation.ID != want {
			t.Fatalf("index %d: ID = %d, want %d: %+v", i, results[i].Observation.ID, want, results)
		}
	}
}
