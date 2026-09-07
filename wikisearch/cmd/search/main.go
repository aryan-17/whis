// Command search is an interactive ranked search REPL over an in-memory index.
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
	fmt.Print("> ")
	for sc.Scan() {
		query := strings.TrimSpace(sc.Text())
		if query == "" {
			fmt.Print("> ")
			continue
		}

		start := time.Now()
		tokens := a.Analyze(query)
		if len(tokens) == 0 {
			fmt.Println("(no terms after analysis)")
			fmt.Print("> ")
			continue
		}

		result, ok := idx.Lookup(tokens[0].Term)
		if !ok {
			fmt.Printf("found 0 documents in %s\n", time.Since(start))
			fmt.Print("> ")
			continue
		}
		for _, tok := range tokens[1:] {
			other, ok := idx.Lookup(tok.Term)
			if !ok {
				result.Entries = nil
				break
			}
			result = postings.Intersect(result, other)
		}

		results := make([]rank.Result, 0, len(result.Entries))
		for _, entry := range result.Entries {
			var score float64
			for _, tok := range tokens {
				pl, ok := idx.Lookup(tok.Term)
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
			fmt.Printf("%d. %s (%.4f)\n", i+1, doc.Title, res.Score)
		}
		fmt.Print("> ")
	}
}
