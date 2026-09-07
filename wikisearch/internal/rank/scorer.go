// Package rank implements scoring, ranking, and result selection.
package rank

import "wikisearch/internal/index"

// Scorer assigns a relevance score to one (term, document) pair.
// Call for each query term and sum to get the full document score.
type Scorer interface {
	Score(entry index.PostingEntry, docFreq uint32, docLen uint32) float64
}

// Result is one ranked search result.
type Result struct {
	DocID uint32
	Score float64
}

// indexStats is the subset of index.Index needed for scoring.
// Defined here so rank never imports concrete index types.
type indexStats interface {
	NumDocs() uint32
	AvgDocLen() float64
	DocLen(id uint32) uint32
}
