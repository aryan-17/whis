package query_test

import (
	"testing"

	"wikisearch/internal/query"
)

func TestParseSimpleAnd(t *testing.T) {
	node, err := query.Parse("foo bar")
	if err != nil {
		t.Fatal(err)
	}
	and, ok := node.(*query.AndNode)
	if !ok {
		t.Fatalf("got %T, want *AndNode", node)
	}
	if len(and.Children) != 2 {
		t.Fatalf("got %d children, want 2", len(and.Children))
	}
}

func TestParseSingleTerm(t *testing.T) {
	node, err := query.Parse("foo")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := node.(*query.TermNode); !ok {
		t.Fatalf("got %T, want *TermNode", node)
	}
}

func TestParsePhrase(t *testing.T) {
	node, err := query.Parse(`"climate change"`)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := node.(*query.PhraseNode)
	if !ok {
		t.Fatalf("got %T, want *PhraseNode", node)
	}
	if len(p.Terms) != 2 {
		t.Fatalf("phrase has %d terms, want 2", len(p.Terms))
	}
}

func TestParseOr(t *testing.T) {
	node, err := query.Parse("foo OR bar")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := node.(*query.OrNode); !ok {
		t.Fatalf("got %T, want *OrNode", node)
	}
}

func TestParseNot(t *testing.T) {
	node, err := query.Parse("foo NOT bar")
	if err != nil {
		t.Fatal(err)
	}
	and, ok := node.(*query.AndNode)
	if !ok {
		t.Fatalf("got %T, want *AndNode", node)
	}
	if _, isNot := and.Children[1].(*query.NotNode); !isNot {
		t.Fatalf("second child is %T, want *NotNode", and.Children[1])
	}
}

func TestParseField(t *testing.T) {
	node, err := query.Parse("title:photosynthesis")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := node.(*query.FieldNode)
	if !ok {
		t.Fatalf("got %T, want *FieldNode", node)
	}
	if f.Field != "title" {
		t.Errorf("field = %q, want title", f.Field)
	}
}

func TestParseParens(t *testing.T) {
	node, err := query.Parse("(foo OR bar) baz")
	if err != nil {
		t.Fatal(err)
	}
	and, ok := node.(*query.AndNode)
	if !ok {
		t.Fatalf("got %T, want *AndNode", node)
	}
	if _, ok := and.Children[0].(*query.OrNode); !ok {
		t.Fatalf("first child is %T, want *OrNode", and.Children[0])
	}
}
