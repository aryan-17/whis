package rank

import (
	"strings"
	"unicode"
)

// Snippet finds the ~windowSize-character passage with the highest query term
// density and returns it with matching terms highlighted using ANSI bold.
func Snippet(text string, queryTerms []string, windowSize int) string {
	if text == "" {
		return ""
	}

	// Split into words preserving start byte offsets.
	type word struct {
		text  string
		start int
	}
	var words []word
	inWord := false
	start := 0
	for i, r := range text {
		isAlnum := unicode.IsLetter(r) || unicode.IsDigit(r)
		if isAlnum && !inWord {
			start = i
			inWord = true
		} else if !isAlnum && inWord {
			words = append(words, word{text[start:i], start})
			inWord = false
		}
	}
	if inWord {
		words = append(words, word{text[start:], start})
	}
	if len(words) == 0 {
		return ""
	}

	termSet := make(map[string]bool, len(queryTerms))
	for _, t := range queryTerms {
		termSet[strings.ToLower(t)] = true
	}

	// Slide a word-count window, pick window with most hits.
	wSize := windowSize / 6 // rough words-per-window estimate
	if wSize < 5 {
		wSize = 5
	}
	bestStart, bestHits := 0, -1
	for i := range words {
		end := i + wSize
		if end > len(words) {
			end = len(words)
		}
		hits := 0
		for _, w := range words[i:end] {
			if termSet[strings.ToLower(w.text)] {
				hits++
			}
		}
		if hits > bestHits {
			bestHits = hits
			bestStart = i
		}
	}

	bestEnd := bestStart + wSize
	if bestEnd > len(words) {
		bestEnd = len(words)
	}

	// Extract text span: from start of first word to end of last word.
	spanStart := words[bestStart].start
	lastWord := words[bestEnd-1]
	spanEnd := lastWord.start + len(lastWord.text)
	snippet := text[spanStart:spanEnd]

	// Highlight query terms with ANSI bold — case-insensitive.
	for _, term := range queryTerms {
		lsnip := strings.ToLower(snippet)
		lterm := strings.ToLower(term)
		idx := strings.Index(lsnip, lterm)
		if idx >= 0 {
			orig := snippet[idx : idx+len(term)]
			snippet = strings.Replace(snippet, orig, "\033[1m"+orig+"\033[0m", 1)
		}
	}
	return snippet
}
