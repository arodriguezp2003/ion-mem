// Package hybrid provides RRF fusion of BM25 and vector search results,
// plus a Searcher that gracefully degrades to BM25-only when no embedder
// is configured or when the embedding call fails.
package hybrid

import (
	"context"
	"sort"
	"time"

	"github.com/ionix/ion-mem/internal/embed"
	"github.com/ionix/ion-mem/internal/store"
)

// rrfK is the rank-constant used in Reciprocal Rank Fusion.
// A value of 60 is the standard recommendation from the original RRF paper.
const rrfK = 60.0

// vectorRRFWeight boosts the vector list in the hybrid fusion. Plain RRF is
// biased toward documents that appear in BOTH lists: a lexically-adjacent
// distractor (shares one query word AND is vaguely semantically close) can
// outrank the true semantic match that only the vector list surfaces. A
// weight > 1 on the vector list lets a strong pure-semantic hit compete.
// Calibrated against the golden set (see internal/eval): lexical MeanMRR
// must stay 1.0 while the semantic-gap queries improve.
const vectorRRFWeight = 2.0

// RRF computes Reciprocal Rank Fusion scores across one or more ranked lists.
// Each list contains string keys (e.g. observation sync_id or numeric ID as string).
// Score for key k = sum over all lists of 1 / (rrfK + rank), where rank is
// 1-based. Keys not present in a list contribute nothing for that list.
//
// Returns a map[key]score. Higher score = better combined rank.
func RRF(lists ...[]string) map[string]float64 {
	weights := make([]float64, len(lists))
	for i := range weights {
		weights[i] = 1.0
	}
	return WeightedRRF(weights, lists...)
}

// WeightedRRF is RRF with a per-list weight: score for key k = sum over lists
// of weight[i] / (rrfK + rank). weights must have the same length as lists.
func WeightedRRF(weights []float64, lists ...[]string) map[string]float64 {
	scores := make(map[string]float64)
	for li, list := range lists {
		w := 1.0
		if li < len(weights) {
			w = weights[li]
		}
		for i, key := range list {
			rank := float64(i + 1) // 1-based
			scores[key] += w / (rrfK + rank)
		}
	}
	return scores
}

// Meta carries metadata about a Search call result.
type Meta struct {
	// Fuzzy is true when the BM25 path fell back to an OR query.
	Fuzzy bool
	// Hybrid is true when vector search was used and the final results were
	// produced by RRF fusion of BM25 and vector scores.
	Hybrid bool
}

// Searcher wraps a Store and an optional Embedder.
// When Embedder is nil, Search returns BM25 results identically to today.
// When Embedder is non-nil, Search attempts hybrid RRF fusion. On any
// embedding error the call falls back to BM25 silently (Hybrid=false).
type Searcher struct {
	store   *store.Store
	embeddr embed.Embedder
}

// NewSearcher creates a Searcher. embedder may be nil (BM25-only mode).
func NewSearcher(st *store.Store, embedder embed.Embedder) *Searcher {
	return &Searcher{store: st, embeddr: embedder}
}

// NewSearcherFromSettings reads embeddings.enabled from the store settings
// and constructs a Searcher accordingly. When enabled="true", it builds an
// OllamaEmbedder from the stored URL and model; otherwise Embedder is nil.
// Always returns a non-nil *Searcher.
func NewSearcherFromSettings(ctx context.Context, st *store.Store) *Searcher {
	enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false")
	if enabled != "true" {
		return &Searcher{store: st, embeddr: nil}
	}

	url := st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
	model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)

	client := embed.DefaultClient(url)
	embedder := embed.NewOllamaEmbedder(client, model)

	return &Searcher{store: st, embeddr: embedder}
}

// Search executes the search and returns results plus metadata.
//
// Flow:
//  1. BM25 via Store.SearchWithFallback (limit*2 candidates, captures fuzzy flag).
//  2. If Embedder == nil: return BM25 results (Hybrid=false). Identical to today.
//  3. If Embedder non-nil: embed the query with a 3-second timeout.
//     On any embed error: return BM25 results (Hybrid=false, error not propagated).
//  4. Vector search (limit*2 candidates via Store.VectorSearch).
//  5. RRF fusion by observation ID, order descending by score, take params.Limit.
//  6. Map back to SearchResults, preserving BM25-side Snippet when available.
func (s *Searcher) Search(ctx context.Context, params store.SearchParams) ([]store.SearchResult, Meta, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}

	// BM25 candidate fetch (limit*2 so we have enough for re-ranking).
	bm25Params := params
	bm25Params.Limit = limit * 2

	bm25Results, fuzzy, err := s.store.SearchWithFallback(ctx, bm25Params)
	if err != nil {
		return nil, Meta{}, err
	}

	meta := Meta{Fuzzy: fuzzy}

	// BM25-only path (Embedder nil).
	if s.embeddr == nil {
		// Truncate to requested limit.
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, meta, nil
	}

	// Embed the query with a 3-second timeout.
	embedCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	queryVec, embedErr := s.embeddr.Embed(embedCtx, params.Q)
	if embedErr != nil {
		// Graceful degradation: fall back to BM25 only.
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, meta, nil
	}

	// Vector search (limit*2 candidates, filtered to the embedder's model so
	// stale vectors from a different embedding space are excluded).
	vecParams := params
	vecParams.Limit = limit * 2
	vecParams.Model = s.embeddr.Model()
	vecResults, vecErr := s.store.VectorSearch(ctx, queryVec, vecParams)
	if vecErr != nil {
		// Fall back to BM25 on vector search error.
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, meta, nil
	}

	// RRF fusion by sync_id.
	// Build ranked lists.
	bm25Keys := make([]string, 0, len(bm25Results))
	for _, r := range bm25Results {
		bm25Keys = append(bm25Keys, r.Observation.SyncID)
	}

	vecKeys := make([]string, 0, len(vecResults))
	for _, r := range vecResults {
		vecKeys = append(vecKeys, r.Observation.SyncID)
	}

	rrfScores := WeightedRRF([]float64{1.0, vectorRRFWeight}, bm25Keys, vecKeys)

	// Build a merged result set indexed by sync_id. Prefer the BM25-side entry
	// (which carries the Snippet) when a doc appears in both.
	byID := make(map[string]store.SearchResult, len(bm25Results)+len(vecResults))
	for _, r := range vecResults {
		byID[r.Observation.SyncID] = r
	}
	for _, r := range bm25Results {
		// BM25 wins because it has the Snippet.
		byID[r.Observation.SyncID] = r
	}

	// Collect all fused results, overwrite Score with RRF rank (-rrfScore so
	// that "lower is better" is preserved).
	fused := make([]store.SearchResult, 0, len(rrfScores))
	for syncID, rrfScore := range rrfScores {
		if r, ok := byID[syncID]; ok {
			// Negate RRF score to align with "lower is better" convention.
			r.Score = -rrfScore
			fused = append(fused, r)
		}
	}

	// Sort ascending by Score (most negative = best RRF rank).
	sort.Slice(fused, func(i, j int) bool {
		return fused[i].Score < fused[j].Score
	})

	if len(fused) > limit {
		fused = fused[:limit]
	}

	meta.Hybrid = true
	return fused, meta, nil
}
