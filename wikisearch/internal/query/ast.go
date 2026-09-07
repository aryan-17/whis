// Package query implements a query lexer, parser, and AST.
// Grammar:
//
//	expr   := term (OR term)*
//	term   := factor (AND? factor)*      AND is optional (implicit)
//	factor := NOT? atom
//	atom   := WORD | PHRASE | FIELD:atom | '(' expr ')'
package query

// Node is an AST node. All concrete types implement this interface.
type Node interface{ node() }

// AndNode represents an implicit or explicit AND between children.
type AndNode struct{ Children []Node }

// OrNode represents an OR between children.
type OrNode struct{ Children []Node }

// NotNode negates its child.
type NotNode struct{ Child Node }

// TermNode is a single analysed search term.
type TermNode struct{ Term string }

// PhraseNode is a quoted phrase — terms must appear adjacent in order.
type PhraseNode struct{ Terms []string }

// FieldNode scopes its child to a specific field (e.g. title:foo).
type FieldNode struct {
	Field string
	Child Node
}

func (*AndNode) node()    {}
func (*OrNode) node()     {}
func (*NotNode) node()    {}
func (*TermNode) node()   {}
func (*PhraseNode) node() {}
func (*FieldNode) node()  {}
