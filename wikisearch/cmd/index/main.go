// Command index builds a search index from a CirrusSearch dump, writes it to
// disk, and computes PageRank over the document link graph.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
	"wikisearch/internal/link"
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	out := flag.String("index", "data/index", "directory to write segment files")
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

	// Collect link data while indexing.
	var allLinks [][]string // docID → outgoing titles
	titleToID := make(map[string]uint32)

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
		titleToID[doc.Title] = doc.ID
		allLinks = append(allLinks, doc.Links)
		count++
		if count%10000 == 0 {
			fmt.Printf("\r  indexed %d docs...", count)
		}
	}
	idx.Finalize()
	fmt.Printf("\nindexed %d docs in %s\n", count, time.Since(start))

	// Write segment.
	fmt.Printf("writing segment to %s...", *out)
	if err := index.WriteSegment(idx, *out); err != nil {
		log.Fatal(err)
	}
	fmt.Println(" done")

	// Compute PageRank.
	fmt.Print("computing PageRank...")
	graph := link.BuildGraph(int(count), allLinks, titleToID)
	prScores := link.PageRank(graph, int(count), 0.85, 30)
	fmt.Println(" done")

	// Persist PageRank scores alongside segment files.
	prPath := filepath.Join(*out, "pagerank.json")
	data, _ := json.Marshal(prScores)
	if err := os.WriteFile(prPath, data, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("PageRank written to %s\n", prPath)
}
