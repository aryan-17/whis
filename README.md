# wikisearch

A full-text search engine over Simple English Wikipedia, built from scratch in Go. Every layer is hand-rolled — no search libraries, no external indexes. The goal is to understand exactly how search engines work internally.

---

## What it does

- Indexes ~250,000 Wikipedia articles from a CirrusSearch dump
- Ranks results using BM25 (industry-standard term frequency ranking)
- Blends in PageRank computed from the Wikipedia link graph
- Supports phrase queries (`"climate change"`), boolean operators (`AND`, `OR`, `NOT`), and field scoping (`title:darwin`)
- Shows highlighted text snippets for each result
- Persists the index to disk so startup is milliseconds, not minutes
- Loads corpus into Elasticsearch and compares both engines side-by-side on P@10 / MRR / NDCG@10

---

## How it works (quick version)

```
Dump file → Reader → Analyzer → MemoryIndex → segment files (disk)
                                                      ↓
Query → Parser → AST → evalNode() → posting lists → BM25 + PageRank → TopK → Results
```

Full explanation: see [WALKTHROUGH.md](WALKTHROUGH.md)  
Architecture diagrams: see [ARCHITECTURE.md](ARCHITECTURE.md)

---

## Quick start

### 1. Get the corpus

```bash
# Download Simple English Wikipedia CirrusSearch dump (~537MB)
wget -c "https://dumps.wikimedia.org/other/cirrus_search_index/20260830/index_name%3Dsimplewiki_content/simplewiki_content-20260830-00000.json.bz2"

# Or create a dev slice (10,000 articles) to iterate faster
head -20000 simplewiki_content-20260830-00000.json | gzip > wikisearch/data/sample.json.gz
```

### 2. Build the index

```bash
cd wikisearch
go run ./cmd/index -dump data/sample.json.gz -index data/index
```

Output:
```
indexed 10000 docs in 4.2s
writing segment to data/index... done
computing PageRank... done
PageRank written to data/index/pagerank.json
```

### 3. Search

```bash
# Fast startup — loads pre-built index from disk
go run ./cmd/search -index data/index

# Or rebuild from dump each time (slow but no separate index step)
go run ./cmd/search -dump data/sample.json.gz
```

```
Query syntax: terms  "phrase"  AND OR NOT  field:term  (grouped)
> photosynthesis plant
found 47 documents in 1.2ms
1. Photosynthesis (9.2500)
   photosynthesis is a process used by plants to convert light
2. Plant (8.7300)
   a plant is a living thing that uses photosynthesis
...
```

### 4. Measure quality

```bash
# Your engine metrics
go run ./cmd/eval     -dump data/sample.json.gz

# SQLite FTS5 baseline
go run ./cmd/baseline -dump data/sample.json.gz
```

### 5. Compare against Elasticsearch

```bash
# Start ES (one-time)
docker run -d --name es -p 9200:9200 \
  -e "discovery.type=single-node" \
  -e "xpack.security.enabled=false" \
  docker.elastic.co/elasticsearch/elasticsearch:8.14.0

# Load corpus into ES (one-time)
go run ./cmd/esload -dump data/sample.json.gz

# Side-by-side comparison (my engine vs ES)
go run ./cmd/compare -index data/index -dump data/sample.json.gz
```

```
ranker                   P@10     MRR  NDCG@10
--------------------------------------------------
mine (bm25+pagerank)    0.xxx   0.xxx    0.xxx
elasticsearch           0.xxx   0.xxx    0.xxx

query                            my NDCG   es NDCG  winner
--------------------------------------------------------------
photosynthesis                     0.850     0.910  es ✓
world war two                      0.720     0.680  mine ✓
mercury                            0.450     0.600  es ✓
...
```

`cmd/compare` works without ES — prints your metrics and a message if ES is unreachable.

---

## Query syntax

| Syntax | Example | Meaning |
|--------|---------|---------|
| Plain terms | `photosynthesis plant` | AND (implicit) — docs must contain both |
| Explicit AND | `darwin AND evolution` | Same as plain terms |
| OR | `mercury planet OR element` | Either term |
| NOT | `python NOT snake` | Exclude docs with "snake" |
| Phrase | `"climate change"` | Words must be adjacent, in order |
| Field | `title:darwin` | Match in title field only |
| Grouped | `(mercury OR venus) planet` | Precedence grouping |
| Combined | `"natural selection" AND darwin NOT religion` | Mix freely |

---

## How the pipeline works

### 1. Reading the corpus

The Wikipedia dump is line-delimited JSON in pairs:

```
Line 1 (odd)  → {"index":{"_id":"12345"}}          ← action line, skip
Line 2 (even) → {"title":"Photosynthesis","text":"..."} ← document, decode
```

`Reader` streams documents one at a time — never loads the 537MB file into memory. Assigns sequential IDs starting at 0.

### 2. Analyzer pipeline

Every piece of text — both at index-time and query-time — goes through the same 5 steps:

```
"The Running Dogs"
  → Tokenize:  [The@0, Running@1, Dogs@2]
  → Lowercase: [the@0, running@1, dogs@2]
  → Normalize: NFC unicode normalization
  → Stopword:  [running@1, dogs@2]   ← "the" dropped, position gap kept
  → Stem:      [run@1, dog@2]        ← Porter2 stemming
```

The pipeline is identical at index-time and query-time. This is the key invariant: user types `"dogs"` → analyzed to `"dog"` → matches index entry `"dog"`. Any divergence causes silent search misses.

Position gaps are preserved when stopwords are dropped — this matters for phrase queries (Sprint 6).

### 3. Inverted index

Maps every term to the list of documents containing it:

```
"run"  → [{doc0, tf=2, pos=[1,5]}, {doc1, tf=1, pos=[3]}]
"dog"  → [{doc0, tf=1, pos=[2]},   {doc2, tf=3, pos=[0,4,7]}]
```

`tf` = how many times the term appears. `pos` = positions (for phrase queries). Lists are always sorted by docID — this invariant enables all the set operations below.

### 4. Disk persistence (segment format)

Three files written by `cmd/index`, loaded by `cmd/search`:

```
data/index/
├── segment.dict   ← sorted term dictionary with byte offsets into .post
├── segment.post   ← gap-encoded varint posting lists (docID deltas + termFreq)
├── segment.docs   ← JSON lines: one {title, length} per document
└── pagerank.json  ← PageRank scores (one float64 per docID)
```

DocIDs are gap-encoded (store `docID - prevDocID`, not the raw docID). Sorted docIDs produce small gaps → varint encodes small numbers in 1 byte → ~4× smaller than fixed uint32.

Lookup: `dict["photosynthesi"]` → `{offset:9, length:7}` → slice `postData[9:16]` → decode 7 bytes → posting list. O(1), no disk I/O after load.

### 5. Query evaluation

Query string → lexer → recursive descent parser → AST:

```
"climate change" AND ocean NOT pollution

AndNode
├── PhraseNode["climate","change"]  ← both terms must be adjacent
├── TermNode["ocean"]               ← must appear anywhere
└── NotNode[TermNode["pollution"]]  ← must not appear
```

AST is evaluated recursively. Each node type calls a different posting list operation:

| Node | Operation | Algorithm |
|------|-----------|-----------|
| `AndNode` | Intersect | Two-pointer merge O(n+m) |
| `OrNode` | Union | Two-pointer merge O(n+m) |
| `NotNode` | Difference | Two-pointer merge O(n+m) |
| `PhraseNode` | PhraseIntersect | Position gap check O(p+q) per doc |
| `TermNode` | Lookup | Hash map O(1) |

For sparse lists, `IntersectGallop` uses exponential search (probe 1, 2, 4, 8, ... positions) then binary search — **22× faster** than linear scan when lists have large gaps.

### 6. Ranking

**BM25** scores each document for each query term and sums:

```
idf(term)   = log(1 + (N - df + 0.5) / (df + 0.5))
score(term) = idf × tf×(k1+1) / (tf + k1×(1 - b + b×docLen/avgdl))

k1=1.2: TF saturation — 10th occurrence adds less than 3rd
b=0.75: length normalisation — long docs penalised
```

**PageRank** adds a query-independent authority signal. Computed by power iteration over the Wikipedia link graph (d=0.85, 30 iterations):

```
PR(p) = (1-d)/N + d × Σ PR(q)/outdeg(q)   for all q linking to p
```

**Final score:**
```
score = BM25 + w × log(1 + PageRank)
```

`log(1+x)` compresses the wide PageRank distribution so it doesn't overpower BM25.

**Top-k selection:** min-heap of size k. O(n log k) — faster than sorting all results.

### 7. Snippets

Slides a word-count window over the document text, picks the passage with the highest query-term density, then highlights matches with ANSI bold (`\033[1m...\033[0m`).

---

## Project structure

```
wikisearch/
├── cmd/
│   ├── index/       build index from dump, write segment + pagerank.json
│   ├── search/      interactive REPL (loads segment or rebuilds from dump)
│   ├── eval/        P@10 / MRR / NDCG@10 evaluation harness
│   ├── baseline/    SQLite FTS5 comparison (same queries, same metrics)
│   ├── esload/      bulk-load corpus into Elasticsearch
│   └── compare/     side-by-side: my engine vs Elasticsearch
│
├── internal/
│   ├── corpus/      Document type, dump Reader (bzip2/gzip, streaming)
│   ├── analysis/    Token, Tokenizer, filters, Analyzer pipeline
│   ├── postings/    Entry/List types; Intersect/Union/Difference/Phrase/Gallop; varint
│   ├── index/       Index interface; MemoryIndex; segmentIndex + writer
│   ├── rank/        BM25Scorer, TFIDFScorer, TopK heap, Snippet
│   ├── query/       AST nodes, Lexer, recursive-descent Parser
│   ├── link/        Graph, BuildGraph, PageRank power iteration
│   └── eval/        PrecisionAtK, MRR, NDCGAtK
│
├── testdata/
│   └── queries.json 15 queries with relevant title judgments
│
├── ARCHITECTURE.md  package map, dependency rules, invariants
└── WALKTHROUGH.md   step-by-step explanation with worked examples
```

---

## Dependencies

Deliberately minimal — the point is writing this yourself.

| Package | Purpose |
|---------|---------|
| `github.com/kljensen/snowball` | Porter2 stemming |
| `golang.org/x/text/unicode/norm` | NFC Unicode normalisation |
| `github.com/mattn/go-sqlite3` | FTS5 baseline only |

Everything else is stdlib: `compress/bzip2`, `compress/gzip`, `encoding/json`, `encoding/binary`, `container/heap`, `bufio`, `sort`, `math`.

---

## Running tests

```bash
cd wikisearch
go test ./...                          # all tests
go test ./internal/postings/... -v    # specific package
go test ./... -bench=. -benchmem      # benchmarks
```

---

## Flags reference

### cmd/index

| Flag | Default | Description |
|------|---------|-------------|
| `-dump` | required | path to dump (.json.gz or .json.bz2) |
| `-index` | `data/index` | directory to write segment files |

### cmd/search

| Flag | Default | Description |
|------|---------|-------------|
| `-dump` | — | path to dump (required if -index not given) |
| `-index` | — | pre-built segment directory (fast startup) |
| `-pr-weight` | `1.0` | PageRank blend weight |

### cmd/eval / cmd/baseline

| Flag | Default | Description |
|------|---------|-------------|
| `-dump` | required | path to dump |
| `-queries` | `testdata/queries.json` | path to relevance judgments |

### cmd/esload

| Flag | Default | Description |
|------|---------|-------------|
| `-dump` | required | path to dump |
| `-es` | `http://localhost:9200` | Elasticsearch base URL |
| `-index` | `wiki` | ES index name |
| `-batch` | `500` | documents per bulk request |

### cmd/compare

| Flag | Default | Description |
|------|---------|-------------|
| `-dump` | — | path to dump (required if -index not given) |
| `-index` | — | pre-built segment directory (fast startup) |
| `-queries` | `testdata/queries.json` | path to relevance judgments |
| `-es` | `http://localhost:9200` | Elasticsearch base URL |
| `-es-index` | `wiki` | ES index name |
