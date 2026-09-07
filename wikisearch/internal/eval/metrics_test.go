package eval_test

import (
	"math"
	"testing"

	"wikisearch/internal/eval"
)

func TestPrecisionAtK(t *testing.T) {
	results := []string{"A", "B", "C", "D", "E"}
	relevant := map[string]bool{"A": true, "C": true, "E": true}
	// 3 of 5 relevant
	p := eval.PrecisionAtK(results, relevant, 5)
	if math.Abs(p-0.6) > 1e-9 {
		t.Errorf("P@5 = %f, want 0.6", p)
	}
}

func TestPrecisionAtKTruncates(t *testing.T) {
	results := []string{"A", "B", "C", "D", "E"}
	relevant := map[string]bool{"A": true, "E": true}
	// Only look at top 3 — E not counted
	p := eval.PrecisionAtK(results, relevant, 3)
	if math.Abs(p-1.0/3.0) > 1e-9 {
		t.Errorf("P@3 = %f, want 0.333", p)
	}
}

func TestMRRFirstResult(t *testing.T) {
	results := []string{"A", "B", "C"}
	relevant := map[string]bool{"A": true}
	mrr := eval.MRR(results, relevant)
	if math.Abs(mrr-1.0) > 1e-9 {
		t.Errorf("MRR = %f, want 1.0", mrr)
	}
}

func TestMRRThirdResult(t *testing.T) {
	results := []string{"X", "Y", "A", "B"}
	relevant := map[string]bool{"A": true}
	// rank 3 → 1/3
	mrr := eval.MRR(results, relevant)
	if math.Abs(mrr-1.0/3.0) > 1e-9 {
		t.Errorf("MRR = %f, want 0.333", mrr)
	}
}

func TestMRRNoRelevant(t *testing.T) {
	results := []string{"X", "Y", "Z"}
	relevant := map[string]bool{"A": true}
	if eval.MRR(results, relevant) != 0 {
		t.Error("MRR should be 0 when no relevant result found")
	}
}

func TestNDCGPerfect(t *testing.T) {
	results := []string{"A", "B", "C"}
	relevant := map[string]bool{"A": true, "B": true, "C": true}
	ndcg := eval.NDCGAtK(results, relevant, 3)
	if math.Abs(ndcg-1.0) > 1e-9 {
		t.Errorf("NDCG@3 perfect = %f, want 1.0", ndcg)
	}
}

func TestNDCGWorse(t *testing.T) {
	// A and B are relevant; X is not.
	// Perfect puts relevant first; worse buries them behind X.
	// Binary relevance: swapping two relevant items gives same DCG,
	// but pushing them down behind irrelevant items lowers it.
	perfect := []string{"A", "B", "X"}
	worse := []string{"X", "A", "B"}
	relevant := map[string]bool{"A": true, "B": true}
	p := eval.NDCGAtK(perfect, relevant, 3)
	w := eval.NDCGAtK(worse, relevant, 3)
	if w >= p {
		t.Errorf("worse ordering should have lower NDCG: perfect=%f worse=%f", p, w)
	}
}
