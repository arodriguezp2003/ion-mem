package eval

import (
	"context"
	"fmt"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// Mode selects which retrieval backend ModeSearchFn wires up for the eval
// harness.
type Mode string

const (
	// ModeLexical evaluates st.SearchWithFallback (BM25 + OR fallback). This
	// is the historical `ion-mem eval` behavior and requires no embedder.
	ModeLexical Mode = "lexical"
	// ModeVector evaluates pure vector search (embed the query, then
	// st.VectorSearch). Requires a non-nil embedder; unlike hybrid, a failed
	// embed call is a hard error rather than a silent BM25 fallback, since
	// there is no lexical component to fall back to.
	ModeVector Mode = "vector"
	// ModeHybrid evaluates hybrid.Searcher (BM25 + vector, fused by RRF).
	// Requires a non-nil embedder; gracefully degrades to BM25-only per query
	// on embed/vector errors (see hybrid.Searcher.Search), which is why the
	// runner reports Report.HybridRan.
	ModeHybrid Mode = "hybrid"
)

// DefaultEmbedTimeout is the query-embedding timeout ModeSearchFn uses for
// vector and hybrid mode. It is longer than hybrid.Searcher's own 3s default
// because an eval run is often the first query against a cold Ollama model.
const DefaultEmbedTimeout = 10 * time.Second

// ParseMode validates a --mode flag value. It does not resolve an empty
// string to a default mode; callers (the CLI) decide the default (lexical,
// or hybrid when --embeddings is set) before calling ParseMode.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeLexical, ModeVector, ModeHybrid:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("eval.ParseMode: unknown mode %q (want %q, %q, or %q)", s, ModeLexical, ModeVector, ModeHybrid)
	}
}

// ModeSearchFn builds the SearchFn that RunWithSearchFn uses for mode.
//
//   - ModeLexical: st.SearchWithFallback. embedder is unused and may be nil.
//   - ModeVector:  embeds the query (bounded by embedTimeout, or
//     DefaultEmbedTimeout when embedTimeout<=0), then st.VectorSearch filtered
//     to embedder.Model(). Requires a non-nil embedder. Returns an error from
//     the SearchFn on any embed or vector-search failure — vector mode has no
//     fallback path.
//   - ModeHybrid: hybrid.NewSearcherWithWeight(st, embedder, vectorWeight),
//     with its embed timeout raised to embedTimeout (or DefaultEmbedTimeout)
//     and its fusion policy set to fusionPolicy. Requires a non-nil embedder.
//     Falls back to BM25 silently on embed/vector errors (Meta.Hybrid=false);
//     callers surface that as a warning (see cmd/ion-mem's runEval). Under
//     hybrid.FusionStrict, a fuzzy OR-fallback lexical list is instead
//     deliberately dropped (Meta.Hybrid=false, Meta.LexicalSkipped=true) —
//     see hybrid.FusionPolicy; callers should treat that as expected
//     behavior, not a fallback warning.
//
// vectorWeight and fusionPolicy are only used by ModeHybrid. embedTimeout<=0
// resolves to DefaultEmbedTimeout for both ModeVector and ModeHybrid.
func ModeSearchFn(mode Mode, st *store.Store, embedder embed.Embedder, vectorWeight float64, embedTimeout time.Duration, fusionPolicy hybrid.FusionPolicy) (SearchFn, error) {
	if embedTimeout <= 0 {
		embedTimeout = DefaultEmbedTimeout
	}

	switch mode {
	case ModeLexical:
		return func(ctx context.Context, params store.SearchParams) ([]store.SearchResult, SearchMeta, error) {
			results, fuzzy, err := st.SearchWithFallback(ctx, params)
			return results, SearchMeta{Fuzzy: fuzzy}, err
		}, nil

	case ModeVector:
		if embedder == nil {
			return nil, fmt.Errorf("eval.ModeSearchFn: mode %q requires an embedder", mode)
		}
		return func(ctx context.Context, params store.SearchParams) ([]store.SearchResult, SearchMeta, error) {
			embedCtx, cancel := context.WithTimeout(ctx, embedTimeout)
			defer cancel()

			queryVec, err := embedder.Embed(embedCtx, params.Q)
			if err != nil {
				return nil, SearchMeta{}, fmt.Errorf("eval.ModeSearchFn vector: embed query: %w", err)
			}

			vecParams := params
			vecParams.Model = embedder.Model()
			results, err := st.VectorSearch(ctx, queryVec, vecParams)
			if err != nil {
				return nil, SearchMeta{}, fmt.Errorf("eval.ModeSearchFn vector: %w", err)
			}
			return results, SearchMeta{}, nil
		}, nil

	case ModeHybrid:
		if embedder == nil {
			return nil, fmt.Errorf("eval.ModeSearchFn: mode %q requires an embedder", mode)
		}
		searcher := hybrid.NewSearcherWithWeight(st, embedder, vectorWeight).
			WithEmbedTimeout(embedTimeout).
			WithFusionPolicy(fusionPolicy)
		return func(ctx context.Context, params store.SearchParams) ([]store.SearchResult, SearchMeta, error) {
			results, meta, err := searcher.Search(ctx, params)
			return results, SearchMeta{Hybrid: meta.Hybrid, Fuzzy: meta.Fuzzy, LexicalSkipped: meta.LexicalSkipped}, err
		}, nil

	default:
		return nil, fmt.Errorf("eval.ModeSearchFn: unknown mode %q", mode)
	}
}
