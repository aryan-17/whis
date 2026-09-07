// Command baseline runs the same eval queries against SQLite FTS5.
// Gives a concrete number to beat before optimising.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"wikisearch/internal/corpus"
	"wikisearch/internal/eval"
)

type queryCase struct {
	Query    string   `json:"query"`
	Relevant []string `json:"relevant"`
}

func main() {
	dump := flag.String("dump", "", "path to dump")
	queries := flag.String("queries", "testdata/queries.json", "path to queries.json")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE VIRTUAL TABLE docs USING fts5(title, text)`); err != nil {
		log.Fatal(err)
	}

	// Load corpus into SQLite.
	fmt.Print("loading into sqlite...")
	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	stmt, _ := db.Prepare(`INSERT INTO docs(title, text) VALUES (?, ?)`)
	var count int
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		stmt.Exec(doc.Title, doc.Text)
		count++
	}
	r.Close()
	fmt.Printf(" done (%d docs)\n", count)

	// Load queries.
	data, _ := os.ReadFile(*queries)
	var cases []queryCase
	json.Unmarshal(data, &cases)

	var totalP, totalMRR, totalNDCG float64
	for _, c := range cases {
		rows, err := db.Query(
			`SELECT title FROM docs WHERE docs MATCH ? ORDER BY bm25(docs) LIMIT 10`,
			c.Query,
		)
		if err != nil {
			continue
		}
		var titles []string
		for rows.Next() {
			var t string
			rows.Scan(&t)
			titles = append(titles, t)
		}
		rows.Close()

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
	fmt.Printf("%-20s  %6.3f  %6.3f  %8.3f\n", "sqlite fts5", totalP/n, totalMRR/n, totalNDCG/n)
}
