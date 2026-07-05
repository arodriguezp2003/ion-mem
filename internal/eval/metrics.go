package eval

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
