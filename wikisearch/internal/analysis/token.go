// Package analysis implements the text analysis pipeline:
// tokenize → lowercase → normalize → stopword filter → stem.
//
// The same Analyzer instance must be used at index-time and query-time.
// Any divergence causes silent misses: a stemmed query token won't match
// an unstemmed index token.
package analysis

// Token is one term extracted from text, with its ordinal position.
// Position is NOT a byte offset — it is the token's index in the stream.
// Filters that drop tokens (e.g. stopwords) must leave a positional gap;
// they must NOT renumber remaining positions. Phrase queries depend on this.
type Token struct {
	Term     string
	Position int
}
