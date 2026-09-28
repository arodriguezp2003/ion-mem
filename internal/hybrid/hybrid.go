// Package hybrid provides RRF fusion of BM25 and vector search results,
// plus a Searcher that gracefully degrades to BM25-only when no embedder
// is configured or when the embedding call fails.
package hybrid

import (
	"context"
	"sort"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// rrfK is the rank-constant used in Reciprocal Rank Fusion.
// A value of 60 is the standard recommendation from the original RRF paper.
const rrfK = 60.0

// DefaultVectorWeight boosts the vector list in the hybrid fusion. Plain RRF is
// biased toward documents that appear in BOTH lists: a lexically-adjacent
// distractor (shares one query word AND is vaguely semantically close) can
// outrank the true semantic match that only the vector list surfaces. A
// weight > 1 on the vector list lets a strong pure-semantic hit compete.
// Calibrated against the golden set (see internal/eval): lexical MeanMRR
// must stay 1.0 while the semantic-gap queries improve.
//
// NewSearcher uses this value; NewSearcherWithWeight lets callers (e.g. the
// eval harness) tune it.
const DefaultVectorWeight = 2.0

// defaultEmbedTimeout bounds the query-embedding call in Search. Callers whose
// embedder may be cold (e.g. the eval harness hitting a freshly-loaded Ollama
// model) can raise this via Searcher.WithEmbedTimeout.
const defaultEmbedTimeout = 3 * time.Second

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
	// LexicalSkipped is true when FusionStrict discarded the lexical (BM25)
	// list because it came from the noisy OR fuzzy fallback (Fuzzy=true),
	// returning the vector ranking alone instead of fusing it in. Always
	// false under FusionAll. When true, Hybrid is false: no fusion happened.
	//
	// Under Mode-based dispatch (see Mode), ModeHybrid also sets this when
	// the AND-only lexical candidate list came back empty, for the same
	// reason: the vector ranking is returned alone.
	LexicalSkipped bool

	// Mode is the search mode that was requested (see Mode). Zero value ""
	// when the Searcher was constructed without a Mode (legacy
	// FusionPolicy-driven dispatch — see Searcher.mode).
	Mode Mode
	// Effective is the mode actually used to produce these results. It
	// differs from Mode when Search degraded to lexical (Degraded=true).
	Effective Mode
	// Degraded is true when a non-lexical mode was requested but the
	// Searcher fell back to lexical-only results — either because
	// embeddings are disabled (see NewSearcherFromSettings) or because an
	// embed/vector-search call failed. Reason explains why.
	Degraded bool
	// Reason is a short human-readable explanation of Degraded. Empty when
	// Degraded is false.
	Reason string
}

// FusionPolicy controls how Searcher.Search combines a fuzzy-fallback
// lexical list with the vector list.
type FusionPolicy string

const (
	// FusionAll fuses the lexical and vector lists unconditionally,
	// regardless of whether the lexical list came from BM25's OR fuzzy
	// fallback. This is the historical behavior and the production default:
	// it never changes results for existing callers.
	FusionAll FusionPolicy = "all"
	// FusionStrict discards the lexical list entirely when it came from the
	// OR fuzzy fallback (Meta.Fuzzy would be true), returning the vector
	// ranking alone instead (Meta.LexicalSkipped=true, Meta.Hybrid=false).
	// A non-fuzzy (exact AND match) lexical list is still fused normally.
	//
	// Rationale: a fuzzy OR fallback list can match on ANY single query term,
	// which is far noisier than a normal BM25 hit — fusing it with the
	// vector list lets that noise drag down otherwise-good vector rankings.
	FusionStrict FusionPolicy = "strict"
)

// storeSearcher is the subset of *store.Store that Searcher depends on.
// Defined as an interface (rather than Searcher holding *store.Store
// directly) so tests can inject a spy that fails one specific call
// deterministically — e.g. Search erroring while VectorSearch still
// succeeds — without needing real database-level failure injection.
// *store.Store satisfies this interface, so production callers are
// unaffected.
type storeSearcher interface {
	Search(ctx context.Context, params store.SearchParams) ([]store.SearchResult, error)
	SearchWithFallback(ctx context.Context, params store.SearchParams) ([]store.SearchResult, bool, error)
	VectorSearch(ctx context.Context, queryVec []float32, params store.SearchParams) ([]store.SearchResult, error)
}

// Searcher wraps a Store and an optional Embedder.
// When Embedder is nil, Search returns BM25 results identically to today.
// When Embedder is non-nil, Search attempts hybrid RRF fusion. On any
// embedding error the call falls back to BM25 silently (Hybrid=false).
type Searcher struct {
	store        storeSearcher
	embeddr      embed.Embedder
	vectorWeight float64
	embedTimeout time.Duration
	fusionPolicy FusionPolicy

	// mode is the production Mode-based dispatch (see Mode and WithMode).
	// Zero value "" preserves the legacy FusionPolicy-driven behavior in
	// Search (searchLegacy) used by the eval harness and any caller built
	// before Mode existed.
	mode Mode

	// degraded, when true, forces Search to run lexical-only regardless of
	// mode, stamping every Meta with Mode: mode, Effective: ModeLexical,
	// Degraded: true, Reason: degradedReason. Set by NewSearcherFromSettings
	// when embeddings are disabled but a non-lexical mode was requested.
	degraded       bool
	degradedReason string
}

// NewSearcher creates a Searcher using DefaultVectorWeight for the vector-list
// RRF weight and FusionAll for the fusion policy. embedder may be nil
// (BM25-only mode).
func NewSearcher(st *store.Store, embedder embed.Embedder) *Searcher {
	return NewSearcherWithWeight(st, embedder, DefaultVectorWeight)
}

// NewSearcherWithWeight is NewSearcher with a configurable vector-list RRF
// weight (see DefaultVectorWeight for the rationale). embedder may be nil
// (BM25-only mode); vectorWeight is then unused. Fusion policy defaults to
// FusionAll; see WithFusionPolicy to change it.
func NewSearcherWithWeight(st *store.Store, embedder embed.Embedder, vectorWeight float64) *Searcher {
	return &Searcher{
		store:        st,
		embeddr:      embedder,
		vectorWeight: vectorWeight,
		embedTimeout: defaultEmbedTimeout,
		fusionPolicy: FusionAll,
	}
}

// WithEmbedTimeout overrides the query-embedding timeout (default 3s, see
// defaultEmbedTimeout) and returns s for chaining. d<=0 is a no-op.
func (s *Searcher) WithEmbedTimeout(d time.Duration) *Searcher {
	if d > 0 {
		s.embedTimeout = d
	}
	return s
}

// WithFusionPolicy overrides the fusion policy (default FusionAll, see
// FusionPolicy) and returns s for chaining. An unrecognized value is a no-op,
// leaving the current policy unchanged.
func (s *Searcher) WithFusionPolicy(p FusionPolicy) *Searcher {
	switch p {
	case FusionAll, FusionStrict:
		s.fusionPolicy = p
	}
	return s
}

// Search executes the search and returns results plus metadata.
//
// Dispatch: when s.mode is unset (the zero value), Search runs the legacy
// FusionPolicy-driven implementation (searchLegacy) unchanged — this is what
// NewSearcher/NewSearcherWithWeight callers (including the eval harness) get.
// When s.degraded is set (see NewSearcherFromSettings), Search always
// returns lexical-only results with Meta.Degraded=true, regardless of mode.
// Otherwise Search dispatches on s.mode (see Mode) to one of searchLexical,
// searchVector, or searchHybridStrict.
func (s *Searcher) Search(ctx context.Context, params store.SearchParams) ([]store.SearchResult, Meta, error) {
	if s.degraded {
		results, fuzzy, err := s.store.SearchWithFallback(ctx, params)
		results = truncateResults(results, resolveLimit(params.Limit))
		return results, Meta{
			Fuzzy:     fuzzy,
			Mode:      s.mode,
			Effective: ModeLexical,
			Degraded:  true,
			Reason:    s.degradedReason,
		}, err
	}

	switch s.mode {
	case ModeLexical:
		return s.searchLexical(ctx, params)
	case ModeVector:
		return s.searchVector(ctx, params)
	case ModeHybrid:
		return s.searchHybridStrict(ctx, params)
	default:
		return s.searchLegacy(ctx, params)
	}
}

// searchLegacy is the original hybrid Search implementation, preserved
// verbatim for callers that never set a Mode (see Search's dispatch
// comment). Its behavior is governed entirely by FusionPolicy, not Mode.
//
// TODO(search-mode follow-up): the only remaining caller of this path is
// internal/eval's ModeHybrid (see eval/mode.go's ModeSearchFn), which
// predates Mode/WithMode and talks to FusionPolicy directly via
// NewSearcherWithWeight(...).WithFusionPolicy(...). It cannot simply switch
// to WithMode(ModeHybrid) as-is: searchHybridStrict ignores fusionPolicy
// entirely (it is always AND-only/strict), so eval's --fusion=all baseline
// would silently start behaving like --fusion=strict. Migrating means
// either teaching searchHybridStrict to honor FusionAll too, or accepting
// that eval's "all" baseline only makes sense via this legacy path. Once
// resolved, delete searchLegacy and the mode=="" branch in Search.
//
// Flow:
//  1. BM25 via Store.SearchWithFallback (limit*2 candidates, captures fuzzy flag).
//  2. If Embedder == nil: return BM25 results (Hybrid=false). Identical to today.
//  3. If Embedder non-nil: embed the query with a 3-second timeout.
//     On any embed error: return BM25 results (Hybrid=false, error not propagated).
//  4. Vector search (limit*2 candidates via Store.VectorSearch).
//  5. RRF fusion by observation ID, order descending by score, take params.Limit.
//  6. Map back to SearchResults, preserving BM25-side Snippet when available.
func (s *Searcher) searchLegacy(ctx context.Context, params store.SearchParams) ([]store.SearchResult, Meta, error) {
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

	// Embed the query, bounded by s.embedTimeout (default 3s; see
	// WithEmbedTimeout).
	embedCtx, cancel := context.WithTimeout(ctx, s.embedTimeout)
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

	// FusionStrict: a fuzzy OR-fallback lexical list is noisy (any single
	// query term can match), so skip it entirely and return the vector
	// ranking alone rather than let it drag down the fusion.
	if s.fusionPolicy == FusionStrict && fuzzy {
		if len(vecResults) > limit {
			vecResults = vecResults[:limit]
		}
		meta.LexicalSkipped = true
		return vecResults, meta, nil
	}

	fused := fuseResults(bm25Results, vecResults, s.vectorWeight, limit)
	meta.Hybrid = true
	return fused, meta, nil
}

// fuseResults combines a lexical (BM25) result list and a vector result
// list via WeightedRRF, keyed by observation sync_id, and returns the top
// limit results ordered by fused score (best first).
//
// The BM25-side entry wins when a document appears in both lists (it
// carries the FTS Snippet). Ties in RRF score break in the deterministic
// order "first list, first rank wins" (bm25Results before vecResults) rather
// than Go's randomized map iteration order — see the inline comments for
// why this matters.
func fuseResults(bm25Results, vecResults []store.SearchResult, vectorWeight float64, limit int) []store.SearchResult {
	// Build ranked lists.
	bm25Keys := make([]string, 0, len(bm25Results))
	for _, r := range bm25Results {
		bm25Keys = append(bm25Keys, r.Observation.SyncID)
	}

	vecKeys := make([]string, 0, len(vecResults))
	for _, r := range vecResults {
		vecKeys = append(vecKeys, r.Observation.SyncID)
	}

	rrfScores := WeightedRRF([]float64{1.0, vectorWeight}, bm25Keys, vecKeys)

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

	// Build the fused list in a deterministic key order (BM25 list first,
	// then any vector-only keys) instead of ranging over rrfScores directly:
	// Go intentionally randomizes map iteration order, which would make the
	// pre-sort order of tied RRF scores nondeterministic across runs even
	// with a stable sort below. Iterating the source lists instead makes
	// "first list, first rank wins ties" a reproducible, documented rule.
	seen := make(map[string]bool, len(rrfScores))
	order := make([]string, 0, len(rrfScores))
	for _, key := range bm25Keys {
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
	}
	for _, key := range vecKeys {
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
	}

	// Collect all fused results, overwrite Score with RRF rank (-rrfScore so
	// that "lower is better" is preserved).
	fused := make([]store.SearchResult, 0, len(order))
	for _, syncID := range order {
		r, ok := byID[syncID]
		if !ok {
			continue
		}
		// Negate RRF score to align with "lower is better" convention.
		r.Score = -rrfScores[syncID]
		fused = append(fused, r)
	}

	// Sort ascending by Score (most negative = best RRF rank). SliceStable
	// preserves the deterministic pre-sort order (above) for tied scores.
	sort.SliceStable(fused, func(i, j int) bool {
		return fused[i].Score < fused[j].Score
	})

	if len(fused) > limit {
		fused = fused[:limit]
	}
	return fused
}

// resolveLimit applies the same "<=0 defaults to 20" rule used throughout
// this package and internal/store.
func resolveLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

// truncateResults returns results trimmed to at most limit entries.
func truncateResults(results []store.SearchResult, limit int) []store.SearchResult {
	if len(results) > limit {
		return results[:limit]
	}
	return results
}

// attachSnippets fills in Snippet for any result whose Snippet is empty
// (vector-search rows never have one — see store.VectorSearch) using
// makeSnippet on the observation's content.
func attachSnippets(results []store.SearchResult) []store.SearchResult {
	for i := range results {
		if results[i].Snippet == "" {
			results[i].Snippet = makeSnippet(results[i].Observation.Content)
		}
	}
	return results
}

// snippetMaxChars bounds makeSnippet's output length.
const snippetMaxChars = 160

// makeSnippet builds a contextual preview for a result that has no FTS
// snippet() output (i.e. every vector-search row): the first ~160
// characters of content, truncated on a rune boundary with a trailing
// ellipsis when longer than that.
func makeSnippet(content string) string {
	runes := []rune(content)
	if len(runes) <= snippetMaxChars {
		return content
	}
	return string(runes[:snippetMaxChars]) + "…"
}

// summarizeErr renders an error as a short, single-line Meta.Reason value.
func summarizeErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// searchLexical runs Store.SearchWithFallback (BM25, AND then OR) and
// nothing else — see ModeLexical.
func (s *Searcher) searchLexical(ctx context.Context, params store.SearchParams) ([]store.SearchResult, Meta, error) {
	results, fuzzy, err := s.store.SearchWithFallback(ctx, params)
	return results, Meta{Fuzzy: fuzzy, Mode: ModeLexical, Effective: ModeLexical}, err
}

// searchVector embeds the query and runs Store.VectorSearch exclusively —
// see ModeVector. On a missing embedder, embed error, or vector-search
// error it falls back to Store.SearchWithFallback with Degraded=true.
func (s *Searcher) searchVector(ctx context.Context, params store.SearchParams) ([]store.SearchResult, Meta, error) {
	degradeToLexical := func(reason string) ([]store.SearchResult, Meta, error) {
		results, fuzzy, err := s.store.SearchWithFallback(ctx, params)
		return results, Meta{
			Fuzzy: fuzzy, Mode: ModeVector, Effective: ModeLexical,
			Degraded: true, Reason: reason,
		}, err
	}

	if s.embeddr == nil {
		return degradeToLexical("no embedder configured")
	}

	embedCtx, cancel := context.WithTimeout(ctx, s.embedTimeout)
	defer cancel()
	queryVec, embedErr := s.embeddr.Embed(embedCtx, params.Q)
	if embedErr != nil {
		return degradeToLexical(summarizeErr(embedErr))
	}

	vecParams := params
	vecParams.Model = s.embeddr.Model()
	results, vecErr := s.store.VectorSearch(ctx, queryVec, vecParams)
	if vecErr != nil {
		return degradeToLexical(summarizeErr(vecErr))
	}

	results = attachSnippets(results)
	return results, Meta{Mode: ModeVector, Effective: ModeVector}, nil
}

// searchHybridStrict runs an AND-only lexical candidate list (Store.Search
// — the OR fuzzy fallback is never executed) alongside the vector path,
// fusing the two with WeightedRRF when the lexical list is non-empty — see
// ModeHybrid. When the AND query matches nothing, the vector ranking is
// returned alone (LexicalSkipped=true, Hybrid=false). On a missing
// embedder, embed error, or vector-search error it falls back to
// Store.SearchWithFallback with Degraded=true.
func (s *Searcher) searchHybridStrict(ctx context.Context, params store.SearchParams) ([]store.SearchResult, Meta, error) {
	limit := resolveLimit(params.Limit)

	degradeToLexical := func(reason string) ([]store.SearchResult, Meta, error) {
		results, fuzzy, err := s.store.SearchWithFallback(ctx, params)
		return results, Meta{
			Fuzzy: fuzzy, Mode: ModeHybrid, Effective: ModeLexical,
			Degraded: true, Reason: reason,
		}, err
	}

	if s.embeddr == nil {
		return degradeToLexical("no embedder configured")
	}

	embedCtx, cancel := context.WithTimeout(ctx, s.embedTimeout)
	defer cancel()
	queryVec, embedErr := s.embeddr.Embed(embedCtx, params.Q)
	if embedErr != nil {
		return degradeToLexical(summarizeErr(embedErr))
	}

	// Vector search first (computed before the lexical call below) so that,
	// if the lexical call errors, we already have a vector-only ranking to
	// degrade to instead of returning nil results — see the lexErr branch.
	vecParams := params
	vecParams.Limit = limit * 2
	vecParams.Model = s.embeddr.Model()
	vecResults, vecErr := s.store.VectorSearch(ctx, queryVec, vecParams)
	if vecErr != nil {
		return degradeToLexical(summarizeErr(vecErr))
	}
	vecResults = attachSnippets(vecResults)

	// AND-only lexical candidates: deliberately Store.Search, never
	// SearchWithFallback, so the noisy OR fuzzy fallback is never executed
	// in hybrid mode (see ModeHybrid).
	lexParams := params
	lexParams.Limit = limit * 2
	lexResults, lexErr := s.store.Search(ctx, lexParams)
	if lexErr != nil {
		// Mirror the vecErr branch above, but in reverse: lexical failed
		// here (not vector), so degrade to the vector-only ranking we
		// already computed instead of returning nil results.
		vecOnly := truncateResults(vecResults, limit)
		return vecOnly, Meta{
			Mode: ModeHybrid, Effective: ModeVector,
			Degraded: true, Reason: summarizeErr(lexErr),
		}, nil
	}

	if len(lexResults) == 0 {
		vecResults = truncateResults(vecResults, limit)
		return vecResults, Meta{Mode: ModeHybrid, Effective: ModeHybrid, LexicalSkipped: true}, nil
	}

	fused := fuseResults(lexResults, vecResults, s.vectorWeight, limit)
	return fused, Meta{Mode: ModeHybrid, Effective: ModeHybrid, Hybrid: true}, nil
}
