package eval

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// LoadCorpus reads a YAML file at path and returns the parsed corpus documents.
func LoadCorpus(path string) ([]CorpusDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval.LoadCorpus: read %q: %w", path, err)
	}
	var docs []CorpusDoc
	if err := yaml.Unmarshal(data, &docs); err != nil {
		return nil, fmt.Errorf("eval.LoadCorpus: parse %q: %w", path, err)
	}
	return docs, nil
}

// LoadGolden reads a YAML file at path and returns the parsed golden queries.
func LoadGolden(path string) ([]GoldenQuery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval.LoadGolden: read %q: %w", path, err)
	}
	var queries []GoldenQuery
	if err := yaml.Unmarshal(data, &queries); err != nil {
		return nil, fmt.Errorf("eval.LoadGolden: parse %q: %w", path, err)
	}
	return queries, nil
}

// evalSessionID is the fixed session identifier used when seeding the corpus.
// A real session row must exist before observations can be inserted (FK constraint).
const evalSessionID = "eval-seed-session"

// SeedCorpus inserts docs into st under project and backdates each observation
// by its AgeDays so recency decay is realistic. It is idempotent with respect
// to the project: re-seeding the same corpus to the same project is safe but
// will add duplicate rows (use a fresh temp store per evaluation run).
func SeedCorpus(ctx context.Context, st *store.Store, docs []CorpusDoc, project string) error {
	// Ensure the eval session exists (FK constraint on observations.session_id).
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID:        evalSessionID,
		Project:   project,
		Directory: "/eval",
	}); err != nil {
		// Ignore duplicate-session errors; the session may already exist.
		if !isUniqueErr(err) {
			return fmt.Errorf("eval.SeedCorpus: create session: %w", err)
		}
	}

	for _, d := range docs {
		params := store.AddObservationParams{
			SessionID: evalSessionID,
			Type:      d.Type,
			Title:     d.Title,
			Content:   d.Content,
			Project:   project,
			Scope:     "project",
			TopicKey:  d.TopicKey,
		}
		obs, err := st.AddObservation(ctx, params)
		if err != nil {
			return fmt.Errorf("eval.SeedCorpus: insert %q: %w", d.Title, err)
		}
		if d.AgeDays > 0 {
			if err := st.BackdateObservation(ctx, obs.ID, d.AgeDays); err != nil {
				return fmt.Errorf("eval.SeedCorpus: backdate %q: %w", d.Title, err)
			}
		}
	}
	return nil
}

// isUniqueErr reports whether err is a SQLite UNIQUE constraint violation.
func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") ||
		strings.Contains(s, "unique constraint")
}

// SearchMeta carries metadata about one search call. It mirrors hybrid.Meta
// field-for-field, but the eval package defines its own type so ModeLexical
// and ModeVector (see mode.go) don't need to depend on internal/hybrid just
// to report Fuzzy/Hybrid flags.
type SearchMeta struct {
	// Hybrid is true when the search backend fused BM25 and vector results
	// (see hybrid.Meta.Hybrid). Always false for ModeLexical and ModeVector.
	Hybrid bool
	// Fuzzy is true when the BM25 path fell back to an OR query.
	Fuzzy bool
	// LexicalSkipped is true when hybrid.FusionStrict deliberately dropped a
	// fuzzy-fallback lexical list, returning the vector ranking alone (see
	// hybrid.Meta.LexicalSkipped). Always false for ModeLexical, ModeVector,
	// and ModeHybrid under FusionAll. Unlike a true silent fallback (Hybrid
	// false with LexicalSkipped also false, e.g. an embed error), this is a
	// deliberate policy decision — callers should not treat it as a warning.
	LexicalSkipped bool
}

// SearchFn is the signature RunWithSearchFn uses to allow the caller to
// inject any search backend (BM25-only, vector-only, hybrid RRF, etc.)
// without changing the eval.Run signature. See mode.go's ModeSearchFn for the
// standard constructors.
//
// The function must return results in score order (best first).
type SearchFn func(ctx context.Context, params store.SearchParams) ([]store.SearchResult, SearchMeta, error)

// minFetchWindow is the minimum number of results RunWithSearchFn requests
// per query, regardless of the caller's k. See RunWithSearchFn's doc for why.
const minFetchWindow = 10

// Run executes each golden query against st via SearchWithFallback (BM25 +
// OR fallback) for the given default project, and returns an aggregated
// Report. It is equivalent to RunWithSearchFn with ModeSearchFn(ModeLexical, ...).
// k is the cutoff for precision/recall; use 5 as the standard value.
func Run(ctx context.Context, st *store.Store, queries []GoldenQuery, project string, k int) (Report, error) {
	search := func(ctx context.Context, params store.SearchParams) ([]store.SearchResult, SearchMeta, error) {
		results, fuzzy, err := st.SearchWithFallback(ctx, params)
		return results, SearchMeta{Fuzzy: fuzzy}, err
	}
	return RunWithSearchFn(ctx, search, queries, project, k)
}

// RunWithSearchFn is identical to Run except the caller supplies a SearchFn
// instead of having the runner call store.SearchWithFallback directly. This
// is the entry point used by the CLI for --mode vector/hybrid.
//
// Per-query project override: when a GoldenQuery.Project is non-empty, it
// replaces project for that query only (see GoldenQuery.Project).
//
// Metric fetch window: each query is searched ONCE with
// Limit = max(k, minFetchWindow) (minFetchWindow=10). MRR and NDCGAt10 are
// computed over that full fetched window, so a relevant doc ranked e.g. 7th
// still contributes to MRR even when k=5. PrecisionK, RecallAtK, and HitAt1
// are computed against the SAME fetched window, truncated to k, k, and 1
// results respectively. This keeps a single search call per query (important
// for --mode vector/hybrid, which are network-bound) while still reporting
// ranking-quality metrics beyond the precision cutoff.
//
// Per-query error handling: a SearchFn error for one query does NOT abort
// the run. That query still gets a QueryResult (routed to PerQuery or
// KnownGaps exactly as a successful query would be, by ExpectFail), with
// QueryResult.Err set to the error's message and every metric at its zero
// value (the same shape an empty result set would produce) — see
// QueryResult.Err. Report.Errors and Report.ErrorIDs summarize failures
// across the whole run. RunWithSearchFn itself returns a non-nil error (and
// a zero Report) only when EVERY query failed, or when queries is empty —
// both cases where there is nothing meaningful to report.
//
// k is the precision/recall cutoff; use 5 as the standard value.
func RunWithSearchFn(ctx context.Context, search SearchFn, queries []GoldenQuery, project string, k int) (Report, error) {
	if len(queries) == 0 {
		return Report{}, fmt.Errorf("eval.RunWithSearchFn: no golden queries to run")
	}

	if k <= 0 {
		k = 5
	}
	fetchLimit := k
	if fetchLimit < minFetchWindow {
		fetchLimit = minFetchWindow
	}

	var r Report
	var normalPrecisions, normalMRRs, normalHits, normalRecallK, normalRecall10, normalNDCG10 []float64
	var latencies []time.Duration
	var lastErr error

	for _, q := range queries {
		effProject := project
		if q.Project != "" {
			effProject = q.Project
		}

		start := time.Now()
		results, meta, err := search(ctx, store.SearchParams{
			Q:       q.Query,
			Project: effProject,
			Limit:   fetchLimit,
		})
		latency := time.Since(start)

		var errStr string
		if err != nil {
			errStr = err.Error()
			lastErr = err
			r.Errors++
			r.ErrorIDs = append(r.ErrorIDs, q.ID)
			// Discard whatever the SearchFn returned alongside the error, so
			// a failed query always gets the same clean "no results" shape
			// regardless of what a misbehaving SearchFn implementation did.
			results = nil
			meta = SearchMeta{}
		}

		got := make([]string, 0, len(results))
		for _, res := range results {
			got = append(got, res.Observation.Title)
		}

		topHit := ""
		if len(got) > 0 {
			topHit = got[0]
		}

		gotK := truncate(got, k)
		got10 := truncate(got, minFetchWindow)
		got1 := truncate(got, 1)

		qr := QueryResult{
			ID:         q.ID,
			Query:      q.Query,
			Project:    effProject,
			Category:   q.Category,
			Expected:   q.Expected,
			ExpectFail: q.ExpectFail,

			MRR:        MRR(q.Expected, got),
			HitAt1:     HitAtK(q.Expected, got1, 1),
			PrecisionK: PrecisionAtK(q.Expected, gotK, k),
			RecallAtK:  RecallAtK(q.Expected, gotK, k),
			RecallAt10: RecallAtK(q.Expected, got10, minFetchWindow),
			NDCGAt10:   NDCGAtK(q.Expected, got10, minFetchWindow),

			Latency:        latency,
			Hybrid:         meta.Hybrid,
			Fuzzy:          meta.Fuzzy,
			LexicalSkipped: meta.LexicalSkipped,

			Err: errStr,

			TopHit: topHit,
			Top:    buildTop(results, minFetchWindow),
		}

		latencies = append(latencies, latency)
		if meta.Hybrid {
			r.HybridRan++
		}
		if meta.LexicalSkipped {
			r.LexicalSkipped++
		}

		if q.ExpectFail {
			r.KnownGaps = append(r.KnownGaps, qr)
		} else {
			r.PerQuery = append(r.PerQuery, qr)
			normalPrecisions = append(normalPrecisions, qr.PrecisionK)
			normalMRRs = append(normalMRRs, qr.MRR)
			normalHits = append(normalHits, qr.HitAt1)
			normalRecallK = append(normalRecallK, qr.RecallAtK)
			normalRecall10 = append(normalRecall10, qr.RecallAt10)
			normalNDCG10 = append(normalNDCG10, qr.NDCGAt10)
		}
	}

	if r.Errors == len(queries) {
		return Report{}, fmt.Errorf("eval.RunWithSearchFn: all %d queries failed (last error for query %q: %w)",
			r.Errors, r.ErrorIDs[len(r.ErrorIDs)-1], lastErr)
	}

	r.MeanPrecisionAt5 = mean(normalPrecisions)
	r.MeanMRR = mean(normalMRRs)
	r.MeanHitAt1 = mean(normalHits)
	r.MeanRecallAtK = mean(normalRecallK)
	r.MeanRecallAt10 = mean(normalRecall10)
	r.MeanNDCGAt10 = mean(normalNDCG10)
	r.Latency = AggregateLatency(latencies)

	return r, nil
}

// truncate returns got cut down to at most n elements. n<=0 or n>=len(got)
// returns got unchanged (never grows the slice).
func truncate(got []string, n int) []string {
	if n <= 0 || n >= len(got) {
		return got
	}
	return got[:n]
}

// buildTop converts up to the first n search results into TopResult entries
// (1-based Rank) for JSON reporting.
func buildTop(results []store.SearchResult, n int) []TopResult {
	if len(results) < n {
		n = len(results)
	}
	if n <= 0 {
		return nil
	}
	top := make([]TopResult, 0, n)
	for i := 0; i < n; i++ {
		r := results[i]
		top = append(top, TopResult{
			Rank:    i + 1,
			Title:   r.Observation.Title,
			Score:   r.Score,
			Project: r.Observation.Project,
		})
	}
	return top
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
