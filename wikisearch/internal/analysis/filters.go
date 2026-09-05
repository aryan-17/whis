package analysis

import (
	"strings"

	"github.com/kljensen/snowball/english"
	"golang.org/x/text/unicode/norm"
)

// LowercaseFilter lowercases all token terms.
func LowercaseFilter(tokens []Token) []Token {
	for i := range tokens {
		tokens[i].Term = strings.ToLower(tokens[i].Term)
	}
	return tokens
}

// NormalizeFilter applies Unicode NFC normalization.
// Ensures composed form (e.g. é as one code point, not e + combining accent).
func NormalizeFilter(tokens []Token) []Token {
	for i := range tokens {
		tokens[i].Term = norm.NFC.String(tokens[i].Term)
	}
	return tokens
}

// stopwords is a minimal set of common English function words.
// Keeping this small is intentional: aggressive stopword lists harm recall
// on queries like "to be or not to be".
var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "but": {},
	"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {},
	"with": {}, "by": {}, "from": {}, "is": {}, "it": {}, "its": {},
	"as": {}, "be": {}, "was": {}, "are": {}, "were": {}, "has": {},
	"have": {}, "had": {}, "he": {}, "she": {}, "they": {}, "we": {},
	"i": {}, "you": {}, "that": {}, "this": {}, "which": {}, "who": {},
	"not": {}, "no": {}, "so": {}, "if": {}, "do": {}, "did": {},
	"will": {}, "can": {}, "may": {}, "about": {}, "than": {}, "into": {},
}

// StopwordFilter removes common English stopwords.
// Positions are NOT renumbered — gaps are preserved so phrase queries work.
func StopwordFilter(tokens []Token) []Token {
	out := tokens[:0]
	for _, tok := range tokens {
		if _, drop := stopwords[tok.Term]; !drop {
			out = append(out, tok)
		}
	}
	return out
}

// StemFilter applies Porter2 (Snowball) stemming.
func StemFilter(tokens []Token) []Token {
	for i := range tokens {
		tokens[i].Term = english.Stem(tokens[i].Term, false)
	}
	return tokens
}
