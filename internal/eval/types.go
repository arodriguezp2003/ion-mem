// Package eval provides the golden-set search evaluation harness for ion-mem.
//
// It defines the corpus and query types, retrieval metrics, a runner that seeds
// a store, executes queries, and aggregates results, and supporting helpers for
// loading YAML fixtures.
package eval

import "time"

// GoldenQuery describes one test query and its expected ranked results.
//
// Expected lists observation titles in the ideal rank order; titles must be
// unique within the corpus so they serve as stable identifiers.
// ExpectFail=true marks queries that the current lexical engine cannot satisfy
// (e.g. semantic-gap cases). These are still executed but tracked in
// Report.KnownGaps rather than counted in aggregate metrics.
type GoldenQuery struct {
	ID         string   `yaml:"id"`
	Query      string   `yaml:"query"`
	Expected   []string `yaml:"expected"` // observation titles in expected rank order
	ExpectFail bool     `yaml:"expect_fail"`
	Note       string   `yaml:"note"`
	// Project overrides the runner's default project for this query only.
	// Empty (the common case) means "use the project passed to Run /
	// RunWithSearchFn". Combined with --all-projects / --project "" at the
	// CLI level, this lets a single golden set mix project-scoped and
	// cross-project queries.
	Project string `yaml:"project"`
	// Category is an optional free-form label (e.g. "lexical", "semantic-gap",
	// "kebab-identifier") echoed through to QueryResult for slicing reports.
	// It has no effect on scoring.
	Category string `yaml:"category"`
}

// CorpusDoc describes one synthetic observation to seed into the evaluation store.
//
// AgeDays controls how far back last_seen_at and created_at are backdated so
// the recency decay in SearchWithFallback applies realistically.
type CorpusDoc struct {
	Title    string `yaml:"title"`
	Content  string `yaml:"content"`
	Type     string `yaml:"type"`
	TopicKey string `yaml:"topic_key"` // optional; empty = no topic_key
	AgeDays  int    `yaml:"age_days"`
}

// TopResult is one ranked search hit surfaced in a QueryResult for reporting.
// QueryResult.Top keeps at most the top 10 per query (see RunWithSearchFn).
type TopResult struct {
	Rank    int
	Title   string
	Score   float64
	Project string
}

// QueryResult holds the per-query evaluation output.
//
// Project, Category, Expected, and ExpectFail are echoed from the GoldenQuery
// fixture (Project after per-query override resolution) purely for reporting
// convenience; they do not affect how the metrics below are computed.
//
// See RunWithSearchFn for exactly which result window each metric is
// evaluated over (MRR/NDCGAt10 vs. PrecisionK/RecallAtK/RecallAt10/HitAt1).
type QueryResult struct {
	ID         string
	Query      string
	Project    string
	Category   string
	Expected   []string
	ExpectFail bool

	MRR        float64 // reciprocal rank over the full fetched window
	HitAt1     float64 // 1.0 if the very first result is expected, else 0.0
	PrecisionK float64 // precision at the caller's k (kept name for backward compat)
	RecallAtK  float64 // recall at the caller's k
	RecallAt10 float64 // recall at a fixed cutoff of 10
	NDCGAt10   float64 // NDCG at a fixed cutoff of 10 (binary relevance)

	Latency time.Duration // wall-clock time of the search call for this query

	Hybrid         bool // Meta.Hybrid from the search backend, when applicable
	Fuzzy          bool // Meta.Fuzzy (OR-fallback) from the search backend
	LexicalSkipped bool // Meta.LexicalSkipped: hybrid.FusionStrict dropped a fuzzy lexical list (see SearchMeta)

	// Err holds the search error's message when this query's SearchFn call
	// failed. Empty on success. A failed query still gets a QueryResult (not
	// dropped from the run): every metric above is left at its zero value —
	// the same "no results" shape a real empty result set would produce —
	// and TopHit/Top are empty. See RunWithSearchFn for when the whole run
	// aborts instead of recording per-query errors.
	Err string

	TopHit string      // title of the first result, or "" if no results
	Top    []TopResult // up to the top 10 ranked hits, for JSON reporting
}

// LatencyStats aggregates a slice of per-query search latencies. See
// AggregateLatency in metrics.go.
type LatencyStats struct {
	P50  time.Duration
	P95  time.Duration
	Mean time.Duration
}

// Report aggregates evaluation results across all golden queries.
//
// Aggregate metrics (MeanPrecisionAt5, MeanMRR, ...) are computed only over
// queries where ExpectFail=false. ExpectFail queries appear in KnownGaps and
// are excluded from every aggregate below.
type Report struct {
	PerQuery         []QueryResult
	KnownGaps        []QueryResult // ExpectFail queries
	MeanPrecisionAt5 float64       // MeanPrecisionAtK computed at the caller's k; name kept for backward compat
	MeanMRR          float64
	MeanHitAt1       float64
	MeanRecallAtK    float64
	MeanRecallAt10   float64
	MeanNDCGAt10     float64

	Latency LatencyStats

	// HybridRan counts queries (across PerQuery and KnownGaps) whose search
	// backend reported Meta.Hybrid=true. It is 0 for lexical/vector modes and
	// meaningful mainly for --mode hybrid, where the CLI compares it against
	// the total query count to detect silent per-query fallbacks.
	HybridRan int

	// LexicalSkipped counts queries (across PerQuery and KnownGaps) whose
	// search backend reported Meta.LexicalSkipped=true — i.e. hybrid mode
	// under FusionStrict deliberately dropped a fuzzy-fallback lexical list.
	// Always 0 outside --mode hybrid --fusion strict.
	LexicalSkipped int

	// Errors counts queries (across PerQuery and KnownGaps) whose SearchFn
	// call returned an error; ErrorIDs lists their GoldenQuery.ID values in
	// encounter order. A per-query search error does not abort the run (see
	// RunWithSearchFn) unless EVERY query failed, or the golden set was
	// empty — those are the only cases RunWithSearchFn itself returns a
	// non-nil error for.
	Errors   int
	ErrorIDs []string
}
