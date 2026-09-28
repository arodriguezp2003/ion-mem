package eval_test

import (
	"context"
	"testing"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/eval"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// TestRunWithSearchFn_PerQueryProjectOverride verifies that a non-empty
// GoldenQuery.Project overrides the runner's default project for that query
// only, letting a single golden set mix project-scoped queries.
func TestRunWithSearchFn_PerQueryProjectOverride(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)

	if _, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "s1", Project: "proj-a", Directory: "/x"}); err != nil {
		t.Fatalf("CreateSession proj-a: %v", err)
	}
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "s2", Project: "proj-b", Directory: "/x"}); err != nil {
		t.Fatalf("CreateSession proj-b: %v", err)
	}

	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "widget in project a", Content: "widget content a",
		Project: "proj-a", Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation proj-a: %v", err)
	}
	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s2", Type: "manual", Title: "widget in project b", Content: "widget content b",
		Project: "proj-b", Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation proj-b: %v", err)
	}

	queries := []eval.GoldenQuery{
		{
			ID:       "default-project",
			Query:    "widget",
			Expected: []string{"widget in project a"},
			// No Project override: uses the runner's default project "proj-a".
		},
		{
			ID:       "override-project",
			Query:    "widget",
			Expected: []string{"widget in project b"},
			Project:  "proj-b", // overrides the runner's default project.
		},
	}

	report, err := eval.Run(ctx, st, queries, "proj-a", 5)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.PerQuery) != 2 {
		t.Fatalf("PerQuery: got %d results, want 2: %+v", len(report.PerQuery), report.PerQuery)
	}

	byID := make(map[string]eval.QueryResult, len(report.PerQuery))
	for _, qr := range report.PerQuery {
		byID[qr.ID] = qr
	}

	if got := byID["default-project"]; got.Project != "proj-a" || got.TopHit != "widget in project a" {
		t.Errorf("default-project: Project=%q TopHit=%q, want proj-a / %q", got.Project, got.TopHit, "widget in project a")
	}
	if got := byID["override-project"]; got.Project != "proj-b" || got.TopHit != "widget in project b" {
		t.Errorf("override-project: Project=%q TopHit=%q, want proj-b / %q", got.Project, got.TopHit, "widget in project b")
	}
}

// TestRunWithSearchFn_CategoryPassthrough verifies GoldenQuery.Category is
// echoed to QueryResult.Category unchanged and has no effect on scoring.
func TestRunWithSearchFn_CategoryPassthrough(t *testing.T) {
	ctx := context.Background()
	st := mustOpenStore(t)
	proj := "cat-proj"

	if _, err := st.CreateSession(ctx, store.CreateSessionParams{ID: "s1", Project: proj, Directory: "/x"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "gadget doc", Content: "gadget content",
		Project: proj, Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	queries := []eval.GoldenQuery{
		{ID: "q1", Query: "gadget", Expected: []string{"gadget doc"}, Category: "lexical-exact"},
	}

	report, err := eval.Run(ctx, st, queries, proj, 5)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.PerQuery) != 1 {
		t.Fatalf("want 1 result, got %d", len(report.PerQuery))
	}
	if got := report.PerQuery[0].Category; got != "lexical-exact" {
		t.Errorf("Category = %q, want %q", got, "lexical-exact")
	}
}

// TestRunWithSearchFn_FetchWindowIsAtLeastTen verifies the documented fetch
// window: RunWithSearchFn always requests Limit = max(k, 10), even when the
// caller's k is smaller, so MRR/NDCG@10 can see beyond the P@k cutoff.
func TestRunWithSearchFn_FetchWindowIsAtLeastTen(t *testing.T) {
	ctx := context.Background()
	var gotLimit int

	search := func(_ context.Context, params store.SearchParams) ([]store.SearchResult, eval.SearchMeta, error) {
		gotLimit = params.Limit
		return nil, eval.SearchMeta{}, nil
	}

	queries := []eval.GoldenQuery{{ID: "q1", Query: "x", Expected: []string{"y"}}}

	if _, err := eval.RunWithSearchFn(ctx, search, queries, "proj", 3); err != nil {
		t.Fatalf("RunWithSearchFn: %v", err)
	}
	if gotLimit != 10 {
		t.Errorf("Limit = %d, want 10 (max(k=3, 10))", gotLimit)
	}

	if _, err := eval.RunWithSearchFn(ctx, search, queries, "proj", 20); err != nil {
		t.Fatalf("RunWithSearchFn: %v", err)
	}
	if gotLimit != 20 {
		t.Errorf("Limit = %d, want 20 (max(k=20, 10))", gotLimit)
	}
}

// TestRunWithSearchFn_RecordsLatencyAndHybridMeta verifies that per-query
// Latency is measured (non-zero for a search that takes measurable time) and
// that SearchMeta.Hybrid/Fuzzy pass through to QueryResult, feeding
// Report.HybridRan.
func TestRunWithSearchFn_RecordsLatencyAndHybridMeta(t *testing.T) {
	ctx := context.Background()

	// A tiny sleep makes elapsed time realistically nonzero without asserting
	// on any specific duration (see the Latency > 0 checks below) — this
	// keeps the test from depending on scheduler timing precision.
	search := func(_ context.Context, _ store.SearchParams) ([]store.SearchResult, eval.SearchMeta, error) {
		time.Sleep(time.Microsecond)
		return nil, eval.SearchMeta{Hybrid: true, Fuzzy: true}, nil
	}

	queries := []eval.GoldenQuery{
		{ID: "q1", Query: "x", Expected: []string{"y"}},
		{ID: "q2", Query: "z", Expected: []string{"w"}},
	}

	report, err := eval.RunWithSearchFn(ctx, search, queries, "proj", 5)
	if err != nil {
		t.Fatalf("RunWithSearchFn: %v", err)
	}
	if report.HybridRan != 2 {
		t.Errorf("HybridRan = %d, want 2", report.HybridRan)
	}
	for _, qr := range report.PerQuery {
		if !qr.Hybrid || !qr.Fuzzy {
			t.Errorf("query %q: Hybrid=%v Fuzzy=%v, want both true", qr.ID, qr.Hybrid, qr.Fuzzy)
		}
		if qr.Latency <= 0 {
			t.Errorf("query %q: Latency = %v, want > 0", qr.ID, qr.Latency)
		}
	}
	if report.Latency.Mean <= 0 {
		t.Errorf("Report.Latency.Mean = %v, want > 0", report.Latency.Mean)
	}
}
