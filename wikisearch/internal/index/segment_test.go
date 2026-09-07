package index_test

import (
	"testing"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

func buildAndWriteSegment(t *testing.T) (index.Index, index.Index, string) {
	t.Helper()
	docs := []corpus.Document{
		{ID: 0, Title: "Alpha", Text: "alpha beta gamma delta"},
		{ID: 1, Title: "Beta", Text: "beta gamma delta epsilon"},
		{ID: 2, Title: "Gamma", Text: "gamma delta epsilon zeta"},
	}
	a := analysis.NewAnalyzer()
	mem := index.NewMemoryIndex()
	for _, d := range docs {
		mem.Add(d, a)
	}
	mem.Finalize()

	dir := t.TempDir()
	if err := index.WriteSegment(mem, dir); err != nil {
		t.Fatalf("WriteSegment: %v", err)
	}
	seg, err := index.OpenSegment(dir)
	if err != nil {
		t.Fatalf("OpenSegment: %v", err)
	}
	return mem, seg, dir
}

func TestSegmentNumDocs(t *testing.T) {
	mem, seg, _ := buildAndWriteSegment(t)
	if seg.NumDocs() != mem.NumDocs() {
		t.Errorf("NumDocs: mem=%d seg=%d", mem.NumDocs(), seg.NumDocs())
	}
}

func TestSegmentAvgDocLen(t *testing.T) {
	mem, seg, _ := buildAndWriteSegment(t)
	if seg.AvgDocLen() != mem.AvgDocLen() {
		t.Errorf("AvgDocLen: mem=%f seg=%f", mem.AvgDocLen(), seg.AvgDocLen())
	}
}

func TestSegmentLookupDocFreq(t *testing.T) {
	mem, seg, _ := buildAndWriteSegment(t)
	for _, term := range []string{"gamma", "beta", "delta", "zeta"} {
		mpl, mok := mem.Lookup(term)
		spl, sok := seg.Lookup(term)
		if mok != sok {
			t.Errorf("term %q: mem ok=%v seg ok=%v", term, mok, sok)
			continue
		}
		if !mok {
			continue
		}
		if mpl.DocFreq != spl.DocFreq {
			t.Errorf("term %q: docFreq mem=%d seg=%d", term, mpl.DocFreq, spl.DocFreq)
		}
	}
}

func TestSegmentLookupDocIDs(t *testing.T) {
	mem, seg, _ := buildAndWriteSegment(t)
	for _, term := range []string{"gamma", "delta"} {
		mpl, _ := mem.Lookup(term)
		spl, _ := seg.Lookup(term)
		if len(mpl.Entries) != len(spl.Entries) {
			t.Errorf("term %q: entry count mem=%d seg=%d", term, len(mpl.Entries), len(spl.Entries))
			continue
		}
		for i := range mpl.Entries {
			if mpl.Entries[i].DocID != spl.Entries[i].DocID {
				t.Errorf("term %q entry[%d]: docID mem=%d seg=%d",
					term, i, mpl.Entries[i].DocID, spl.Entries[i].DocID)
			}
			if mpl.Entries[i].TermFreq != spl.Entries[i].TermFreq {
				t.Errorf("term %q entry[%d]: termFreq mem=%d seg=%d",
					term, i, mpl.Entries[i].TermFreq, spl.Entries[i].TermFreq)
			}
		}
	}
}

func TestSegmentDoc(t *testing.T) {
	_, seg, _ := buildAndWriteSegment(t)
	doc, err := seg.Doc(0)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Alpha" {
		t.Errorf("Doc(0).Title = %q, want Alpha", doc.Title)
	}
}

func TestSegmentMissingTerm(t *testing.T) {
	_, seg, _ := buildAndWriteSegment(t)
	_, ok := seg.Lookup("xyzzy")
	if ok {
		t.Error("expected not found for missing term")
	}
}
