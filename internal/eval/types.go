// Package eval provides the golden-set search evaluation harness for ion-mem.
//
// It defines the corpus and query types, retrieval metrics, a runner that seeds
// a store, executes queries, and aggregates results, and supporting helpers for
// loading YAML fixtures.
package eval

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

// QueryResult holds the per-query evaluation output.
type QueryResult struct {
	ID         string
	Query      string
	MRR        float64
	PrecisionK float64
	TopHit     string // title of the first result, or "" if no results
	ExpectFail bool
}

// Report aggregates evaluation results across all golden queries.
//
// Aggregate metrics (MeanPrecisionAt5, MeanMRR) are computed only over queries
// where ExpectFail=false. ExpectFail queries appear in KnownGaps.
type Report struct {
	PerQuery          []QueryResult
	KnownGaps         []QueryResult // ExpectFail queries
	MeanPrecisionAt5  float64
	MeanMRR           float64
}
