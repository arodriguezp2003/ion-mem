package hybrid

import (
	"context"
	"fmt"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// Mode selects the production retrieval strategy a Searcher dispatches to.
// It is orthogonal to FusionPolicy: FusionPolicy only tunes the legacy
// fusion behavior used when Mode is unset (the zero value), which is what
// the eval harness and any caller constructed before Mode existed continue
// to get. Mode is what NewSearcherFromSettings uses to pick one of three
// dedicated code paths (see Searcher.Search).
type Mode string

const (
	// ModeLexical runs Store.SearchWithFallback (BM25, AND then OR) and
	// nothing else. No embedder is ever consulted, even if one is configured.
	ModeLexical Mode = "lexical"
	// ModeVector embeds the query and runs Store.VectorSearch exclusively —
	// no BM25 at all. On an embed or vector-search error it falls back to
	// Store.SearchWithFallback (Meta.Degraded=true).
	ModeVector Mode = "vector"
	// ModeHybrid runs Store.Search (AND-only lexical candidates — the noisy
	// OR fuzzy fallback is never executed) alongside the vector path, fusing
	// the two lists with WeightedRRF when the lexical list is non-empty.
	// When the AND query matches nothing, the vector ranking is returned
	// alone (Meta.LexicalSkipped=true). On an embed or vector-search error it
	// falls back to Store.SearchWithFallback (Meta.Degraded=true).
	ModeHybrid Mode = "hybrid"
)

// ParseMode validates a search.mode setting (or --mode flag) value.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeLexical, ModeVector, ModeHybrid:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("hybrid.ParseMode: unknown mode %q (want %q, %q, or %q)", s, ModeLexical, ModeVector, ModeHybrid)
	}
}

// WithMode sets the Mode-based dispatch (see Mode) and returns s for
// chaining. An unrecognized value is a no-op, leaving the current mode
// unchanged — mirroring WithFusionPolicy's defensive behavior. Leaving Mode
// unset (the zero value) keeps the legacy FusionPolicy-driven behavior.
func (s *Searcher) WithMode(m Mode) *Searcher {
	switch m {
	case ModeLexical, ModeVector, ModeHybrid:
		s.mode = m
	}
	return s
}

// NewSearcherFromSettings reads search.mode and embeddings.enabled from the
// store settings and constructs a Searcher accordingly.
//
// Resolution:
//   - mode := search.mode setting, defaulting to store.DefaultSearchMode
//     ("vector") when unset or invalid.
//   - When embeddings.enabled != "true" and mode != lexical: the Searcher is
//     forced into lexical dispatch, and every Search call reports
//     Meta{Mode: mode, Effective: ModeLexical, Degraded: true,
//     Reason: "embeddings disabled"}. Lexical mode itself never needs
//     embeddings, so requesting it with embeddings disabled is NOT degraded.
//   - Otherwise builds an OllamaEmbedder from the stored URL/model (when
//     embeddings are enabled) and configures the Searcher for mode.
//
// Always returns a non-nil *Searcher using DefaultVectorWeight.
func NewSearcherFromSettings(ctx context.Context, st *store.Store) *Searcher {
	enabled := st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false") == "true"

	modeStr := st.SettingOrDefault(ctx, store.SettingSearchMode, store.DefaultSearchMode)
	mode, err := ParseMode(modeStr)
	if err != nil {
		// A corrupted/hand-edited setting must not crash search; fall back to
		// the product default rather than propagating a parse error.
		mode, _ = ParseMode(store.DefaultSearchMode)
	}

	if !enabled && mode != ModeLexical {
		s := NewSearcherWithWeight(st, nil, DefaultVectorWeight)
		s.mode = mode
		s.degraded = true
		s.degradedReason = "embeddings disabled"
		return s
	}

	var embedder embed.Embedder
	if enabled {
		url := st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
		model := st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)
		client := embed.DefaultClient(url)
		embedder = embed.NewOllamaEmbedder(client, model)
	}

	s := NewSearcherWithWeight(st, embedder, DefaultVectorWeight)
	s.mode = mode
	return s
}
