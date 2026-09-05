package analysis_test

import (
	"testing"

	"wikisearch/internal/analysis"
)

func TestTokenizeBasic(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"Hello, world!", []string{"Hello", "world"}},
		{"C++ programming", []string{"C", "programming"}},
		{"don't stop", []string{"don", "t", "stop"}},
		{"  spaces  ", []string{"spaces"}},
		{"123 go", []string{"123", "go"}},
		{"", nil},
	}
	for _, c := range cases {
		got := analysis.Tokenize(c.input)
		if len(got) != len(c.want) {
			t.Errorf("Tokenize(%q): got %d tokens %v, want %d %v",
				c.input, len(got), got, len(c.want), c.want)
			continue
		}
		for i := range got {
			if got[i].Term != c.want[i] {
				t.Errorf("Tokenize(%q)[%d]: got %q, want %q", c.input, i, got[i].Term, c.want[i])
			}
		}
	}
}

func TestTokenizePositions(t *testing.T) {
	tokens := analysis.Tokenize("the cat sat")
	for i, tok := range tokens {
		if tok.Position != i {
			t.Errorf("position[%d] = %d, want %d", i, tok.Position, i)
		}
	}
}

func TestTokenizeUnicode(t *testing.T) {
	// Non-ASCII letters must be kept, not split.
	tokens := analysis.Tokenize("café résumé")
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(tokens), tokens)
	}
	if tokens[0].Term != "café" {
		t.Errorf("tokens[0] = %q, want café", tokens[0].Term)
	}
}
