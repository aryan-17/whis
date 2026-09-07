// Package index defines the Index interface and its implementations.
// All implementations (MemoryIndex, segmentIndex) satisfy Index,
// so cmd/search never changes when swapping between them.
package index

import (
	"wikisearch/internal/corpus"
	"wikisearch/internal/postings"
)

// Index is the interface all index implementations satisfy.
type Index interface {
	// Lookup returns the posting list for term, false if not indexed.
	Lookup(term string) (postings.List, bool)
	// NumDocs returns total number of indexed documents.
	NumDocs() uint32
	// AvgDocLen returns mean document length in tokens (needed for BM25).
	AvgDocLen() float64
	// DocLen returns token count for document id.
	DocLen(id uint32) uint32
	// Doc returns document metadata for id.
	Doc(id uint32) (corpus.Document, error)
}
