// Command eval runs the evaluation harness and prints P@10/MRR/NDCG.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/eval"
	"wikisearch/internal/index"
	"wikisearch/internal/postings"
	"wikisearch/internal/rank"
)

type queryCase struct {
	Query    string   `json:"query"`
	Relevant []string `json:"relevant"`
}

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	queries := flag.String("queries", "testdata/queries.json", "path to queries.json")
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

	data, err := os.ReadFile(*queries)
	if err != nil {
		log.Fatal(err)
	}
	var cases []queryCase
	if err := json.Unmarshal(data, &cases); err != nil {
		log.Fatal(err)
	}

	var totalP, totalMRR, totalNDCG float64
	for _, c := range cases {
		tokens := a.Analyze(c.Query)
		if len(tokens) == 0 {
			continue
		}

		result, ok := idx.Lookup(tokens[0].Term)
		if !ok {
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

		// Cache posting lists once per query — not per candidate document.
		termPLs := make(map[string]postings.List, len(tokens))
		for _, tok := range tokens {
			if pl, ok := idx.Lookup(tok.Term); ok {
				termPLs[tok.Term] = pl
			}
		}

		results := make([]rank.Result, 0, len(result.Entries))
		for _, entry := range result.Entries {
			var score float64
			for _, tok := range tokens {
				if pl, ok := termPLs[tok.Term]; ok {
					score += scorer.Score(entry, pl.DocFreq, idx.DocLen(entry.DocID))
				}
			}
			results = append(results, rank.Result{DocID: entry.DocID, Score: score})
		}

		top := rank.TopK(results, 10)
		titles := make([]string, len(top))
		for i, res := range top {
			doc, _ := idx.Doc(res.DocID)
			titles[i] = doc.Title
		}

		rel := make(map[string]bool, len(c.Relevant))
		for _, r := range c.Relevant {
			rel[r] = true
		}
		totalP += eval.PrecisionAtK(titles, rel, 10)
		totalMRR += eval.MRR(titles, rel)
		totalNDCG += eval.NDCGAtK(titles, rel, 10)
	}

	n := float64(len(cases))
	fmt.Printf("\n%-20s  %6s  %6s  %8s\n", "ranker", "P@10", "MRR", "NDCG@10")
	fmt.Printf("%-20s  %6.3f  %6.3f  %8.3f\n", "mine (bm25)", totalP/n, totalMRR/n, totalNDCG/n)
}
