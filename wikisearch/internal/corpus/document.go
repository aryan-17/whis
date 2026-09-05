// Package corpus handles reading and representing Wikipedia dump documents.
package corpus

// Document is one Wikipedia article decoded from a CirrusSearch dump.
// ID is assigned sequentially by the Reader — it is not the Wikipedia page ID.
// Length is the token count, set by the indexer after analysis.
type Document struct {
	ID     uint32
	Title  string
	Text   string
	Links  []string // outgoing_link field — used for PageRank in Phase 4
	Length uint32
}
