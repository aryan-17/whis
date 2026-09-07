// Package postings defines posting list types and operations.
package postings

// Entry is one (document, term-frequency) pair in a posting list.
// Positions is nil until Sprint 6 (positional index).
// Invariant: posting lists are always sorted by DocID ascending.
type Entry struct {
	DocID     uint32
	TermFreq  uint32
	Positions []uint32
}

// List holds all documents containing a term.
// Entries MUST be sorted by DocID — all callers rely on this.
type List struct {
	Term    string
	DocFreq uint32
	Entries []Entry
}
