package eval

import (
	"context"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ionix/ion-mem/internal/store"
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

// Run executes each golden query against st for the given project, computes
// PrecisionAtK and MRR for each, and returns an aggregated Report.
// k is the cutoff for precision; use 5 as the standard value.
func Run(ctx context.Context, st *store.Store, queries []GoldenQuery, project string, k int) (Report, error) {
	if k <= 0 {
		k = 5
	}

	var r Report

	var normalPrecisions, normalMRRs []float64

	for _, q := range queries {
		results, _, err := st.SearchWithFallback(ctx, store.SearchParams{
			Q:       q.Query,
			Project: project,
			Limit:   k,
		})
		if err != nil {
			return Report{}, fmt.Errorf("eval.Run query %q: %w", q.ID, err)
		}

		// Metrics are computed over exactly the top-k results so the
		// expect_fail contract ("no expected title within k") holds and
		// cannot flake when a doc drifts into ranks k+1..k*2.
		got := make([]string, 0, len(results))
		for _, res := range results {
			got = append(got, res.Observation.Title)
		}
		if len(got) > k {
			got = got[:k]
		}

		topHit := ""
		if len(got) > 0 {
			topHit = got[0]
		}

		mrr := MRR(q.Expected, got)
		p := PrecisionAtK(q.Expected, got, k)

		qr := QueryResult{
			ID:         q.ID,
			Query:      q.Query,
			MRR:        mrr,
			PrecisionK: p,
			TopHit:     topHit,
			ExpectFail: q.ExpectFail,
		}

		if q.ExpectFail {
			r.KnownGaps = append(r.KnownGaps, qr)
		} else {
			r.PerQuery = append(r.PerQuery, qr)
			normalPrecisions = append(normalPrecisions, p)
			normalMRRs = append(normalMRRs, mrr)
		}
	}

	if len(normalPrecisions) > 0 {
		r.MeanPrecisionAt5 = mean(normalPrecisions)
	}
	if len(normalMRRs) > 0 {
		r.MeanMRR = mean(normalMRRs)
	}

	return r, nil
}

// SearchFn is the signature used by RunWithSearchFn to allow the caller to
// inject any search backend (BM25-only, hybrid RRF, etc.) without changing
// the eval.Run signature.
//
// The function must return results in score order (best first) and may return
// a non-nil Meta for context (e.g. Hybrid=true), which is currently ignored
// by the runner but available for future reporting.
type SearchFn func(ctx context.Context, params store.SearchParams) ([]store.SearchResult, error)

// RunWithSearchFn is identical to Run except the caller supplies a SearchFn
// instead of having the runner call store.SearchWithFallback directly. This
// is the entry point used by the CLI when --embeddings is active.
//
// k is the precision cutoff; use 5 as the standard value.
func RunWithSearchFn(ctx context.Context, search SearchFn, queries []GoldenQuery, project string, k int) (Report, error) {
	if k <= 0 {
		k = 5
	}

	var r Report

	var normalPrecisions, normalMRRs []float64

	for _, q := range queries {
		results, err := search(ctx, store.SearchParams{
			Q:       q.Query,
			Project: project,
			Limit:   k,
		})
		if err != nil {
			return Report{}, fmt.Errorf("eval.RunWithSearchFn query %q: %w", q.ID, err)
		}

		got := make([]string, 0, len(results))
		for _, res := range results {
			got = append(got, res.Observation.Title)
		}
		if len(got) > k {
			got = got[:k]
		}

		topHit := ""
		if len(got) > 0 {
			topHit = got[0]
		}

		mrr := MRR(q.Expected, got)
		p := PrecisionAtK(q.Expected, got, k)

		qr := QueryResult{
			ID:         q.ID,
			Query:      q.Query,
			MRR:        mrr,
			PrecisionK: p,
			TopHit:     topHit,
			ExpectFail: q.ExpectFail,
		}

		if q.ExpectFail {
			r.KnownGaps = append(r.KnownGaps, qr)
		} else {
			r.PerQuery = append(r.PerQuery, qr)
			normalPrecisions = append(normalPrecisions, p)
			normalMRRs = append(normalMRRs, mrr)
		}
	}

	if len(normalPrecisions) > 0 {
		r.MeanPrecisionAt5 = mean(normalPrecisions)
	}
	if len(normalMRRs) > 0 {
		r.MeanMRR = mean(normalMRRs)
	}

	return r, nil
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
