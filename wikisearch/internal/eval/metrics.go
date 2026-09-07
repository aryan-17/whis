// Package eval implements information retrieval evaluation metrics.
package eval

import "math"

// PrecisionAtK returns the fraction of top-k results that are relevant.
func PrecisionAtK(results []string, relevant map[string]bool, k int) float64 {
	if k <= 0 {
		return 0
	}
	var hits float64
	for i := 0; i < k && i < len(results); i++ {
		if relevant[results[i]] {
			hits++
		}
	}
	return hits / float64(k)
}

// MRR returns the reciprocal rank of the first relevant result.
// Returns 0 if no relevant result appears in results.
func MRR(results []string, relevant map[string]bool) float64 {
	for i, r := range results {
		if relevant[r] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// NDCGAtK returns Normalized Discounted Cumulative Gain at k.
// Assumes binary relevance (0 or 1).
// A perfect ranking scores 1.0; random ordering scores near 0.
func NDCGAtK(results []string, relevant map[string]bool, k int) float64 {
	dcg := dcgAtK(results, relevant, k)
	// Ideal DCG: put all relevant results at the top.
	ideal := make([]string, 0, len(relevant))
	for r := range relevant {
		ideal = append(ideal, r)
	}
	idcg := dcgAtK(ideal, relevant, k)
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// dcgAtK computes Discounted Cumulative Gain at k with binary relevance.
func dcgAtK(results []string, relevant map[string]bool, k int) float64 {
	var dcg float64
	for i := 0; i < k && i < len(results); i++ {
		if relevant[results[i]] {
			// log2(rank+1) where rank is 1-based
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}
	return dcg
}
