package analysis

import "unicode"

// Tokenize splits text on runs of non-letter, non-digit characters.
// Uses unicode.IsLetter so accented and non-Latin letters are kept intact.
// Positions are sequential ordinal indices starting at 0.
func Tokenize(text string) []Token {
	var tokens []Token
	var buf []rune
	pos := 0

	flush := func() {
		if len(buf) > 0 {
			tokens = append(tokens, Token{Term: string(buf), Position: pos})
			pos++
			buf = buf[:0]
		}
	}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf = append(buf, r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}
