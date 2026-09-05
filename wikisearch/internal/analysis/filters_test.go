package analysis_test

import (
	"testing"

	"wikisearch/internal/analysis"
)

func TestLowercaseFilter(t *testing.T) {
	in := []analysis.Token{{Term: "Running", Position: 0}, {Term: "DOGS", Position: 1}}
	out := analysis.LowercaseFilter(in)
	if out[0].Term != "running" || out[1].Term != "dogs" {
		t.Errorf("LowercaseFilter: got %v", out)
	}
}

func TestStopwordFilterDrops(t *testing.T) {
	in := []analysis.Token{
		{Term: "the", Position: 0},
		{Term: "running", Position: 1},
		{Term: "dogs", Position: 2},
	}
	out := analysis.StopwordFilter(in)
	if len(out) != 2 {
		t.Fatalf("got %d tokens, want 2", len(out))
	}
}

func TestStopwordFilterPreservesGap(t *testing.T) {
	// Dropping "the" at position 0 must leave running at position 1 — not renumber to 0.
	in := []analysis.Token{
		{Term: "the", Position: 0},
		{Term: "running", Position: 1},
		{Term: "dogs", Position: 2},
	}
	out := analysis.StopwordFilter(in)
	if out[0].Position != 1 {
		t.Errorf("position gap lost: running at position %d, want 1", out[0].Position)
	}
	if out[1].Position != 2 {
		t.Errorf("position gap lost: dogs at position %d, want 2", out[1].Position)
	}
}

func TestStemFilter(t *testing.T) {
	cases := []struct{ in, want string }{
		{"running", "run"},
		{"dogs", "dog"},
		{"photosynthesis", "photosynthesi"},
	}
	for _, c := range cases {
		in := []analysis.Token{{Term: c.in, Position: 0}}
		out := analysis.StemFilter(in)
		if out[0].Term != c.want {
			t.Errorf("stem(%q) = %q, want %q", c.in, out[0].Term, c.want)
		}
	}
}
