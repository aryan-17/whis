package rank

import (
	"math"

	"wikisearch/internal/postings"
)

// TFIDFScorer implements classic TF-IDF.
//
//	tf(t,d)  = 1 + log(freq(t,d))
//	idf(t)   = log(N / df(t))
//	score    = tf × idf
type TFIDFScorer struct{ idx indexStats }

// NewTFIDF returns a TFIDFScorer backed by idx.
func NewTFIDF(idx indexStats) *TFIDFScorer { return &TFIDFScorer{idx: idx} }

// Score returns the TF-IDF contribution of one term in one document.
func (s *TFIDFScorer) Score(entry postings.Entry, docFreq uint32, _ uint32) float64 {
	if docFreq == 0 || entry.TermFreq == 0 {
		return 0
	}
	tf := 1 + math.Log(float64(entry.TermFreq))
	idf := math.Log(float64(s.idx.NumDocs()) / float64(docFreq))
	return tf * idf
}
