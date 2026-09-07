package query

import "strings"

type tokenKind int

const (
	tokWord   tokenKind = iota
	tokPhrase           // "quoted string"
	tokAnd              // AND
	tokOr               // OR
	tokNot              // NOT
	tokLParen           // (
	tokRParen           // )
	tokColon            // :
	tokEOF
)

type lexToken struct {
	kind tokenKind
	val  string
}

// lex converts input into a flat token slice.
func lex(input string) []lexToken {
	var tokens []lexToken
	i := 0
	for i < len(input) {
		switch {
		case input[i] == ' ' || input[i] == '\t':
			i++
		case input[i] == '(':
			tokens = append(tokens, lexToken{tokLParen, "("})
			i++
		case input[i] == ')':
			tokens = append(tokens, lexToken{tokRParen, ")"})
			i++
		case input[i] == ':':
			tokens = append(tokens, lexToken{tokColon, ":"})
			i++
		case input[i] == '"':
			// Scan to closing quote.
			j := strings.Index(input[i+1:], `"`)
			var phrase string
			if j == -1 {
				phrase = input[i+1:]
				i = len(input)
			} else {
				phrase = input[i+1 : i+1+j]
				i = i + 1 + j + 1
			}
			tokens = append(tokens, lexToken{tokPhrase, phrase})
		default:
			// Scan a word (stops at space, paren, colon, quote).
			j := i
			for j < len(input) && input[j] != ' ' && input[j] != '\t' &&
				input[j] != '(' && input[j] != ')' &&
				input[j] != ':' && input[j] != '"' {
				j++
			}
			word := input[i:j]
			i = j
			switch strings.ToUpper(word) {
			case "AND":
				tokens = append(tokens, lexToken{tokAnd, word})
			case "OR":
				tokens = append(tokens, lexToken{tokOr, word})
			case "NOT":
				tokens = append(tokens, lexToken{tokNot, word})
			default:
				tokens = append(tokens, lexToken{tokWord, word})
			}
		}
	}
	tokens = append(tokens, lexToken{tokEOF, ""})
	return tokens
}
