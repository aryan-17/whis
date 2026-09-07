package postings_test

import (
	"testing"

	"wikisearch/internal/index"
	"wikisearch/internal/postings"
)

// pl builds a PostingList from a list of docIDs (termFreq=1 each).
func pl(docIDs ...uint32) index.PostingList {
	entries := make([]index.PostingEntry, len(docIDs))
	for i, id := range docIDs {
		entries[i] = index.PostingEntry{DocID: id, TermFreq: 1}
	}
	return index.PostingList{DocFreq: uint32(len(entries)), Entries: entries}
}

// ids extracts docIDs from a PostingList for easy comparison.
func ids(p index.PostingList) []uint32 {
	out := make([]uint32, len(p.Entries))
	for i, e := range p.Entries {
		out[i] = e.DocID
	}
	return out
}

func eqU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestIntersect(t *testing.T) {
	cases := []struct {
		a, b index.PostingList
		want []uint32
	}{
		{pl(1, 3, 5, 7), pl(2, 3, 5, 8), []uint32{3, 5}},
		{pl(1, 2, 3), pl(1, 2, 3), []uint32{1, 2, 3}},
		{pl(1, 2), pl(3, 4), nil},
		{pl(), pl(1, 2), nil},
	}
	for _, c := range cases {
		got := ids(postings.Intersect(c.a, c.b))
		if !eqU32(got, c.want) {
			t.Errorf("Intersect(%v, %v) = %v, want %v",
				ids(c.a), ids(c.b), got, c.want)
		}
	}
}

func TestUnion(t *testing.T) {
	cases := []struct {
		a, b index.PostingList
		want []uint32
	}{
		{pl(1, 3), pl(2, 3, 4), []uint32{1, 2, 3, 4}},
		{pl(1, 2, 3), pl(1, 2, 3), []uint32{1, 2, 3}},
		{pl(), pl(1, 2), []uint32{1, 2}},
	}
	for _, c := range cases {
		got := ids(postings.Union(c.a, c.b))
		if !eqU32(got, c.want) {
			t.Errorf("Union(%v, %v) = %v, want %v",
				ids(c.a), ids(c.b), got, c.want)
		}
	}
}

func TestDifference(t *testing.T) {
	cases := []struct {
		a, b index.PostingList
		want []uint32
	}{
		{pl(1, 2, 3, 4), pl(2, 4), []uint32{1, 3}},
		{pl(1, 2, 3), pl(), []uint32{1, 2, 3}},
		{pl(1, 2), pl(1, 2), nil},
	}
	for _, c := range cases {
		got := ids(postings.Difference(c.a, c.b))
		if !eqU32(got, c.want) {
			t.Errorf("Difference(%v, %v) = %v, want %v",
				ids(c.a), ids(c.b), got, c.want)
		}
	}
}
