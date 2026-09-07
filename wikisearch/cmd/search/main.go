// Command search — interactive ranked search REPL.
// Sprint 6: phrase queries, structured query parsing, snippets.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
	"wikisearch/internal/postings"
	"wikisearch/internal/query"
	"wikisearch/internal/rank"
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	fmt.Print("building index...")
	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		idx.Add(doc, a)
	}
	idx.Finalize()
	fmt.Printf(" done (%d docs)\n", idx.NumDocs())

	scorer := rank.NewBM25(idx, 1.2, 0.75)

	sc := bufio.NewScanner(os.Stdin)
	fmt.Println(`Query syntax: terms  "phrase"  AND OR NOT  field:term  (grouped)`)
	fmt.Print("> ")

	for sc.Scan() {
		input := strings.TrimSpace(sc.Text())
		if input == "" {
			fmt.Print("> ")
			continue
		}

		start := time.Now()

		// Parse query into AST.
		ast, err := query.Parse(input)
		if err != nil {
			fmt.Printf("parse error: %v\n", err)
			fmt.Print("> ")
			continue
		}

		// Evaluate AST to a posting list.
		result, ok := evalNode(ast, idx, a)
		if !ok || len(result.Entries) == 0 {
			fmt.Printf("found 0 documents in %s\n", time.Since(start))
			fmt.Print("> ")
			continue
		}

		// Score with BM25.
		queryTerms := collectTerms(ast, a)
		results := make([]rank.Result, 0, len(result.Entries))
		for _, entry := range result.Entries {
			var score float64
			for _, term := range queryTerms {
				pl, ok := idx.Lookup(term)
				if !ok {
					continue
				}
				score += scorer.Score(entry, pl.DocFreq, idx.DocLen(entry.DocID))
			}
			results = append(results, rank.Result{DocID: entry.DocID, Score: score})
		}

		top := rank.TopK(results, 10)
		fmt.Printf("found %d documents in %s\n", len(result.Entries), time.Since(start))
		for i, res := range top {
			doc, _ := idx.Doc(res.DocID)
			snip := rank.Snippet(doc.Text, queryTerms, 160)
			fmt.Printf("%d. %s (%.4f)\n   %s\n", i+1, doc.Title, res.Score, snip)
		}
		fmt.Print("> ")
	}
}

// evalNode recursively evaluates an AST node against the index.
func evalNode(node query.Node, idx index.Index, a *analysis.Analyzer) (postings.List, bool) {
	switch n := node.(type) {
	case *query.TermNode:
		terms := a.Analyze(n.Term)
		if len(terms) == 0 {
			return postings.List{}, false
		}
		return idx.Lookup(terms[0].Term)

	case *query.PhraseNode:
		if len(n.Terms) == 0 {
			return postings.List{}, false
		}
		analyzed := make([][]analysis.Token, len(n.Terms))
		for i, w := range n.Terms {
			analyzed[i] = a.Analyze(w)
		}
		// Start with first term's posting list.
		if len(analyzed[0]) == 0 {
			return postings.List{}, false
		}
		result, ok := idx.Lookup(analyzed[0][0].Term)
		if !ok {
			return postings.List{}, false
		}
		// Intersect with each subsequent term using PhraseIntersect.
		for i := 1; i < len(analyzed); i++ {
			if len(analyzed[i]) == 0 {
				continue
			}
			other, ok := idx.Lookup(analyzed[i][0].Term)
			if !ok {
				return postings.List{}, false
			}
			result = postings.PhraseIntersect(result, other, uint32(i))
		}
		return result, true

	case *query.AndNode:
		if len(n.Children) == 0 {
			return postings.List{}, false
		}
		result, ok := evalNode(n.Children[0], idx, a)
		if !ok {
			return postings.List{}, false
		}
		for _, child := range n.Children[1:] {
			other, ok := evalNode(child, idx, a)
			if !ok {
				return postings.List{}, false
			}
			result = postings.Intersect(result, other)
		}
		return result, true

	case *query.OrNode:
		if len(n.Children) == 0 {
			return postings.List{}, false
		}
		result, ok := evalNode(n.Children[0], idx, a)
		if !ok {
			return postings.List{}, false
		}
		for _, child := range n.Children[1:] {
			other, ok := evalNode(child, idx, a)
			if !ok {
				continue
			}
			result = postings.Union(result, other)
		}
		return result, true

	case *query.NotNode:
		// NOT alone returns nothing useful — used only as part of AND NOT.
		return postings.List{}, false

	case *query.FieldNode:
		// For now treat field queries same as term queries (title boost in Sprint 4+).
		return evalNode(n.Child, idx, a)

	default:
		return postings.List{}, false
	}
}

// collectTerms extracts all leaf term strings from an AST for scoring/snippets.
func collectTerms(node query.Node, a *analysis.Analyzer) []string {
	var terms []string
	switch n := node.(type) {
	case *query.TermNode:
		for _, tok := range a.Analyze(n.Term) {
			terms = append(terms, tok.Term)
		}
	case *query.PhraseNode:
		for _, w := range n.Terms {
			for _, tok := range a.Analyze(w) {
				terms = append(terms, tok.Term)
			}
		}
	case *query.AndNode:
		for _, c := range n.Children {
			terms = append(terms, collectTerms(c, a)...)
		}
	case *query.OrNode:
		for _, c := range n.Children {
			terms = append(terms, collectTerms(c, a)...)
		}
	case *query.NotNode:
		// Don't score/highlight NOT terms.
	case *query.FieldNode:
		terms = append(terms, collectTerms(n.Child, a)...)
	}
	return terms
}
