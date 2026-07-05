package eval

import (
	"math"
	"testing"
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
