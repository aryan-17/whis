// Command search — interactive ranked search REPL.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
	"wikisearch/internal/link"
	"wikisearch/internal/postings"
	"wikisearch/internal/query"
	"wikisearch/internal/rank"
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2); required unless -index given")
	idxDir := flag.String("index", "", "pre-built segment directory (fast startup)")
	prWeight := flag.Float64("pr-weight", 1.0, "PageRank blend weight")
	flag.Parse()

	a := analysis.NewAnalyzer()
	var idx index.Index
	var prScores []float64

	if *idxDir != "" {
		// Fast path: load pre-built segment from disk (milliseconds).
		seg, err := index.OpenSegment(*idxDir)
		if err != nil {
			log.Fatalf("open segment: %v", err)
		}
		idx = seg
		fmt.Printf("loaded segment (%d docs)\n", idx.NumDocs())

		// Load PageRank if available.
		if data, err := os.ReadFile(filepath.Join(*idxDir, "pagerank.json")); err == nil {
			if err := json.Unmarshal(data, &prScores); err != nil {
				log.Printf("warn: pagerank.json parse failed: %v", err)
			} else {
				fmt.Println("loaded PageRank from disk")
			}
		}
	} else {
		// Slow path: build MemoryIndex from dump.
		if *dump == "" {
			log.Fatal("-dump or -index required")
		}
		fmt.Print("building index...")
		r, err := corpus.NewReader(*dump)
		if err != nil {
			log.Fatal(err)
		}
		defer r.Close()

		mem := index.NewMemoryIndex()
		var allLinks [][]string
		titleToID := make(map[string]uint32)
		for {
			doc, ok, err := r.Next()
			if err != nil {
				log.Fatal(err)
			}
			if !ok {
				break
			}
			mem.Add(doc, a)
			titleToID[doc.Title] = doc.ID
			allLinks = append(allLinks, doc.Links)
		}
		mem.Finalize()
		idx = mem
		fmt.Printf(" done (%d docs)\n", idx.NumDocs())

		fmt.Print("computing PageRank...")
		graph := link.BuildGraph(int(idx.NumDocs()), allLinks, titleToID)
		prScores = link.PageRank(graph, int(idx.NumDocs()), 0.85, 30)
		fmt.Println(" done")
	}

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
		ast, err := query.Parse(input)
		if err != nil {
			fmt.Printf("parse error: %v\n", err)
			fmt.Print("> ")
			continue
		}

		result, ok := evalNode(ast, idx, a)
		if !ok || len(result.Entries) == 0 {
			fmt.Printf("found 0 documents in %s\n", time.Since(start))
			fmt.Print("> ")
			continue
		}

		queryTerms := collectTerms(ast, a)

		// Cache posting lists once — not per candidate document.
		termPLs := make(map[string]postings.List, len(queryTerms))
		for _, term := range queryTerms {
			if pl, ok := idx.Lookup(term); ok {
				termPLs[term] = pl
			}
		}

		results := make([]rank.Result, 0, len(result.Entries))
		for _, entry := range result.Entries {
			var bm25Score float64
			for _, term := range queryTerms {
				if pl, ok := termPLs[term]; ok {
					bm25Score += scorer.Score(entry, pl.DocFreq, idx.DocLen(entry.DocID))
				}
			}
			pr := 0.0
			if int(entry.DocID) < len(prScores) {
				pr = prScores[entry.DocID]
			}
			results = append(results, rank.Result{
				DocID: entry.DocID,
				Score: bm25Score + *prWeight*math.Log1p(pr),
			})
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

func evalNode(node query.Node, idx index.Index, a *analysis.Analyzer) (postings.List, bool) {
	switch n := node.(type) {
	case *query.TermNode:
		tokens := a.Analyze(n.Term)
		if len(tokens) == 0 {
			return postings.List{}, false
		}
		return idx.Lookup(tokens[0].Term)

	case *query.PhraseNode:
		if len(n.Terms) == 0 {
			return postings.List{}, false
		}
		analyzed := make([][]analysis.Token, len(n.Terms))
		for i, w := range n.Terms {
			analyzed[i] = a.Analyze(w)
		}
		if len(analyzed[0]) == 0 {
			return postings.List{}, false
		}
		result, ok := idx.Lookup(analyzed[0][0].Term)
		if !ok {
			return postings.List{}, false
		}
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
		return postings.List{}, false

	case *query.FieldNode:
		return evalNode(n.Child, idx, a)

	default:
		return postings.List{}, false
	}
}

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
	case *query.FieldNode:
		terms = append(terms, collectTerms(n.Child, a)...)
	}
	return terms
}
