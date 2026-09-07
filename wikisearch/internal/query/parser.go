package query

import (
	"fmt"
	"strings"
)

type parser struct {
	tokens []lexToken
	pos    int
}

func (p *parser) peek() lexToken { return p.tokens[p.pos] }
func (p *parser) next() lexToken { t := p.tokens[p.pos]; p.pos++; return t }
func (p *parser) done() bool     { return p.peek().kind == tokEOF }

// Parse converts a query string into an AST node.
func Parse(input string) (Node, error) {
	p := &parser{tokens: lex(input)}
	node := p.parseExpr()
	if !p.done() {
		return nil, fmt.Errorf("unexpected token: %q", p.peek().val)
	}
	return node, nil
}

// parseExpr handles OR — lowest precedence.
func (p *parser) parseExpr() Node {
	left := p.parseTerm()
	children := []Node{left}
	for p.peek().kind == tokOr {
		p.next()
		children = append(children, p.parseTerm())
	}
	if len(children) == 1 {
		return children[0]
	}
	return &OrNode{Children: children}
}

// parseTerm handles implicit/explicit AND.
func (p *parser) parseTerm() Node {
	var children []Node
	for {
		if p.peek().kind == tokAnd {
			p.next() // consume explicit AND
		}
		tok := p.peek()
		if tok.kind == tokEOF || tok.kind == tokRParen || tok.kind == tokOr {
			break
		}
		children = append(children, p.parseFactor())
	}
	if len(children) == 1 {
		return children[0]
	}
	return &AndNode{Children: children}
}

// parseFactor handles NOT.
func (p *parser) parseFactor() Node {
	if p.peek().kind == tokNot {
		p.next()
		return &NotNode{Child: p.parseAtom()}
	}
	return p.parseAtom()
}

// parseAtom handles words, phrases, field:, and parenthesised expressions.
func (p *parser) parseAtom() Node {
	tok := p.next()
	switch tok.kind {
	case tokWord:
		// Check for field: next token is colon.
		if p.peek().kind == tokColon {
			p.next() // consume ':'
			return &FieldNode{Field: tok.val, Child: p.parseAtom()}
		}
		return &TermNode{Term: tok.val}
	case tokPhrase:
		return &PhraseNode{Terms: splitWords(tok.val)}
	case tokLParen:
		node := p.parseExpr()
		if p.peek().kind == tokRParen {
			p.next()
		}
		return node
	default:
		// Treat unexpected token as a plain term.
		return &TermNode{Term: tok.val}
	}
}

func splitWords(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}
