// Command index builds a search index from a CirrusSearch dump.
// Sprint 1: prints document count and first title as a smoke test.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"wikisearch/internal/corpus"
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

	start := time.Now()
	var count uint32
	var firstTitle string

	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		if count == 0 {
			firstTitle = doc.Title
		}
		count++
		if count%10000 == 0 {
			fmt.Printf("\r  read %d docs...", count)
		}
	}

	fmt.Printf("\ndocs:        %d\nfirst title: %s\nelapsed:     %s\n",
		count, firstTitle, time.Since(start))
}
