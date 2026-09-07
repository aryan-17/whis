package postings_test

import (
	"testing"

	"wikisearch/internal/postings"
)

func makeLargeList(n int, stride uint32) postings.List {
	entries := make([]postings.Entry, n)
	for i := range entries {
		entries[i] = postings.Entry{DocID: uint32(i) * stride, TermFreq: 1}
	}
	return postings.List{DocFreq: uint32(n), Entries: entries}
}

// BenchmarkIntersect measures two-pointer intersection of 10k-entry lists.
func BenchmarkIntersect(b *testing.B) {
	a := makeLargeList(10000, 1)  // dense: 0,1,2,...
	bb := makeLargeList(10000, 2) // sparse: 0,2,4,...
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		postings.Intersect(a, bb)
	}
}

// BenchmarkIntersectGallop measures galloping intersection of sparse lists.
func BenchmarkIntersectGallop(b *testing.B) {
	a := makeLargeList(1000, 1)    // dense
	bb := makeLargeList(10000, 10) // very sparse
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		postings.IntersectGallop(a, bb)
	}
}
