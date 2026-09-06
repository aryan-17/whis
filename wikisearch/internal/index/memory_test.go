package index_test

import (
	"testing"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

func buildTestIndex(t *testing.T) index.Index {
	t.Helper()
	docs := []corpus.Document{
		{ID: 0, Title: "Photosynthesis", Text: "photosynthesis is a process used by plants"},
		{ID: 1, Title: "Plant", Text: "a plant is a living thing that uses photosynthesis"},
		{ID: 2, Title: "Animal", Text: "an animal is a living thing that cannot do photosynthesis"},
	}
	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()
	for _, d := range docs {
		idx.Add(d, a)
	}
	idx.Finalize()
	return idx
}

func TestLookupExists(t *testing.T) {
	idx := buildTestIndex(t)
	// "photosynthesis" stems to "photosynthesi"
	pl, ok := idx.Lookup("photosynthesi")
	if !ok {
		t.Fatal("lookup photosynthesi: not found")
	}
	if pl.DocFreq != 3 {
		t.Errorf("docFreq = %d, want 3", pl.DocFreq)
	}
}

func TestLookupMissing(t *testing.T) {
	idx := buildTestIndex(t)
	_, ok := idx.Lookup("xyzzy")
	if ok {
		t.Error("lookup xyzzy: expected not found")
	}
}

func TestPostingsSortedByDocID(t *testing.T) {
	idx := buildTestIndex(t)
	pl, _ := idx.Lookup("photosynthesi")
	for i := 1; i < len(pl.Entries); i++ {
		if pl.Entries[i].DocID <= pl.Entries[i-1].DocID {
			t.Errorf("posting list not sorted at index %d: %d <= %d",
				i, pl.Entries[i].DocID, pl.Entries[i-1].DocID)
		}
	}
}

func TestNumDocs(t *testing.T) {
	idx := buildTestIndex(t)
	if idx.NumDocs() != 3 {
		t.Errorf("NumDocs = %d, want 3", idx.NumDocs())
	}
}

func TestDocLen(t *testing.T) {
	idx := buildTestIndex(t)
	// Length must be non-zero for all docs.
	for i := uint32(0); i < 3; i++ {
		if idx.DocLen(i) == 0 {
			t.Errorf("DocLen(%d) = 0", i)
		}
	}
}

func TestAvgDocLen(t *testing.T) {
	idx := buildTestIndex(t)
	if idx.AvgDocLen() <= 0 {
		t.Error("AvgDocLen() <= 0")
	}
}

func TestDocReturnsTitle(t *testing.T) {
	idx := buildTestIndex(t)
	doc, err := idx.Doc(0)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Photosynthesis" {
		t.Errorf("Doc(0).Title = %q, want Photosynthesis", doc.Title)
	}
}
