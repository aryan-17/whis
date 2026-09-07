package postings_test

import (
	"testing"

	"wikisearch/internal/postings"
)

func entryWithPos(docID uint32, positions ...uint32) postings.Entry {
	return postings.Entry{DocID: docID, TermFreq: uint32(len(positions)), Positions: positions}
}

func TestPhraseIntersectMatch(t *testing.T) {
	// "climate" at pos 2,5 in doc0; "change" at pos 3,9 in doc0.
	// pos(change)=3 == pos(climate)=2 + gap=1 → phrase match in doc0.
	climate := postings.List{Entries: []postings.Entry{
		entryWithPos(0, 2, 5),
		entryWithPos(1, 0),
	}}
	change := postings.List{Entries: []postings.Entry{
		entryWithPos(0, 3, 9),
		entryWithPos(2, 1),
	}}
	result := postings.PhraseIntersect(climate, change, 1)
	if len(result.Entries) != 1 {
		t.Fatalf("got %d docs, want 1: %v", len(result.Entries), result.Entries)
	}
	if result.Entries[0].DocID != 0 {
		t.Errorf("got docID %d, want 0", result.Entries[0].DocID)
	}
}

func TestPhraseIntersectNoMatch(t *testing.T) {
	// "climate" at pos 0; "change" at pos 5 — gap=5, not adjacent.
	climate := postings.List{Entries: []postings.Entry{entryWithPos(0, 0)}}
	change := postings.List{Entries: []postings.Entry{entryWithPos(0, 5)}}
	result := postings.PhraseIntersect(climate, change, 1)
	if len(result.Entries) != 0 {
		t.Errorf("expected no match, got %v", result.Entries)
	}
}

func TestPhraseIntersectDifferentDocs(t *testing.T) {
	// Words appear in different documents — no shared doc → no match.
	a := postings.List{Entries: []postings.Entry{entryWithPos(0, 1)}}
	b := postings.List{Entries: []postings.Entry{entryWithPos(1, 2)}}
	result := postings.PhraseIntersect(a, b, 1)
	if len(result.Entries) != 0 {
		t.Errorf("expected no match across docs, got %v", result.Entries)
	}
}

func TestPhraseIntersectMultipleMatches(t *testing.T) {
	// Both doc0 and doc1 contain the phrase.
	a := postings.List{Entries: []postings.Entry{
		entryWithPos(0, 0),
		entryWithPos(1, 3),
	}}
	b := postings.List{Entries: []postings.Entry{
		entryWithPos(0, 1), // gap=1 from pos 0 ✓
		entryWithPos(1, 4), // gap=1 from pos 3 ✓
	}}
	result := postings.PhraseIntersect(a, b, 1)
	if len(result.Entries) != 2 {
		t.Errorf("got %d matches, want 2", len(result.Entries))
	}
}

func TestPhraseIntersectStopwordGap(t *testing.T) {
	// Phrase "climate the change" with stopword dropped.
	// "climate"@0, "change"@2 (gap=2, "the" dropped at pos 1).
	climate := postings.List{Entries: []postings.Entry{entryWithPos(0, 0)}}
	change := postings.List{Entries: []postings.Entry{entryWithPos(0, 2)}}
	// gap=1 → no match (stopword gap means they're not adjacent)
	result1 := postings.PhraseIntersect(climate, change, 1)
	if len(result1.Entries) != 0 {
		t.Error("gap=1 should not match across stopword gap")
	}
	// gap=2 → match
	result2 := postings.PhraseIntersect(climate, change, 2)
	if len(result2.Entries) != 1 {
		t.Error("gap=2 should match across one stopword gap")
	}
}
