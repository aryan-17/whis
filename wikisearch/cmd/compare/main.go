// Command compare runs the same 15 queries against both your engine and
// Elasticsearch, then prints a side-by-side P@10 / MRR / NDCG@10 table.
//
// Usage:
//
//	# With pre-built segment (fast):
//	go run ./cmd/compare -index data/index -dump data/sample.json.gz
//
//	# Or rebuild from dump each time:
//	go run ./cmd/compare -dump data/sample.json.gz
//
// Requires Elasticsearch running and loaded via cmd/esload.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

type metrics struct {
	p, mrr, ndcg float64
}

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2); required unless -index given")
	idxDir := flag.String("index", "", "pre-built segment directory")
	queries := flag.String("queries", "testdata/queries.json", "path to queries.json")
	esURL := flag.String("es", "http://localhost:9200", "Elasticsearch base URL")
	esIndex := flag.String("es-index", "wiki", "ES index name")
	flag.Parse()

	// Load query cases.
	data, err := os.ReadFile(*queries)
	if err != nil {
		log.Fatalf("read queries: %v", err)
	}
	var cases []queryCase
	if err := json.Unmarshal(data, &cases); err != nil {
		log.Fatalf("parse queries: %v", err)
	}

	// --- My engine ---
	myMetrics := runMyEngine(*dump, *idxDir, cases)

	// --- SQLite baseline ---
	// (reuse cmd/baseline logic inline would be circular; skip for brevity)

	// --- Elasticsearch ---
	esMetrics, esAvail := runElasticsearch(*esURL, *esIndex, cases)

	// --- Print table ---
	fmt.Printf("\n%-22s  %6s  %6s  %8s\n", "ranker", "P@10", "MRR", "NDCG@10")
	fmt.Println(strings.Repeat("-", 50))
	printRow("mine (bm25+pagerank)", myMetrics, len(cases))
	if esAvail {
		printRow("elasticsearch", esMetrics, len(cases))
	} else {
		fmt.Printf("%-22s  %s\n", "elasticsearch", "(not reachable — start ES and run cmd/esload)")
	}

	// Per-query breakdown if both available.
	if esAvail {
		fmt.Println()
		printPerQueryBreakdown(cases, *dump, *idxDir, *esURL, *esIndex)
	}
}

func printRow(name string, m metrics, n int) {
	fmt.Printf("%-22s  %6.3f  %6.3f  %8.3f\n",
		name, m.p/float64(n), m.mrr/float64(n), m.ndcg/float64(n))
}

// ── My engine ────────────────────────────────────────────────────────────────

func runMyEngine(dump, idxDir string, cases []queryCase) metrics {
	a := analysis.NewAnalyzer()
	var idx index.Index

	if idxDir != "" {
		seg, err := index.OpenSegment(idxDir)
		if err != nil {
			log.Fatalf("open segment: %v", err)
		}
		idx = seg
		fmt.Printf("my engine: loaded segment (%d docs)\n", idx.NumDocs())
	} else {
		if dump == "" {
			log.Fatal("-dump or -index required")
		}
		fmt.Print("my engine: building index...")
		r, err := corpus.NewReader(dump)
		if err != nil {
			log.Fatal(err)
		}
		defer r.Close()
		mem := index.NewMemoryIndex()
		for {
			doc, ok, err := r.Next()
			if err != nil {
				log.Fatal(err)
			}
			if !ok {
				break
			}
			mem.Add(doc, a)
		}
		mem.Finalize()
		idx = mem
		fmt.Printf(" done (%d docs)\n", idx.NumDocs())
	}

	// Load PageRank if available.
	var prScores []float64
	if idxDir != "" {
		if data, err := os.ReadFile(filepath.Join(idxDir, "pagerank.json")); err == nil {
			json.Unmarshal(data, &prScores)
		}
	}

	scorer := rank.NewBM25(idx, 1.2, 0.75)
	var m metrics

	for _, c := range cases {
		titles := mySearch(idx, a, scorer, prScores, c.Query, 10)
		rel := relevanceMap(c.Relevant)
		m.p += eval.PrecisionAtK(titles, rel, 10)
		m.mrr += eval.MRR(titles, rel)
		m.ndcg += eval.NDCGAtK(titles, rel, 10)
	}
	return m
}

func mySearch(idx index.Index, a *analysis.Analyzer, scorer rank.Scorer,
	prScores []float64, query string, k int) []string {

	tokens := a.Analyze(query)
	if len(tokens) == 0 {
		return nil
	}

	result, ok := idx.Lookup(tokens[0].Term)
	if !ok {
		return nil
	}
	for _, tok := range tokens[1:] {
		other, ok := idx.Lookup(tok.Term)
		if !ok {
			return nil
		}
		result = postings.Intersect(result, other)
	}

	termPLs := make(map[string]postings.List, len(tokens))
	for _, tok := range tokens {
		if pl, ok := idx.Lookup(tok.Term); ok {
			termPLs[tok.Term] = pl
		}
	}

	results := make([]rank.Result, 0, len(result.Entries))
	for _, entry := range result.Entries {
		var bm25Score float64
		for _, tok := range tokens {
			if pl, ok := termPLs[tok.Term]; ok {
				bm25Score += scorer.Score(entry, pl.DocFreq, idx.DocLen(entry.DocID))
			}
		}
		pr := 0.0
		if int(entry.DocID) < len(prScores) {
			pr = prScores[entry.DocID]
		}
		results = append(results, rank.Result{
			DocID: entry.DocID,
			Score: bm25Score + pr,
		})
	}

	top := rank.TopK(results, k)
	titles := make([]string, len(top))
	for i, res := range top {
		doc, _ := idx.Doc(res.DocID)
		titles[i] = doc.Title
	}
	return titles
}

// ── Elasticsearch ─────────────────────────────────────────────────────────────

func runElasticsearch(esURL, esIndex string, cases []queryCase) (metrics, bool) {
	base := strings.TrimRight(esURL, "/")

	// Ping to check availability.
	resp, err := http.Get(base + "/_cluster/health")
	if err != nil || resp.StatusCode >= 400 {
		return metrics{}, false
	}
	resp.Body.Close()

	fmt.Printf("elasticsearch: connected at %s\n", base)
	var m metrics
	for _, c := range cases {
		titles := esSearch(base, esIndex, c.Query, 10)
		rel := relevanceMap(c.Relevant)
		m.p += eval.PrecisionAtK(titles, rel, 10)
		m.mrr += eval.MRR(titles, rel)
		m.ndcg += eval.NDCGAtK(titles, rel, 10)
	}
	return m, true
}

func esSearch(base, esIndex, query string, size int) []string {
	body, _ := json.Marshal(map[string]interface{}{
		"size": size,
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  query,
				"fields": []string{"title^2", "text"},
				"type":   "best_fields",
			},
		},
		"_source": []string{"title"},
	})

	resp, err := http.Post(
		fmt.Sprintf("%s/%s/_search", base, esIndex),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					Title string `json:"title"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	titles := make([]string, 0, len(result.Hits.Hits))
	for _, h := range result.Hits.Hits {
		titles = append(titles, h.Source.Title)
	}
	return titles
}

// ── Per-query breakdown ───────────────────────────────────────────────────────

func printPerQueryBreakdown(cases []queryCase, dump, idxDir, esURL, esIndex string) {
	a := analysis.NewAnalyzer()
	var idx index.Index
	var prScores []float64

	if idxDir != "" {
		seg, _ := index.OpenSegment(idxDir)
		idx = seg
		if data, err := os.ReadFile(filepath.Join(idxDir, "pagerank.json")); err == nil {
			json.Unmarshal(data, &prScores)
		}
	} else if dump != "" {
		r, _ := corpus.NewReader(dump)
		mem := index.NewMemoryIndex()
		for {
			doc, ok, _ := r.Next()
			if !ok {
				break
			}
			mem.Add(doc, a)
		}
		mem.Finalize()
		r.Close()
		idx = mem
	} else {
		return
	}

	scorer := rank.NewBM25(idx, 1.2, 0.75)
	base := strings.TrimRight(esURL, "/")

	fmt.Printf("%-30s  %8s  %8s  %8s\n", "query", "my NDCG", "es NDCG", "winner")
	fmt.Println(strings.Repeat("-", 62))

	for _, c := range cases {
		rel := relevanceMap(c.Relevant)

		myTitles := mySearch(idx, a, scorer, prScores, c.Query, 10)
		esTitles := esSearch(base, esIndex, c.Query, 10)

		myNDCG := eval.NDCGAtK(myTitles, rel, 10)
		esNDCG := eval.NDCGAtK(esTitles, rel, 10)

		winner := "tie"
		if myNDCG > esNDCG+0.01 {
			winner = "mine ✓"
		} else if esNDCG > myNDCG+0.01 {
			winner = "es ✓"
		}

		q := c.Query
		if len(q) > 28 {
			q = q[:28]
		}
		fmt.Printf("%-30s  %8.3f  %8.3f  %s\n", q, myNDCG, esNDCG, winner)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func relevanceMap(relevant []string) map[string]bool {
	m := make(map[string]bool, len(relevant))
	for _, r := range relevant {
		m[r] = true
	}
	return m
}
