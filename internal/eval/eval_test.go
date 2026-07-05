package eval_test

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/eval"
	"github.com/ionix/ion-mem/internal/store"
)

// testdataPath returns the absolute path to the testdata directory.
func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	return filepath.Join(dir, "testdata", name)
}

// TestEvalGoldenSet seeds the synthetic corpus into a fresh temp store and runs
// all golden queries. It asserts aggregate quality thresholds and verifies that
// all expect_fail queries indeed fail to surface their expected result.
//
// Thresholds were measured on first calibration run and set ~14% below measured:
//
//	Measured MeanMRR:          1.0000  (all 18 non-fail queries return correct top hit)
//	Threshold MeanMRR:         0.60    (conservative — guards significant regressions)
//
//	Measured MeanPrecisionAt5: 0.2333  (limited by k=5 denominator with single expected docs)
//	Threshold MeanPrecisionAt5: 0.20   (~14% below measured)
//
// P@5 is structurally capped low because most queries have a single expected doc
// and k=5 means a perfect single hit = 1/5 = 0.20. Queries with 2 expected docs
// score 2/5 = 0.40. MRR is the primary signal for ranking quality.
//
// These guard regressions without being flaky across corpus/query edits.
func TestEvalGoldenSet(t *testing.T) {
	ctx := context.Background()

	// Load fixtures.
	corpusPath := testdataPath("corpus.yaml")
	goldenPath := testdataPath("golden.yaml")

	docs, err := eval.LoadCorpus(corpusPath)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	queries, err := eval.LoadGolden(goldenPath)
	if err != nil {
		t.Fatalf("LoadGolden: %v", err)
	}

	// Fresh store for each run.
	st, storeErr := store.Open(t.TempDir())
	if storeErr != nil {
		t.Fatalf("store.Open: %v", storeErr)
	}
	t.Cleanup(func() { st.Close() })

	// Seed corpus.
	const project = "eval-golden"
	if err := eval.SeedCorpus(ctx, st, docs, project); err != nil {
		t.Fatalf("SeedCorpus: %v", err)
	}

	// Run evaluation.
	const k = 5
	report, err := eval.Run(ctx, st, queries, project, k)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Log per-query table.
	t.Log("=== Per-query results ===")
	header := fmt.Sprintf("%-5s  %-45s  %5s  %5s  %-40s", "ID", "Query", "MRR", "P@5", "TopHit")
	t.Log(header)
	t.Log(strings.Repeat("-", len(header)))
	for _, qr := range report.PerQuery {
		t.Logf("%-5s  %-45s  %5.3f  %5.3f  %-40s",
			qr.ID,
			truncateStr(qr.Query, 45),
			qr.MRR,
			qr.PrecisionK,
			truncateStr(qr.TopHit, 40),
		)
	}
	t.Log("")
	t.Log("=== Known gaps (expect_fail=true) ===")
	for _, qr := range report.KnownGaps {
		t.Logf("%-5s  %-45s  %5.3f  %5.3f  %-40s  note: BM25 lexical gap",
			qr.ID,
			truncateStr(qr.Query, 45),
			qr.MRR,
			qr.PrecisionK,
			truncateStr(qr.TopHit, 40),
		)
	}
	t.Log("")
	t.Logf("=== Aggregate (non-fail queries) ===")
	t.Logf("MeanMRR:           %.4f", report.MeanMRR)
	t.Logf("MeanPrecisionAt5:  %.4f", report.MeanPrecisionAt5)

	// --- Regression thresholds ---
	const minMeanMRR = 0.60
	const minMeanP5 = 0.20

	if report.MeanMRR < minMeanMRR {
		t.Errorf("MeanMRR regression: got %.4f, want >= %.4f", report.MeanMRR, minMeanMRR)
	}
	if report.MeanPrecisionAt5 < minMeanP5 {
		t.Errorf("MeanPrecisionAt5 regression: got %.4f, want >= %.4f", report.MeanPrecisionAt5, minMeanP5)
	}

	// --- ExpectFail assertion ---
	// Every known-gap query must fail to surface its expected doc in the top k.
	// When embeddings land, these will flip to passing and the test will force
	// a conscious threshold update.
	goldenByID := make(map[string]eval.GoldenQuery, len(queries))
	for _, q := range queries {
		goldenByID[q.ID] = q
	}

	for _, qr := range report.KnownGaps {
		gq := goldenByID[qr.ID]
		// Build set of expected titles.
		wantSet := make(map[string]bool, len(gq.Expected))
		for _, title := range gq.Expected {
			wantSet[title] = true
		}
		// The top hit must NOT be an expected title.
		if wantSet[qr.TopHit] {
			t.Errorf("expect_fail query %q unexpectedly succeeded: top hit %q matched expected",
				qr.ID, qr.TopHit)
		}
		// MRR must be 0 (no expected title in any position within k).
		if qr.MRR > 0 {
			t.Errorf("expect_fail query %q has MRR=%.4f, want 0 (semantic gap should be a miss)",
				qr.ID, qr.MRR)
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
