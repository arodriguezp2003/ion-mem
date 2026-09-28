package eval

import (
	"math"
	"testing"
	"time"
)

// TestPrecisionAtK verifies the precision@k metric with hand-computed cases.
func TestPrecisionAtK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []string
		got      []string
		k        int
		want     float64
	}{
		{
			name:     "perfect match k=5",
			expected: []string{"A", "B", "C"},
			got:      []string{"A", "B", "C", "D", "E"},
			k:        5,
			want:     3.0 / 5.0, // 3 hits in top-5
		},
		{
			name:     "no match",
			expected: []string{"A"},
			got:      []string{"X", "Y", "Z"},
			k:        3,
			want:     0.0,
		},
		{
			name:     "single exact hit at position 1",
			expected: []string{"A"},
			got:      []string{"A", "B"},
			k:        3,
			want:     1.0 / 3.0,
		},
		{
			name:     "k larger than results",
			expected: []string{"A", "B"},
			got:      []string{"A", "B"},
			k:        10,
			want:     2.0 / 10.0,
		},
		{
			name:     "empty results",
			expected: []string{"A"},
			got:      []string{},
			k:        5,
			want:     0.0,
		},
		{
			name:     "multiple expected, partial hit",
			expected: []string{"A", "B", "C"},
			got:      []string{"X", "A", "Y", "B", "Z"},
			k:        5,
			want:     2.0 / 5.0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := PrecisionAtK(tt.expected, tt.got, tt.k)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("PrecisionAtK(%v, %v, %d) = %f, want %f",
					tt.expected, tt.got, tt.k, got, tt.want)
			}
		})
	}
}

// TestMRR verifies the MRR metric with hand-computed cases.
func TestMRR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []string
		got      []string
		want     float64
	}{
		{
			name:     "first result is expected",
			expected: []string{"A"},
			got:      []string{"A", "B", "C"},
			want:     1.0, // rank 1 → 1/1
		},
		{
			name:     "expected at rank 2",
			expected: []string{"B"},
			got:      []string{"A", "B", "C"},
			want:     0.5, // rank 2 → 1/2
		},
		{
			name:     "expected at rank 3",
			expected: []string{"C"},
			got:      []string{"A", "B", "C"},
			want:     1.0 / 3.0,
		},
		{
			name:     "none expected found",
			expected: []string{"Z"},
			got:      []string{"A", "B", "C"},
			want:     0.0,
		},
		{
			name:     "multiple expected, first-found wins",
			expected: []string{"C", "A"},
			got:      []string{"A", "B", "C"},
			want:     1.0, // "A" is at rank 1
		},
		{
			name:     "empty results",
			expected: []string{"A"},
			got:      []string{},
			want:     0.0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MRR(tt.expected, tt.got)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("MRR(%v, %v) = %f, want %f",
					tt.expected, tt.got, got, tt.want)
			}
		})
	}
}

// TestHitAtK verifies the binary hit-rate metric.
func TestHitAtK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []string
		got      []string
		k        int
		want     float64
	}{
		{
			name:     "hit within k",
			expected: []string{"A"},
			got:      []string{"X", "A", "B"},
			k:        3,
			want:     1.0,
		},
		{
			name:     "hit exactly at k boundary",
			expected: []string{"C"},
			got:      []string{"A", "B", "C", "D"},
			k:        3,
			want:     1.0,
		},
		{
			name:     "hit beyond k is a miss",
			expected: []string{"D"},
			got:      []string{"A", "B", "C", "D"},
			k:        3,
			want:     0.0,
		},
		{
			name:     "no hit anywhere",
			expected: []string{"Z"},
			got:      []string{"A", "B", "C"},
			k:        3,
			want:     0.0,
		},
		{
			name:     "k=1 first result is a hit",
			expected: []string{"A"},
			got:      []string{"A", "B"},
			k:        1,
			want:     1.0,
		},
		{
			name:     "k=1 first result is a miss even if second matches",
			expected: []string{"B"},
			got:      []string{"A", "B"},
			k:        1,
			want:     0.0,
		},
		{
			name:     "empty results",
			expected: []string{"A"},
			got:      []string{},
			k:        5,
			want:     0.0,
		},
		{
			name:     "k<=0 is always a miss",
			expected: []string{"A"},
			got:      []string{"A"},
			k:        0,
			want:     0.0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := HitAtK(tt.expected, tt.got, tt.k)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("HitAtK(%v, %v, %d) = %f, want %f", tt.expected, tt.got, tt.k, got, tt.want)
			}
		})
	}
}

// TestRecallAtK verifies recall's len(expected) denominator (as opposed to
// PrecisionAtK's fixed-k denominator).
func TestRecallAtK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []string
		got      []string
		k        int
		want     float64
	}{
		{
			name:     "single expected found reaches 1.0 regardless of k",
			expected: []string{"A"},
			got:      []string{"X", "Y", "A", "Z", "W"},
			k:        5,
			want:     1.0,
		},
		{
			name:     "single expected found but outside k",
			expected: []string{"A"},
			got:      []string{"X", "Y", "A"},
			k:        2,
			want:     0.0,
		},
		{
			name:     "two of three expected found within k",
			expected: []string{"A", "B", "C"},
			got:      []string{"A", "X", "B"},
			k:        3,
			want:     2.0 / 3.0,
		},
		{
			name:     "no expected titles",
			expected: []string{},
			got:      []string{"A", "B"},
			k:        5,
			want:     0.0,
		},
		{
			name:     "empty results",
			expected: []string{"A"},
			got:      []string{},
			k:        5,
			want:     0.0,
		},
		{
			name:     "duplicate got entries do not inflate recall",
			expected: []string{"A", "B"},
			got:      []string{"A", "A", "A"},
			k:        3,
			want:     0.5,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RecallAtK(tt.expected, tt.got, tt.k)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("RecallAtK(%v, %v, %d) = %f, want %f", tt.expected, tt.got, tt.k, got, tt.want)
			}
		})
	}
}

// TestNDCGAtK verifies binary-relevance NDCG@k with hand-computed cases.
// Relevance choice (binary vs. graded-by-expected-position) is documented on
// NDCGAtK itself.
func TestNDCGAtK(t *testing.T) {
	t.Parallel()

	log2 := math.Log2

	tests := []struct {
		name     string
		expected []string
		got      []string
		k        int
		want     float64
	}{
		{
			name:     "single expected at rank 1 is perfect",
			expected: []string{"A"},
			got:      []string{"A", "B", "C"},
			k:        3,
			want:     1.0,
		},
		{
			name:     "single expected at rank 2: DCG/IDCG",
			expected: []string{"A"},
			got:      []string{"X", "A", "B"},
			k:        3,
			// DCG = 1/log2(3); IDCG = 1/log2(2) (ideal: relevant doc at rank 1)
			want: (1.0 / log2(3)) / (1.0 / log2(2)),
		},
		{
			name:     "no relevant doc found",
			expected: []string{"Z"},
			got:      []string{"A", "B", "C"},
			k:        3,
			want:     0.0,
		},
		{
			name:     "two relevant docs both in top k, already ideal order",
			expected: []string{"A", "B"},
			got:      []string{"A", "B", "C"},
			k:        3,
			// DCG = IDCG = 1/log2(2) + 1/log2(3) -> ratio 1.0
			want: 1.0,
		},
		{
			name:     "two relevant docs found out of ideal order still scores 1.0 (binary relevance)",
			expected: []string{"A", "B"},
			got:      []string{"B", "A", "C"},
			k:        3,
			// Binary relevance: both A and B contribute rel=1 regardless of
			// which one is "more expected", so DCG = IDCG here too.
			want: 1.0,
		},
		{
			name:     "no expected titles",
			expected: []string{},
			got:      []string{"A"},
			k:        3,
			want:     0.0,
		},
		{
			name:     "empty results",
			expected: []string{"A"},
			got:      []string{},
			k:        3,
			want:     0.0,
		},
		{
			name:     "k<=0",
			expected: []string{"A"},
			got:      []string{"A"},
			k:        0,
			want:     0.0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NDCGAtK(tt.expected, tt.got, tt.k)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("NDCGAtK(%v, %v, %d) = %f, want %f", tt.expected, tt.got, tt.k, got, tt.want)
			}
		})
	}
}

// TestAggregateLatency verifies the p50/p95/mean reduction using the
// nearest-rank percentile method over a small, hand-checkable sample.
func TestAggregateLatency(t *testing.T) {
	t.Parallel()

	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }

	t.Run("empty input returns zero value", func(t *testing.T) {
		t.Parallel()
		got := AggregateLatency(nil)
		if got != (LatencyStats{}) {
			t.Errorf("AggregateLatency(nil) = %+v, want zero value", got)
		}
	})

	t.Run("single value", func(t *testing.T) {
		t.Parallel()
		got := AggregateLatency([]time.Duration{ms(100)})
		want := LatencyStats{P50: ms(100), P95: ms(100), Mean: ms(100)}
		if got != want {
			t.Errorf("AggregateLatency = %+v, want %+v", got, want)
		}
	})

	t.Run("ten values 10..100ms: nearest-rank p50 and p95, and mean", func(t *testing.T) {
		t.Parallel()
		var latencies []time.Duration
		for i := 1; i <= 10; i++ {
			latencies = append(latencies, ms(i*10))
		}
		got := AggregateLatency(latencies)
		// Nearest-rank: idx = ceil(p*n)-1. p50: ceil(0.5*10)-1=4 -> sorted[4]=50ms.
		// p95: ceil(0.95*10)-1=9 -> sorted[9]=100ms.
		want := LatencyStats{P50: ms(50), P95: ms(100), Mean: ms(55)}
		if got != want {
			t.Errorf("AggregateLatency = %+v, want %+v", got, want)
		}
	})

	t.Run("unsorted input is not mutated and does not affect the caller's slice", func(t *testing.T) {
		t.Parallel()
		input := []time.Duration{ms(30), ms(10), ms(20)}
		inputCopy := append([]time.Duration(nil), input...)
		_ = AggregateLatency(input)
		for i := range input {
			if input[i] != inputCopy[i] {
				t.Fatalf("AggregateLatency mutated its input: got %v, want %v", input, inputCopy)
			}
		}
	})
}
