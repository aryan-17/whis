package analysis

// Analyzer runs the full text analysis pipeline:
// tokenize → lowercase → NFC normalize → stopword filter → Porter2 stem.
//
// Use the same Analyzer at index-time and query-time.
// Any difference between the two produces silent misses.
type Analyzer struct{}

// NewAnalyzer returns an Analyzer with the standard pipeline.
func NewAnalyzer() *Analyzer { return &Analyzer{} }

// Analyze returns the analyzed token stream for text.
func (a *Analyzer) Analyze(text string) []Token {
	tokens := Tokenize(text)
	tokens = LowercaseFilter(tokens)
	tokens = NormalizeFilter(tokens)
	tokens = StopwordFilter(tokens)
	tokens = StemFilter(tokens)
	return tokens
}
