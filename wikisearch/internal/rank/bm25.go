package rank

import (
	"math"

	"wikisearch/internal/index"
)

// BM25Scorer implements Okapi BM25.
//
//	idf(t)     = log(1 + (N - df + 0.5) / (df + 0.5))
//	score(t,d) = idf(t) × f(t,d)×(k1+1) / (f(t,d) + k1×(1 - b + b×|d|/avgdl))
//
// k1 controls TF saturation: higher = TF keeps mattering at high frequencies.
// b  controls length normalisation: 0 = ignore length, 1 = full normalisation.
// Defaults: k1=1.2, b=0.75 — standard for most corpora.
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
// Sum across all query terms to get the full document score.
func (s *BM25Scorer) Score(entry index.PostingEntry, docFreq uint32, docLen uint32) float64 {
	N := float64(s.idx.NumDocs())
	df := float64(docFreq)
	tf := float64(entry.TermFreq)
	dl := float64(docLen)
	avgdl := s.idx.AvgDocLen()

	// Smoothed IDF — avoids log(0) when df approaches N.
	idf := math.Log(1 + (N-df+0.5)/(df+0.5))

	// TF with saturation and length normalisation.
	norm := tf * (s.k1 + 1) / (tf + s.k1*(1-s.b+s.b*dl/avgdl))

	return idf * norm
}
