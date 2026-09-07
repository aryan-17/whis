package postings_test

import (
	"testing"

	"wikisearch/internal/postings"
)

func TestIntersectGallopMatchesIntersect(t *testing.T) {
	// IntersectGallop must produce identical results to Intersect.
	cases := []struct{ a, b postings.List }{
		{pl(1, 3, 5, 7, 9), pl(2, 3, 5, 8, 9)},
		{pl(1, 2, 3), pl(1, 2, 3)},
		{pl(1, 2), pl(3, 4)},
		{pl(), pl(1, 2)},
		{pl(1, 100, 200, 300), pl(50, 100, 150, 300, 500)},
	}
	for _, c := range cases {
		want := ids(postings.Intersect(c.a, c.b))
		got := ids(postings.IntersectGallop(c.a, c.b))
		if !eqU32(got, want) {
			t.Errorf("IntersectGallop(%v, %v) = %v, want %v",
				ids(c.a), ids(c.b), got, want)
		}
	}
}
