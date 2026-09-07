package index

import (
	"fmt"
	"sort"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
)

// MemoryIndex holds the entire inverted index in RAM.
// Call Add for each document in ID order, then Finalize before querying.
type MemoryIndex struct {
	postings   map[string][]PostingEntry // built during Add, sorted in Finalize
	docs       []corpus.Document
	docLengths []uint32
	totalLen   uint64
}

// NewMemoryIndex returns an empty MemoryIndex ready for building.
func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{postings: make(map[string][]PostingEntry)}
}

// Add analyzes doc and records its postings in the index.
// Documents must be added in ascending ID order.
func (m *MemoryIndex) Add(doc corpus.Document, a *analysis.Analyzer) {
	tokens := a.Analyze(doc.Title + " " + doc.Text)
	doc.Length = uint32(len(tokens))

	// Count term frequencies within this document.
	freq := make(map[string]uint32, len(tokens))
	for _, tok := range tokens {
		freq[tok.Term]++
	}

	for term, tf := range freq {
		m.postings[term] = append(m.postings[term], PostingEntry{
			DocID:    doc.ID,
			TermFreq: tf,
		})
	}

	m.docs = append(m.docs, doc)
	m.docLengths = append(m.docLengths, doc.Length)
	m.totalLen += uint64(doc.Length)
}

// Finalize sorts all posting lists by DocID.
// Must be called after all Add calls and before any Lookup.
func (m *MemoryIndex) Finalize() {
	for term, entries := range m.postings {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].DocID < entries[j].DocID
		})
		m.postings[term] = entries
	}
}

// Lookup returns the posting list for term.
func (m *MemoryIndex) Lookup(term string) (PostingList, bool) {
	entries, ok := m.postings[term]
	if !ok {
		return PostingList{}, false
	}
	return PostingList{
		Term:    term,
		DocFreq: uint32(len(entries)),
		Entries: entries,
	}, true
}

// NumDocs returns the total number of indexed documents.
func (m *MemoryIndex) NumDocs() uint32 { return uint32(len(m.docs)) }

// DocLen returns the token count for document id.
func (m *MemoryIndex) DocLen(id uint32) uint32 { return m.docLengths[id] }

// AvgDocLen returns the mean document length across the corpus.
func (m *MemoryIndex) AvgDocLen() float64 {
	if len(m.docs) == 0 {
		return 0
	}
	return float64(m.totalLen) / float64(len(m.docs))
}

// Doc returns the document metadata for id.
func (m *MemoryIndex) Doc(id uint32) (corpus.Document, error) {
	if int(id) >= len(m.docs) {
		return corpus.Document{}, fmt.Errorf("doc %d out of range (have %d)", id, len(m.docs))
	}
	return m.docs[id], nil
}
