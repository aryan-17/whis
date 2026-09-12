# wikisearch

Full-text search engine over Simple English Wikipedia (~250k articles).  
BM25 + PageRank ranking, phrase queries, boolean operators, persistent segment index.

Built from scratch in Go — no Lucene, no Bleve, no Solr.

---

## Quick start

```bash
# 1. Slice the dump (10k articles for dev iteration)
head -20000 ../dump/simplewiki_content-20260830-00000.json | gzip > data/sample.json.gz

# 2. Build the inverted index
go run ./cmd/index -dump data/sample.json.gz -index data/index
# indexed 10000 docs in 4.8s
# writing segment to data/index... done
# computing PageRank... done

# 3. Search
go run ./cmd/search -index data/index
```

Or use the pre-built binary:

```bash
./search -index data/index
```

---

## Live session

```
loaded segment (10000 docs)
loaded PageRank from disk
Query syntax: terms  "phrase"  AND OR NOT  field:term  (grouped)
Commands:     open <n>  — show full text of result n

> darwin AND evolution
found 3 documents in 28µs

1. Biology (9.41)
   Darwin wrote the first draft of On the Origin of Species. Earlier,
   Jean-Baptiste Lamarck had offered one of the first full theories of
   evolution...

2. December 27 (3.68)
   1831 – Charles Darwin departs on his voyage aboard HMS Beagle...

3. June 18 (3.23)
   1858 – Charles Darwin and Alfred Russel Wallace jointly announce the
   theory of natural selection...

> world war two
found 182 documents in 187µs

1. Amatex (14.26)
   Amatex is a military explosive... Campbell, John (1985). Naval weapons
   of world war two. London...

2. A. J. P. Taylor (14.05)
   History of World War Two: 1974. "Fritz Fischer and His School," The
   Journal of Modern History...

> title:einstein
found 19 documents in 14µs

1. Atomic Cartoons (10.70)
2. The Honor of Thieves (9.99)
3. Cosmic time (6.37)
   ...Friedmann–Lemaître–Robertson–Walker solutions of Einstein field
   equations...

> mercury OR venus
found 47 documents in 31µs

> open 1
[opens full article text in /tmp/wikisearch_1.txt and prints to terminal]
```

---

## Query syntax

| Syntax | Example | Effect |
|--------|---------|--------|
| Plain terms | `photosynthesis plant` | implicit AND |
| Explicit AND | `darwin AND evolution` | both required |
| OR | `mercury OR venus` | either |
| NOT | `python NOT snake` | exclude term |
| Phrase | `"natural selection"` | exact adjacency |
| Field | `title:darwin` | title field only |
| Grouped | `(mercury OR venus) planet` | precedence |

---

## Architecture

```
corpus/     read + stream JSON dump
analysis/   tokenize → lowercase → stem → stopword filter
index/      MemoryIndex (build) + SegmentIndex (load from disk)
postings/   gap-encoded varint posting lists, galloping intersect
rank/       BM25 scorer, PageRank loader, hybrid blend
query/      boolean parser → AST → evaluator
link/       Wikipedia link-graph → PageRank via power iteration
cmd/        search REPL, index builder, eval harness, ES comparison
```

Segment files written once by `cmd/index`, loaded in milliseconds by `cmd/search`.

```
data/index/
├── segment.dict    ← term directory (offsets into .post)
├── segment.post    ← gap-encoded varint posting lists
├── segment.docs    ← title + doc length per docID
└── pagerank.json   ← PageRank score per docID
```

---

## Flags

```bash
# Tune PageRank influence (default 1.0)
./search -index data/index -pr-weight 0.5   # reduce PR
./search -index data/index -pr-weight 0     # BM25 only

# Rebuild index from dump on every startup (no pre-built index needed)
./search -dump data/sample.json.gz
```

---

## Evaluation

```bash
go run ./cmd/eval -dump data/sample.json.gz
```

Runs 15 hand-judged queries, reports P@10 / MRR / NDCG@10 against relevance judgments in `testdata/queries.json`.

```bash
# SQLite FTS5 baseline for comparison
go run ./cmd/baseline -dump data/sample.json.gz

# Side-by-side vs Elasticsearch (needs Docker)
go run ./cmd/compare -index data/index -dump data/sample.json.gz
```

---

## Tests & benchmarks

```bash
go test ./...
go test ./internal/... -bench=. -benchmem 2>&1 | grep Benchmark
```

Key benchmarks: `BenchmarkBuild`, `BenchmarkLookup`, `BenchmarkIntersect`, `BenchmarkIntersectGallop`.

---

See `INSTRUCTIONS.md` for full setup, `ARCHITECTURE.md` for design decisions, `DEEP_DIVE.md` for a single query traced end-to-end.
