package corpus_test

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"wikisearch/internal/corpus"
)

// makeTestDump writes a minimal two-article gzip dump and returns its path.
func makeTestDump(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "dump-*.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)

	type actionLine struct {
		Index struct {
			ID string `json:"_id"`
		} `json:"index"`
	}
	type docLine struct {
		Title        string   `json:"title"`
		Text         string   `json:"text"`
		OutgoingLink []string `json:"outgoing_link"`
	}

	enc := json.NewEncoder(gw)
	enc.Encode(actionLine{})
	enc.Encode(docLine{Title: "Photosynthesis", Text: "Photosynthesis is a process.", OutgoingLink: []string{"Plant"}})
	enc.Encode(actionLine{})
	enc.Encode(docLine{Title: "Plant", Text: "A plant is a living thing.", OutgoingLink: []string{"Photosynthesis"}})

	gw.Close()
	f.Close()
	return f.Name()
}

func TestReaderCount(t *testing.T) {
	r, err := corpus.NewReader(makeTestDump(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var count int
	for {
		_, ok, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		count++
	}
	if count != 2 {
		t.Errorf("got %d docs, want 2", count)
	}
}

func TestReaderFirstTitle(t *testing.T) {
	r, err := corpus.NewReader(makeTestDump(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	doc, ok, err := r.Next()
	if err != nil || !ok {
		t.Fatalf("Next() = %v, %v, %v", doc, ok, err)
	}
	if doc.Title != "Photosynthesis" {
		t.Errorf("got title %q, want Photosynthesis", doc.Title)
	}
}

func TestReaderSequentialIDs(t *testing.T) {
	r, err := corpus.NewReader(makeTestDump(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	d0, _, _ := r.Next()
	d1, _, _ := r.Next()
	if d0.ID != 0 || d1.ID != 1 {
		t.Errorf("IDs = %d, %d; want 0, 1", d0.ID, d1.ID)
	}
}

func TestReaderLinks(t *testing.T) {
	r, err := corpus.NewReader(makeTestDump(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	doc, _, _ := r.Next()
	if len(doc.Links) != 1 || doc.Links[0] != "Plant" {
		t.Errorf("links = %v, want [Plant]", doc.Links)
	}
}

func TestReaderSkipsOddLines(t *testing.T) {
	// Action lines (odd) must be skipped — count must stay at 2
	r, err := corpus.NewReader(makeTestDump(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var count int
	for {
		_, ok, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		count++
	}
	if count != 2 {
		t.Errorf("got %d docs after skip; want 2 (action lines not skipped)", count)
	}
}
