package eval_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/eval"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// TestRunWithSearchFn_PerQueryError_RecordsErrAndContinues verifies that a
// search error for ONE query no longer aborts the whole run: the failing
// query gets a QueryResult with Err set and all-zero metrics, other queries
// are still scored normally, and Report.Errors/ErrorIDs reflect the failure.
func TestRunWithSearchFn_PerQueryError_RecordsErrAndContinues(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom: connection reset")

	search := func(_ context.Context, params store.SearchParams) ([]store.SearchResult, eval.SearchMeta, error) {
		if params.Q == "bad-query" {
			return nil, eval.SearchMeta{}, boom
		}
		return []store.SearchResult{
			{Observation: store.Observation{Title: "good doc"}},
		}, eval.SearchMeta{}, nil
	}

	queries := []eval.GoldenQuery{
		{ID: "Q1", Query: "good-query-1", Expected: []string{"good doc"}},
		{ID: "Q2", Query: "bad-query", Expected: []string{"anything"}},
		{ID: "Q3", Query: "good-query-2", Expected: []string{"good doc"}},
	}

	report, err := eval.RunWithSearchFn(ctx, search, queries, "proj", 5)
	if err != nil {
		t.Fatalf("RunWithSearchFn: a single failing query must not abort the run: %v", err)
	}

	if report.Errors != 1 {
		t.Errorf("report.Errors = %d, want 1", report.Errors)
	}
	if len(report.ErrorIDs) != 1 || report.ErrorIDs[0] != "Q2" {
		t.Errorf("report.ErrorIDs = %v, want [Q2]", report.ErrorIDs)
	}
	if len(report.PerQuery) != 3 {
		t.Fatalf("want all 3 queries present in PerQuery, got %d: %+v", len(report.PerQuery), report.PerQuery)
	}

	byID := make(map[string]eval.QueryResult, len(report.PerQuery))
	for _, qr := range report.PerQuery {
		byID[qr.ID] = qr
	}

	failed := byID["Q2"]
	if failed.Err == "" || !strings.Contains(failed.Err, "boom") {
		t.Errorf("Q2.Err = %q, want it to contain the underlying error", failed.Err)
	}
	if failed.MRR != 0 || failed.PrecisionK != 0 || failed.HitAt1 != 0 || failed.RecallAtK != 0 || failed.RecallAt10 != 0 || failed.NDCGAt10 != 0 {
		t.Errorf("Q2 metrics should all be zero on error, got %+v", failed)
	}
	if failed.TopHit != "" || len(failed.Top) != 0 {
		t.Errorf("Q2 should have no results, got TopHit=%q Top=%+v", failed.TopHit, failed.Top)
	}

	for _, id := range []string{"Q1", "Q3"} {
		qr := byID[id]
		if qr.Err != "" {
			t.Errorf("%s.Err = %q, want empty (this query succeeded)", id, qr.Err)
		}
		if qr.MRR != 1 {
			t.Errorf("%s.MRR = %v, want 1 (exact match at rank 1)", id, qr.MRR)
		}
	}
}

// TestRunWithSearchFn_ExpectFailQueryError_GoesToKnownGaps verifies an
// errored ExpectFail query still routes to KnownGaps (not PerQuery), same as
// a successful expect_fail query would, while still counting toward
// Report.Errors/ErrorIDs. A second, successful query keeps the run from
// hitting the "every query failed" abort path so KnownGaps is actually
// observable.
func TestRunWithSearchFn_ExpectFailQueryError_GoesToKnownGaps(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("timeout")

	search := func(_ context.Context, params store.SearchParams) ([]store.SearchResult, eval.SearchMeta, error) {
		if params.Q == "gap-query" {
			return nil, eval.SearchMeta{}, boom
		}
		return []store.SearchResult{{Observation: store.Observation{Title: "good doc"}}}, eval.SearchMeta{}, nil
	}

	queries := []eval.GoldenQuery{
		{ID: "GAP1", Query: "gap-query", Expected: []string{"x"}, ExpectFail: true},
		{ID: "Q1", Query: "good-query", Expected: []string{"good doc"}},
	}

	report, err := eval.RunWithSearchFn(ctx, search, queries, "proj", 5)
	if err != nil {
		t.Fatalf("RunWithSearchFn: %v", err)
	}
	if report.Errors != 1 {
		t.Errorf("report.Errors = %d, want 1", report.Errors)
	}
	if len(report.ErrorIDs) != 1 || report.ErrorIDs[0] != "GAP1" {
		t.Errorf("report.ErrorIDs = %v, want [GAP1]", report.ErrorIDs)
	}
	if len(report.KnownGaps) != 1 {
		t.Fatalf("want the errored expect_fail query in KnownGaps, got %d entries", len(report.KnownGaps))
	}
	if report.KnownGaps[0].Err == "" || !strings.Contains(report.KnownGaps[0].Err, "timeout") {
		t.Errorf("KnownGaps[0].Err = %q, want it to contain the underlying error", report.KnownGaps[0].Err)
	}
	for _, qr := range report.PerQuery {
		if qr.ID == "GAP1" {
			t.Error("errored expect_fail query must not appear in PerQuery")
		}
	}
}

// TestRunWithSearchFn_AllQueriesFail_ReturnsError verifies the "every query
// failed" abort condition.
func TestRunWithSearchFn_AllQueriesFail_ReturnsError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("connection refused")

	search := func(_ context.Context, _ store.SearchParams) ([]store.SearchResult, eval.SearchMeta, error) {
		return nil, eval.SearchMeta{}, boom
	}

	queries := []eval.GoldenQuery{
		{ID: "Q1", Query: "a", Expected: []string{"x"}},
		{ID: "Q2", Query: "b", Expected: []string{"y"}},
	}

	_, err := eval.RunWithSearchFn(ctx, search, queries, "proj", 5)
	if err == nil {
		t.Fatal("expected an error when every query fails")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("error %q should mention the failure count (2)", err.Error())
	}
}

// TestRunWithSearchFn_PartialFailure_NotAllFailed_NoError is the converse of
// the "all failed" case: as long as at least one query succeeds, no error is
// returned even with several failures.
func TestRunWithSearchFn_PartialFailure_NotAllFailed_NoError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	search := func(_ context.Context, params store.SearchParams) ([]store.SearchResult, eval.SearchMeta, error) {
		if params.Q == "ok" {
			return nil, eval.SearchMeta{}, nil
		}
		return nil, eval.SearchMeta{}, boom
	}

	queries := []eval.GoldenQuery{
		{ID: "Q1", Query: "fail1", Expected: []string{"x"}},
		{ID: "Q2", Query: "fail2", Expected: []string{"x"}},
		{ID: "Q3", Query: "ok", Expected: []string{"x"}},
	}

	report, err := eval.RunWithSearchFn(ctx, search, queries, "proj", 5)
	if err != nil {
		t.Fatalf("RunWithSearchFn: 1 of 3 succeeded, should not error: %v", err)
	}
	if report.Errors != 2 {
		t.Errorf("report.Errors = %d, want 2", report.Errors)
	}
}

// TestRunWithSearchFn_EmptyGoldenSet_ReturnsError verifies the documented
// "or the golden set is empty" abort condition.
func TestRunWithSearchFn_EmptyGoldenSet_ReturnsError(t *testing.T) {
	ctx := context.Background()
	search := func(_ context.Context, _ store.SearchParams) ([]store.SearchResult, eval.SearchMeta, error) {
		t.Fatal("search should never be called for an empty golden set")
		return nil, eval.SearchMeta{}, nil
	}

	_, err := eval.RunWithSearchFn(ctx, search, nil, "proj", 5)
	if err == nil {
		t.Fatal("expected an error for an empty golden set")
	}
}
