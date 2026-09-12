# wikisearch — Running Instructions

Everything you need to go from zero to a working search engine.

---

## Prerequisites

```bash
# Go 1.22+
go version

# SQLite (for baseline only)
# Already handled via go-sqlite3 — needs a C compiler
xcode-select --install   # macOS
```

---

## Directory layout

```
whis/
├── dump/
│   ├── simplewiki_content-20260830-00000.json       ← decompressed dump
│   └── simplewiki_content-20260830-00000.json.bz2   ← compressed dump
└── wikisearch/
    ├── data/
    │   ├── sample.json.gz    ← 10k-article dev slice (you create this)
    │   └── index/            ← pre-built segment files (you create this)
    └── testdata/
        └── queries.json      ← 15 relevance judgments
```

All commands run from `wikisearch/`:

```bash
cd /Users/ctme/Documents/projects/whis/wikisearch
```

---

## Step 1 — Create sample slice

Creates a 10,000-article dev slice for fast iteration. Do NOT test against the full 537MB dump while developing.

```bash
head -20000 ../dump/simplewiki_content-20260830-00000.json \
  | gzip > data/sample.json.gz
```

Verify:
```bash
go run ./cmd/index -dump data/sample.json.gz 2>&1 | grep "indexed"
# Expected: indexed 10000 docs in ~5s
```

---

## Step 2 — Build the index

Reads dump → builds inverted index → writes segment files → computes PageRank.

```bash
go run ./cmd/index -dump data/sample.json.gz -index data/index
```

Expected output:
```
indexed 10000 docs in 4.8s
writing segment to data/index... done
computing PageRank... done
PageRank written to data/index/pagerank.json
```

Files created:
```
data/index/
├── segment.dict      ← term directory with byte offsets
├── segment.post      ← gap-encoded varint posting lists
├── segment.docs      ← title + length per document
└── pagerank.json     ← PageRank score per docID
```

---

## Step 3 — Search (fast path)

Loads pre-built segment from disk. Starts in milliseconds.

```bash
go run ./cmd/search -index data/index
```

Optional flags:
```bash
go run ./cmd/search -index data/index -pr-weight 0.5    # reduce PageRank influence
go run ./cmd/search -index data/index -pr-weight 0      # BM25 only, no PageRank
```

### Search (slow path — no pre-built index)

Rebuilds from dump on every startup. Useful when you haven't run cmd/index yet.

```bash
go run ./cmd/search -dump data/sample.json.gz
```

---

## Step 4 — Query syntax

| Syntax | Example | What it does |
|--------|---------|--------------|
| Plain terms | `photosynthesis plant` | AND (implicit) — both must appear |
| Explicit AND | `darwin AND evolution` | Same as plain |
| OR | `mercury planet OR element` | Either term |
| NOT | `python NOT snake` | Exclude "snake" |
| Phrase | `"climate change"` | Words adjacent in order |
| Field | `title:darwin` | Title field only |
| Grouped | `(mercury OR venus) planet` | Precedence |
| Combined | `"natural selection" AND darwin NOT religion` | Mix freely |

---

## Step 5 — Run tests

```bash
# All tests
go test ./...

# Verbose
go test ./... -v 2>&1 | grep -E "RUN|PASS|FAIL|ok"

# Specific package
go test ./internal/postings/... -v
go test ./internal/analysis/... -v
go test ./internal/index/... -v
```

Expected: all packages `ok`, zero `FAIL`.

---

## Step 6 — Run evaluation

Runs 15 hand-judged queries through your engine. Prints P@10 / MRR / NDCG@10.

```bash
go run ./cmd/eval -dump data/sample.json.gz
```

Custom query file:
```bash
go run ./cmd/eval -dump data/sample.json.gz -queries testdata/queries.json
```

Expected output:
```
building index... done (10000 docs)

ranker                P@10     MRR   NDCG@10
mine (bm25)          0.xxx   0.xxx    0.xxx
```

---

## Step 7 — SQLite FTS5 baseline

Same 15 queries against SQLite FTS5 with built-in `bm25()` ranking. Gives a concrete number to compare against.

```bash
go run ./cmd/baseline -dump data/sample.json.gz
```

Expected:
```
ranker                P@10     MRR   NDCG@10
sqlite fts5          0.xxx   0.xxx    0.xxx
```

Being slightly behind SQLite here is normal — PageRank (Sprint 7) closes the gap.

---

## Step 8 — Benchmarks

Records performance numbers. Always run before and after any optimisation.

```bash
go test ./internal/... -bench=. -benchmem -count=3 2>&1 | grep Benchmark
```

Compare against the saved baseline:
```bash
cat data/bench_baseline.txt
```

Key numbers to watch:
- `BenchmarkBuild` — time to index 5000 docs
- `BenchmarkLookup` — time per posting list lookup
- `BenchmarkIntersect` — two-pointer merge
- `BenchmarkIntersectGallop` — should be ~22× faster on sparse lists

---

## Step 9 — Elasticsearch comparison (optional, needs Docker)

### Start Elasticsearch

```bash
docker run -d --name es \
  -p 9200:9200 \
  -e "discovery.type=single-node" \
  -e "xpack.security.enabled=false" \
  docker.elastic.co/elasticsearch/elasticsearch:8.14.0

# Wait ~30 seconds for ES to start
curl -s localhost:9200/_cluster/health | grep status
# Should show: "status":"green" or "status":"yellow"
```

### Load corpus into ES

```bash
go run ./cmd/esload -dump data/sample.json.gz
```

Options:
```bash
go run ./cmd/esload -dump data/sample.json.gz -es http://localhost:9200 -index wiki -batch 500
```

Expected:
```
created index wiki
  loaded 10000 docs (batch 20)...
force merging... done
done: 10000 docs in ~30s
```

### Compare both engines

```bash
go run ./cmd/compare -index data/index -dump data/sample.json.gz
```

Expected output:
```
my engine: loaded segment (10000 docs)
elasticsearch: connected at http://localhost:9200

ranker                   P@10     MRR  NDCG@10
--------------------------------------------------
mine (bm25+pagerank)    0.xxx   0.xxx    0.xxx
elasticsearch           0.xxx   0.xxx    0.xxx

query                            my NDCG   es NDCG  winner
--------------------------------------------------------------
photosynthesis                     0.850     0.910  es ✓
world war two                      0.720     0.680  mine ✓
...
```

If ES is not running, `cmd/compare` prints your metrics and skips ES gracefully.

### Stop ES when done

```bash
docker stop es && docker rm es
```

---

## Step 10 — Verify analyzer matches ES

```bash
# ES tokenization
curl -s 'localhost:9200/wiki/_analyze' \
  -H 'Content-Type: application/json' \
  -d '{"analyzer":"english","text":"The running dogs jumped quickly"}' | jq .tokens[].token

# Expected: run, dog, jump, quickli (or quick depending on ES version)
# Our output: run, dog, jump, quick
```

### Verify BM25 scores

```bash
# Replace 42 with an actual docID from your index
curl -s 'localhost:9200/wiki/_explain/42' \
  -H 'Content-Type: application/json' \
  -d '{"query":{"match":{"text":"photosynthesis"}}}' | jq .explanation
```

Shows ES's IDF, TF, field length values. Compare against your BM25 calculation.

---

## Full test sequence (copy-paste)

```bash
cd /Users/ctme/Documents/projects/whis/wikisearch

# 1. Create sample
head -20000 ../dump/simplewiki_content-20260830-00000.json | gzip > data/sample.json.gz

# 2. Build index
go run ./cmd/index -dump data/sample.json.gz -index data/index

# 3. Run all tests
go test ./...

# 4. Interactive search
go run ./cmd/search -index data/index

# 5. Eval metrics
go run ./cmd/eval -dump data/sample.json.gz

# 6. SQLite baseline
go run ./cmd/baseline -dump data/sample.json.gz

# 7. Benchmarks
go test ./internal/... -bench=. -benchmem 2>&1 | grep Benchmark
```

---

## Troubleshooting

**`go: cannot find module`**
```bash
cd wikisearch && go mod tidy
```

**`cgo: C compiler not found`** (SQLite baseline)
```bash
xcode-select --install   # macOS
# or skip baseline: just use cmd/eval
```

**`scanner: token too long`**
Scanner buffer too small. Already set to 10MB — should not happen on simplewiki.

**ES `connection refused`**
ES hasn't started yet. Wait 30s and retry `curl localhost:9200`.

**Eval scores all 0.000**
`data/sample.json.gz` may not contain articles matching the query titles in `testdata/queries.json`. Use the full dump or a larger slice (`head -500000`).

**`open data/index/segment.dict: no such file`**
Run `cmd/index` first to build the segment before using `cmd/search -index`.
