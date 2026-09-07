package index

import (
	"fmt"
	"sort"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/postings"
)

// MemoryIndex holds the entire inverted index in RAM.
// Call Add for each document in ID order, then Finalize before querying.
type MemoryIndex struct {
	postings   map[string][]postings.Entry
	docs       []corpus.Document
	docLengths []uint32
	totalLen   uint64
}

// NewMemoryIndex returns an empty MemoryIndex ready for building.
func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{postings: make(map[string][]postings.Entry)}
}

// Add analyzes doc and records its postings.
// Documents must be added in ascending ID order.
func (m *MemoryIndex) Add(doc corpus.Document, a *analysis.Analyzer) {
	tokens := a.Analyze(doc.Title + " " + doc.Text)
	doc.Length = uint32(len(tokens))

	// Collect positions per term — needed for phrase queries (Sprint 6).
	type termData struct {
		positions []uint32
	}
	byTerm := make(map[string]*termData, len(tokens))
	for _, tok := range tokens {
		td := byTerm[tok.Term]
		if td == nil {
			td = &termData{}
			byTerm[tok.Term] = td
		}
		td.positions = append(td.positions, uint32(tok.Position))
	}
	for term, td := range byTerm {
		m.postings[term] = append(m.postings[term], postings.Entry{
			DocID:     doc.ID,
			TermFreq:  uint32(len(td.positions)),
			Positions: td.positions,
		})
	}

	m.docs = append(m.docs, doc)
	m.docLengths = append(m.docLengths, doc.Length)
	m.totalLen += uint64(doc.Length)
}

// Finalize sorts all posting lists by DocID. Call after all Add calls.
func (m *MemoryIndex) Finalize() {
	for term, entries := range m.postings {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].DocID < entries[j].DocID
		})
		m.postings[term] = entries
	}
}

func (m *MemoryIndex) Lookup(term string) (postings.List, bool) {
	entries, ok := m.postings[term]
	if !ok {
		return postings.List{}, false
	}
	return postings.List{Term: term, DocFreq: uint32(len(entries)), Entries: entries}, true
}

func (m *MemoryIndex) NumDocs() uint32         { return uint32(len(m.docs)) }
func (m *MemoryIndex) DocLen(id uint32) uint32 { return m.docLengths[id] }
func (m *MemoryIndex) AvgDocLen() float64 {
	if len(m.docs) == 0 {
		return 0
	}
	return float64(m.totalLen) / float64(len(m.docs))
}
func (m *MemoryIndex) Doc(id uint32) (corpus.Document, error) {
	if int(id) >= len(m.docs) {
		return corpus.Document{}, fmt.Errorf("doc %d out of range (have %d)", id, len(m.docs))
	}
	return m.docs[id], nil
}
