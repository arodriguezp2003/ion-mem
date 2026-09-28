package store

import "sort"

// sortSearchResultsByScore sorts results ascending by Score (more negative =
// more relevant), the shared ordering convention for both BM25 (searchMatch)
// and vector (VectorSearch) results.
//
// It uses sort.SliceStable rather than sort.Slice so that results with an
// exactly equal Score keep the order they were appended in (SQL scan order).
// sort.Slice's tie-breaking is unspecified and can reorder equal-score
// entries differently between otherwise-identical runs, which made repeated
// `ion-mem eval` invocations non-reproducible for tied scores.
func sortSearchResultsByScore(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score < results[j].Score
	})
}
