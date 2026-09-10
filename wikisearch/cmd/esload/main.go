// Command esload bulk-loads a CirrusSearch dump into Elasticsearch.
//
// Usage:
//
//	go run ./cmd/esload -dump data/sample.json.gz -es http://localhost:9200
//
// Start ES first:
//
//	docker run -d --name es -p 9200:9200 \
//	  -e "discovery.type=single-node" \
//	  -e "xpack.security.enabled=false" \
//	  docker.elastic.co/elasticsearch/elasticsearch:8.14.0
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"wikisearch/internal/corpus"
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	esURL := flag.String("es", "http://localhost:9200", "Elasticsearch base URL")
	index := flag.String("index", "wiki", "ES index name")
	batch := flag.Int("batch", 500, "documents per bulk request")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	base := strings.TrimRight(*esURL, "/")

	// Delete and recreate index with english analyzer.
	deleteIndex(base, *index)
	createIndex(base, *index)

	// Bulk load with refresh disabled for speed.
	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	fmt.Printf("loading into %s/%s (batch=%d)...\n", base, *index, *batch)
	start := time.Now()
	var buf bytes.Buffer
	var total, batchNum int

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		batchNum++
		resp, err := http.Post(
			fmt.Sprintf("%s/%s/_bulk", base, *index),
			"application/x-ndjson",
			bytes.NewReader(buf.Bytes()),
		)
		if err != nil {
			log.Fatalf("bulk request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			log.Fatalf("bulk error (status %d): %s", resp.StatusCode, body)
		}
		// Check for per-item errors.
		var result struct {
			Errors bool `json:"errors"`
		}
		json.Unmarshal(body, &result)
		if result.Errors {
			log.Printf("warn: batch %d had item errors", batchNum)
		}
		fmt.Printf("\r  loaded %d docs (batch %d)...", total, batchNum)
		buf.Reset()
	}

	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}

		// Action line.
		fmt.Fprintf(&buf, `{"index":{"_id":"%d"}}`, doc.ID)
		buf.WriteByte('\n')

		// Document line.
		d, _ := json.Marshal(map[string]string{
			"title": doc.Title,
			"text":  doc.Text,
		})
		buf.Write(d)
		buf.WriteByte('\n')

		total++
		if total%*batch == 0 {
			flush()
		}
	}
	flush()

	// Restore refresh interval and force merge.
	setRefresh(base, *index, "1s")
	forceMerge(base, *index)

	fmt.Printf("\ndone: %d docs in %s\n", total, time.Since(start))
	fmt.Printf("index: %s/%s\n", base, *index)
	fmt.Println("\nVerify analyzer:")
	fmt.Printf("  curl -s '%s/%s/_analyze' -H 'Content-Type: application/json' -d '{\"analyzer\":\"english\",\"text\":\"The running dogs\"}' | jq .\n", base, *index)
}

func deleteIndex(base, idx string) {
	req, _ := http.NewRequest(http.MethodDelete, base+"/"+idx, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return // ES not running or index doesn't exist — ok
	}
	resp.Body.Close()
}

func createIndex(base, idx string) {
	mapping := `{
  "settings": {
    "refresh_interval": "-1",
    "number_of_replicas": 0,
    "analysis": {
      "analyzer": {
        "wiki_english": {
          "type": "english"
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "title": { "type": "text", "analyzer": "wiki_english", "boost": 2 },
      "text":  { "type": "text", "analyzer": "wiki_english" }
    }
  }
}`
	resp, err := http.Post(base+"/"+idx, "application/json", strings.NewReader(mapping))
	if err != nil {
		log.Fatalf("create index: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Fatalf("create index failed (status %d): %s", resp.StatusCode, body)
	}
	fmt.Printf("created index %s\n", idx)
}

func setRefresh(base, idx, interval string) {
	body := fmt.Sprintf(`{"index":{"refresh_interval":"%s"}}`, interval)
	req, _ := http.NewRequest(http.MethodPut, base+"/"+idx+"/_settings",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
}

func forceMerge(base, idx string) {
	fmt.Print("force merging...")
	resp, err := http.Post(base+"/"+idx+"/_forcemerge?max_num_segments=1", "", nil)
	if err == nil {
		resp.Body.Close()
	}
	fmt.Println(" done")
}
