package analysis_test

import (
	"testing"

	"wikisearch/internal/analysis"
)

func TestAnalyzerPipeline(t *testing.T) {
	a := analysis.NewAnalyzer()
	// "The Running Dogs": "the" is stopword, "running"→"run", "dogs"→"dog"
	tokens := a.Analyze("The Running Dogs")
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(tokens), tokens)
	}
	if tokens[0].Term != "run" {
		t.Errorf("tokens[0] = %q, want run", tokens[0].Term)
	}
	if tokens[1].Term != "dog" {
		t.Errorf("tokens[1] = %q, want dog", tokens[1].Term)
	}
	// "the" dropped at position 0 — "running" keeps position 1, "dogs" keeps 2
	if tokens[0].Position != 1 || tokens[1].Position != 2 {
		t.Errorf("positions = %d, %d; want 1, 2", tokens[0].Position, tokens[1].Position)
	}
}

func TestAnalyzerIdempotent(t *testing.T) {
	// Running the same text twice must produce identical output.
	a := analysis.NewAnalyzer()
	first := a.Analyze("photosynthesis plant")
	second := a.Analyze("photosynthesis plant")
	if len(first) != len(second) {
		t.Fatalf("got different lengths: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("token[%d] differs: %v vs %v", i, first[i], second[i])
		}
	}
}
