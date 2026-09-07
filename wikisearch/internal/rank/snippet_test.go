package rank_test

import (
	"strings"
	"testing"

	"wikisearch/internal/rank"
)

func TestSnippetContainsQueryTerm(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog. " +
		"Photosynthesis is a process used by plants to convert light into energy. " +
		"The dog barked loudly."
	snip := rank.Snippet(text, []string{"photosynthesi"}, 120)
	lower := strings.ToLower(snip)
	if !strings.Contains(lower, "photosynthes") {
		t.Errorf("snippet missing query term: %q", snip)
	}
}

func TestSnippetNonEmpty(t *testing.T) {
	snip := rank.Snippet("hello world foo bar", []string{"foo"}, 50)
	if snip == "" {
		t.Error("snippet should not be empty")
	}
}

func TestSnippetEmptyText(t *testing.T) {
	snip := rank.Snippet("", []string{"foo"}, 50)
	if snip != "" {
		t.Errorf("empty text should give empty snippet, got %q", snip)
	}
}
