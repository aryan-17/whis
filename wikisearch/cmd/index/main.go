// Command index builds a search index from a CirrusSearch dump.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()

	start := time.Now()
	var count uint32
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		idx.Add(doc, a)
		count++
		if count%10000 == 0 {
			fmt.Printf("\r  indexed %d docs...", count)
		}
	}
	idx.Finalize()

	fmt.Printf("\nindexed %d docs in %s\n", count, time.Since(start))
}
