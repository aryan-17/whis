// Package index defines the core index types and the Index interface.
// All index implementations (in-memory, on-disk segment) satisfy Index,
// so cmd/search never changes when implementations are swapped.
package index

import "wikisearch/internal/corpus"

// PostingEntry is one (document, term-frequency) pair in a posting list.
// Positions is nil until Sprint 6 (positional index).
// Invariant: posting lists are always sorted by DocID ascending.
type PostingEntry struct {
	DocID     uint32
	TermFreq  uint32
	Positions []uint32
}

// PostingList holds all documents containing a term.
// Entries MUST be sorted by DocID — all callers rely on this.
type PostingList struct {
	Term    string
	DocFreq uint32
	Entries []PostingEntry
}

// Index is the interface all index implementations satisfy.
// Define it here so cmd/search depends on the interface, not a concrete type.
type Index interface {
	// Lookup returns the posting list for term, false if term not in index.
	Lookup(term string) (PostingList, bool)
	// NumDocs returns total number of indexed documents.
	NumDocs() uint32
	// AvgDocLen returns mean document length in tokens (needed for BM25).
	AvgDocLen() float64
	// DocLen returns token count for document id.
	DocLen(id uint32) uint32
	// Doc returns the document metadata for id.
	Doc(id uint32) (corpus.Document, error)
}
