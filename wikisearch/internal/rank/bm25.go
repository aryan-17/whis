package rank

import (
	"math"

	"wikisearch/internal/postings"
)

// BM25Scorer implements Okapi BM25.
//
//	idf(t)     = log(1 + (N - df + 0.5) / (df + 0.5))
//	score(t,d) = idf(t) × f(t,d)×(k1+1) / (f(t,d) + k1×(1 - b + b×|d|/avgdl))
//
// k1=1.2, b=0.75 are standard defaults.
type BM25Scorer struct {
	idx indexStats
	k1  float64
	b   float64
}

// NewBM25 returns a BM25Scorer. Use k1=1.2, b=0.75 as starting defaults.
func NewBM25(idx indexStats, k1, b float64) *BM25Scorer {
	return &BM25Scorer{idx: idx, k1: k1, b: b}
}

// Score returns the BM25 contribution of one term in one document.
func (s *BM25Scorer) Score(entry postings.Entry, docFreq uint32, docLen uint32) float64 {
	N := float64(s.idx.NumDocs())
	df := float64(docFreq)
	tf := float64(entry.TermFreq)
	dl := float64(docLen)
	avgdl := s.idx.AvgDocLen()

	idf := math.Log(1 + (N-df+0.5)/(df+0.5))
	norm := tf * (s.k1 + 1) / (tf + s.k1*(1-s.b+s.b*dl/avgdl))
	return idf * norm
}
