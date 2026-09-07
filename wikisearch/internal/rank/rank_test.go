package rank_test

import (
	"math"
	"testing"

	"wikisearch/internal/index"
	"wikisearch/internal/rank"
)

// stubIndex satisfies indexStats for scoring tests without building a real index.
type stubIndex struct {
	numDocs uint32
	avgLen  float64
	docLens map[uint32]uint32
}

func (s *stubIndex) NumDocs() uint32         { return s.numDocs }
func (s *stubIndex) AvgDocLen() float64      { return s.avgLen }
func (s *stubIndex) DocLen(id uint32) uint32 { return s.docLens[id] }

func newStub() *stubIndex {
	return &stubIndex{
		numDocs: 1000,
		avgLen:  100,
		docLens: map[uint32]uint32{0: 50, 1: 100, 2: 200},
	}
}

// BM25 tests

func TestBM25NonNegative(t *testing.T) {
	s := rank.NewBM25(newStub(), 1.2, 0.75)
	score := s.Score(index.PostingEntry{DocID: 0, TermFreq: 3}, 50, 50)
	if score < 0 {
		t.Errorf("BM25 score negative: %f", score)
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		t.Errorf("BM25 score invalid: %f", score)
	}
}

func TestBM25HigherTFHigherScore(t *testing.T) {
	s := rank.NewBM25(newStub(), 1.2, 0.75)
	low := s.Score(index.PostingEntry{DocID: 1, TermFreq: 1}, 10, 100)
	high := s.Score(index.PostingEntry{DocID: 1, TermFreq: 5}, 10, 100)
	if high <= low {
		t.Errorf("higher TF should give higher score: low=%f high=%f", low, high)
	}
}

func TestBM25ShorterDocHigherScore(t *testing.T) {
	// Same TF, same docFreq — shorter doc should score higher (b>0 normalises length)
	s := rank.NewBM25(newStub(), 1.2, 0.75)
	short := s.Score(index.PostingEntry{DocID: 0, TermFreq: 2}, 10, 50) // doc shorter than avg
	long := s.Score(index.PostingEntry{DocID: 2, TermFreq: 2}, 10, 200) // doc longer than avg
	if short <= long {
		t.Errorf("shorter doc should score higher: short=%f long=%f", short, long)
	}
}

func TestBM25RareTermHigherScore(t *testing.T) {
	// Lower docFreq = rarer term = higher IDF = higher score
	s := rank.NewBM25(newStub(), 1.2, 0.75)
	rare := s.Score(index.PostingEntry{DocID: 1, TermFreq: 2}, 5, 100)
	common := s.Score(index.PostingEntry{DocID: 1, TermFreq: 2}, 500, 100)
	if rare <= common {
		t.Errorf("rare term should score higher: rare=%f common=%f", rare, common)
	}
}

func TestBM25TFSaturation(t *testing.T) {
	// With k1=1.2, TF saturates — going from tf=10 to tf=100 adds less than tf=1 to tf=2
	s := rank.NewBM25(newStub(), 1.2, 0.75)
	delta1 := s.Score(index.PostingEntry{DocID: 1, TermFreq: 2}, 10, 100) -
		s.Score(index.PostingEntry{DocID: 1, TermFreq: 1}, 10, 100)
	delta2 := s.Score(index.PostingEntry{DocID: 1, TermFreq: 100}, 10, 100) -
		s.Score(index.PostingEntry{DocID: 1, TermFreq: 10}, 10, 100)
	if delta2 >= delta1 {
		t.Errorf("TF should saturate: delta(1→2)=%f delta(10→100)=%f", delta1, delta2)
	}
}

// TF-IDF tests

func TestTFIDFNonNegative(t *testing.T) {
	s := rank.NewTFIDF(newStub())
	score := s.Score(index.PostingEntry{DocID: 0, TermFreq: 3}, 50, 50)
	if score < 0 || math.IsNaN(score) {
		t.Errorf("TFIDF score invalid: %f", score)
	}
}

func TestTFIDFHigherTFHigherScore(t *testing.T) {
	s := rank.NewTFIDF(newStub())
	low := s.Score(index.PostingEntry{DocID: 0, TermFreq: 1}, 10, 50)
	high := s.Score(index.PostingEntry{DocID: 0, TermFreq: 5}, 10, 50)
	if high <= low {
		t.Errorf("higher TF should give higher TFIDF: low=%f high=%f", low, high)
	}
}
