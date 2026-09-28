package eval

import (
	"math"
	"sort"
	"time"
)

// PrecisionAtK returns the fraction of the top-k retrieved results that appear
// in expected. k is treated as the denominator regardless of how many results
// are actually returned (so fewer than k results lowers precision).
func PrecisionAtK(expected, got []string, k int) float64 {
	if k <= 0 || len(expected) == 0 {
		return 0
	}

	want := make(map[string]bool, len(expected))
	for _, t := range expected {
		want[t] = true
	}

	hits := 0
	for i, t := range got {
		if i >= k {
			break
		}
		if want[t] {
			hits++
		}
	}

	return float64(hits) / float64(k)
}

// MRR returns the reciprocal rank of the first retrieved result whose title
// appears in expected. If no expected title is found in got, MRR is 0.
// Rank is 1-based.
func MRR(expected, got []string) float64 {
	want := make(map[string]bool, len(expected))
	for _, t := range expected {
		want[t] = true
	}

	for i, t := range got {
		if want[t] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// HitAtK returns 1.0 if any expected title appears within the top-k of got,
// else 0.0. Unlike PrecisionAtK, this is binary (did we get at least one
// hit), which is the standard "Hit Rate@k" metric — most commonly used at
// k=1 to ask "was the very first result correct?".
func HitAtK(expected, got []string, k int) float64 {
	if k <= 0 || len(expected) == 0 {
		return 0
	}

	want := make(map[string]bool, len(expected))
	for _, t := range expected {
		want[t] = true
	}

	for i, t := range got {
		if i >= k {
			break
		}
		if want[t] {
			return 1
		}
	}
	return 0
}

// RecallAtK returns the fraction of DISTINCT expected titles found within the
// top-k of got. Unlike PrecisionAtK (whose denominator is always k), the
// denominator here is len(expected), so a query with a single expected title
// reaches 1.0 as soon as that title appears anywhere in the top-k, instead of
// being capped at 1/k.
func RecallAtK(expected, got []string, k int) float64 {
	if len(expected) == 0 {
		return 0
	}

	want := make(map[string]bool, len(expected))
	for _, t := range expected {
		want[t] = true
	}

	found := make(map[string]bool, len(expected))
	for i, t := range got {
		if i >= k {
			break
		}
		if want[t] {
			found[t] = true
		}
	}

	return float64(len(found)) / float64(len(expected))
}

// NDCGAtK returns the Normalized Discounted Cumulative Gain of got against
// expected, cut off at rank k (1-based).
//
// Relevance choice: BINARY (1 if a title is in expected, 0 otherwise), not
// graded by the title's position within Expected. Expected's doc comment
// describes it as "titles in ideal rank order", which would support treating
// the first expected title as more relevant than the second — but the golden
// fixtures in this repo (internal/eval/testdata/golden.yaml) overwhelmingly
// list either exactly one expected title, or several EQUALLY valid results
// (e.g. two docs that both satisfy a multi-term query, in no meaningful
// preference order — see Q05/Q06). Binary relevance keeps the metric
// unambiguous for that common case; graded relevance would silently penalize
// a correct query that happens to rank two equally-good docs "out of order".
//
//	DCG@k  = sum_{i=1..k} rel_i / log2(i+1)          (i is 1-based rank)
//	IDCG@k = DCG of the ideal ranking: min(len(expected), k) relevant docs,
//	         all ranked first.
//	NDCG@k = DCG@k / IDCG@k, or 0 when IDCG@k is 0 (no expected titles, or
//	         k<=0).
func NDCGAtK(expected, got []string, k int) float64 {
	if k <= 0 || len(expected) == 0 {
		return 0
	}

	want := make(map[string]bool, len(expected))
	for _, t := range expected {
		want[t] = true
	}

	gain := func(rank int) float64 { return 1.0 / math.Log2(float64(rank+1)) } // rank is 1-based

	dcg := 0.0
	for i, t := range got {
		if i >= k {
			break
		}
		if want[t] {
			dcg += gain(i + 1)
		}
	}

	idealHits := len(expected)
	if idealHits > k {
		idealHits = k
	}
	idcg := 0.0
	for rank := 1; rank <= idealHits; rank++ {
		idcg += gain(rank)
	}
	if idcg == 0 {
		return 0
	}

	return dcg / idcg
}

// AggregateLatency reduces per-query latencies to p50, p95, and mean.
// Percentiles use the nearest-rank method (index = ceil(p*n)-1) over a
// stable-sorted COPY of latencies — the input slice is never mutated. An
// empty input returns the zero LatencyStats.
func AggregateLatency(latencies []time.Duration) LatencyStats {
	n := len(latencies)
	if n == 0 {
		return LatencyStats{}
	}

	sorted := make([]time.Duration, n)
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	percentile := func(p float64) time.Duration {
		idx := int(math.Ceil(p*float64(n))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return sorted[idx]
	}

	var sum time.Duration
	for _, d := range latencies {
		sum += d
	}

	return LatencyStats{
		P50:  percentile(0.50),
		P95:  percentile(0.95),
		Mean: sum / time.Duration(n),
	}
}
