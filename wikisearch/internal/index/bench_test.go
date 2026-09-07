package index_test

import (
	"testing"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

// BenchmarkBuild measures time to index 5000 documents.
func BenchmarkBuild(b *testing.B) {
	a := analysis.NewAnalyzer()
	docs := make([]corpus.Document, 5000)
	for i := range docs {
		docs[i] = corpus.Document{
			ID:   uint32(i),
			Text: "photosynthesis plant process light energy chlorophyll leaf green",
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := index.NewMemoryIndex()
		for _, d := range docs {
			idx.Add(d, a)
		}
		idx.Finalize()
	}
}

// BenchmarkLookup measures single-term lookup on a 10k-doc index.
func BenchmarkLookup(b *testing.B) {
	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()
	for i := 0; i < 10000; i++ {
		idx.Add(corpus.Document{
			ID:   uint32(i),
			Text: "photosynthesis plant process light energy",
		}, a)
	}
	idx.Finalize()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Lookup("photosynthesi")
	}
}
