// Command search is an interactive boolean search REPL over an in-memory index.
// Sprint 3: AND all query terms, print top 10 results in docID order.
// Sprint 4 will replace this with BM25 ranking.
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
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	// Build index.
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

	// REPL.
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

		// AND all terms together using two-pointer intersect.
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

		elapsed := time.Since(start)
		fmt.Printf("found %d documents in %s\n", len(result.Entries), elapsed)

		limit := 10
		if len(result.Entries) < limit {
			limit = len(result.Entries)
		}
		for i, entry := range result.Entries[:limit] {
			doc, _ := idx.Doc(entry.DocID)
			fmt.Printf("%d. %s\n", i+1, doc.Title)
		}
		fmt.Print("> ")
	}
}
