# Search Engine — Sprint Tracker

**Project:** wikisearch (Go search engine over Simple English Wikipedia)  
**Plan:** `docs/superpowers/plans/2026-09-03-search-engine.md`  
**Corpus:** `simplewiki_content-20260830-00000.json.bz2` (537MB, ~250k articles)

---

## Progress

| Sprint | Goal | Status |
|--------|------|--------|
| 1 | Project skeleton + dump reader | ⬜ Not started |
| 2 | Analyzer pipeline | ⬜ Not started |
| 3 | In-memory index + boolean search + REPL | ⬜ Not started |
| 4 | Ranking (BM25) + evaluation harness | ⬜ Not started |
| 5 | Disk persistence (segment format) | ⬜ Not started |
| 6 | Phrase queries + query parser + snippets | ⬜ Not started |
| 7 | PageRank | ⬜ Not started |
| 8 | Performance (benchmarks, WAND, skip pointers) | ⬜ Not started |
| 9 | Elasticsearch comparison | ⬜ Not started |

---

## Sprint 1 — Project Setup + Dump Reader

**Done when:** `go build ./...` passes, reader prints doc count + first title from sample.

### Task 1: Directory skeleton + core types

- [ ] Create directory structure (`cmd/`, `internal/`, `testdata/`, `data/`)
- [ ] `go mod init wikisearch`
- [ ] Write `internal/corpus/document.go` — `Document` struct
- [ ] Write `internal/index/index.go` — `Index` interface + `PostingList`/`PostingEntry` types
- [ ] `go build ./...` passes

### Task 2: Dump reader

- [ ] Write `internal/corpus/reader_test.go` — tests for count, IDs, field parsing
- [ ] Run test → FAIL
- [ ] Write `internal/corpus/reader.go` — sniff extension, bzip2/gzip, skip odd lines, assign IDs
- [ ] Run test → PASS
- [ ] Write `cmd/index/main.go` — print doc count + first title
- [ ] Create `data/sample.json.gz` (20,000 lines = 10,000 articles)
- [ ] Smoke test: `go run ./cmd/index -dump data/sample.json.gz`
- [ ] Commit: `feat: project skeleton, dump reader`

---

## Sprint 2 — Analyzer Pipeline

**Done when:** `"The Running Dogs"` → `[run(pos=1), dog(pos=2)]` (stopword gap preserved).

### Task 3: Tokenizer

- [ ] Write `internal/analysis/tokenizer_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/analysis/token.go` + `tokenizer.go`
- [ ] Run → PASS
- [ ] Commit: `feat: tokenizer with Unicode support`

### Task 4: Filter chain + Analyzer

- [ ] `go get github.com/kljensen/snowball golang.org/x/text/unicode/norm`
- [ ] Write `internal/analysis/filters_test.go` + `analyzer_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/analysis/filters.go` (lowercase, normalize, stopword, stem)
- [ ] Write `internal/analysis/analyzer.go` — pipeline struct
- [ ] Run → PASS
- [ ] Commit: `feat: analyzer pipeline (lowercase→normalize→stopword→stem)`

---

## Sprint 3 — In-Memory Index + Boolean Search + REPL

**Done when:** REPL returns ranked doc titles for `photosynthesis plant` in <100ms.

### Task 5: In-memory index

- [ ] Write `internal/index/memory_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/index/memory.go` — `MemoryIndex` implementing `Index`
- [ ] Run → PASS
- [ ] Commit: `feat: in-memory inverted index`

### Task 6: Posting list ops

- [ ] Write `internal/postings/ops_test.go` (Intersect, Union, Difference)
- [ ] Run → FAIL
- [ ] Write `internal/postings/ops.go` — two-pointer merge
- [ ] Run → PASS
- [ ] Commit: `feat: posting list ops (AND/OR/NOT)`

### Task 7: Index builder + search REPL

- [ ] Update `cmd/index/main.go` to build full index
- [ ] Write `cmd/search/main.go` — AND terms, print top 10 titles
- [ ] Smoke test on sample
- [ ] Commit: `feat: boolean search REPL — Phase 1 complete`

---

## Sprint 4 — Ranking + Evaluation

**Done when:** `cmd/eval` prints P@10/MRR/NDCG table for mine vs SQLite FTS5.

### Task 8: TF-IDF + BM25 scorers

- [ ] Write `internal/rank/rank_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/rank/scorer.go`, `tfidf.go`, `bm25.go`
- [ ] Run → PASS
- [ ] Commit: `feat: TF-IDF and BM25 scorers`

### Task 9: Top-k heap + ranked REPL

- [ ] Write `internal/rank/topk.go` — `container/heap` min-heap
- [ ] Update `cmd/search/main.go` to use BM25 + top-k
- [ ] Smoke test
- [ ] Commit: `feat: top-k heap, BM25 ranking`

### Task 10: Evaluation harness + SQLite baseline

- [ ] Write `testdata/queries.json` (15 queries with relevant titles)
- [ ] Write `internal/eval/metrics_test.go` (P@k, MRR, NDCG)
- [ ] Run → FAIL
- [ ] Write `internal/eval/metrics.go`
- [ ] Run → PASS
- [ ] Write `cmd/eval/main.go`
- [ ] `go get github.com/mattn/go-sqlite3`
- [ ] Write `cmd/baseline/main.go` — SQLite FTS5
- [ ] Run both, compare numbers
- [ ] Commit: `feat: eval harness (P@k, MRR, NDCG), SQLite baseline`

---

## Sprint 5 — Disk Persistence

**Done when:** startup is milliseconds, eval scores identical to in-memory, bytes-per-posting measured.

### Task 11: Varint codec

- [ ] Write `internal/postings/varint_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/postings/varint.go` — `AppendUvarint` / `ReadUvarint`
- [ ] Run → PASS
- [ ] Commit: `feat: varint codec`

### Task 12: Segment writer + reader

- [ ] Write `internal/index/segment_test.go` — roundtrip test
- [ ] Run → FAIL
- [ ] Write `internal/index/writer.go` — serialize to `segment.dict` + `segment.post` + `segment.docs`
- [ ] Write `internal/index/segment.go` — deserialize, satisfy `Index`
- [ ] Run → PASS
- [ ] Update `cmd/index` to write segment, `cmd/search` to load it
- [ ] Re-run eval: NDCG must match in-memory exactly
- [ ] Commit: `feat: segment writer/reader, persistent index`

---

## Sprint 6 — Phrase Queries + Query Parser + Snippets

**Done when:** `"climate change"` outranks `climate AND change`; field queries work; snippets render.

### Task 13: Positional index + phrase intersection

- [ ] Write `internal/postings/phrase_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/postings/phrase.go` — `PhraseIntersect`
- [ ] Update `MemoryIndex.Add` to store positions
- [ ] Run → PASS
- [ ] Commit: `feat: positional index, phrase intersection`

### Task 14: Query parser

- [ ] Write `internal/query/parser_test.go` (AND, OR, NOT, phrase, field)
- [ ] Run → FAIL
- [ ] Write `internal/query/ast.go` — AST node types
- [ ] Write `internal/query/lexer.go`
- [ ] Write `internal/query/parser.go` — recursive descent
- [ ] Run → PASS
- [ ] Commit: `feat: query lexer + recursive-descent parser`

### Task 15: Snippet generation

- [ ] Write `internal/rank/snippet_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/rank/snippet.go` — density window + ANSI highlight
- [ ] Run → PASS
- [ ] Commit: `feat: snippet generation with term highlighting`

---

## Sprint 7 — PageRank

**Done when:** PageRank effect on NDCG is measured (positive or negative, either is valid).

### Task 16: Link graph + PageRank

- [ ] Write `internal/link/pagerank_test.go`
- [ ] Run → FAIL
- [ ] Write `internal/link/pagerank.go` — power iteration, dangling node handling
- [ ] Run → PASS
- [ ] Build graph from corpus `outgoing_link` field
- [ ] Blend into BM25: `final = bm25 + w * log(1 + pagerank)`
- [ ] Tune `w` via eval harness, not by eye
- [ ] Commit: `feat: PageRank, blend into BM25`

---

## Sprint 8 — Performance

**Done when:** before/after latency distribution shown, WAND skip reason explained.

### Task 17: Benchmarks + profiling baseline

- [ ] Write `internal/index/bench_test.go` + `internal/postings/bench_test.go`
- [ ] Run `go test ./internal/... -bench=. -benchmem > bench_baseline.txt`
- [ ] Profile: `go test -bench=BenchmarkBuild -cpuprofile=cpu.out`
- [ ] Open `go tool pprof -http=:8080 cpu.out`, note top 3 hot spots
- [ ] Commit: `perf: add benchmarks, record baseline`

### Task 18: Skip pointers + WAND

- [ ] Add galloping search to `Intersect` in `ops.go`
- [ ] Write `internal/rank/wand.go` — weak AND early termination
- [ ] Verify WAND output matches TopK on small inputs
- [ ] Re-run benchmarks, compare to baseline
- [ ] Commit: `perf: skip pointers (galloping), WAND early termination`

---

## Sprint 9 — Elasticsearch Comparison

**Done when:** comparison table printed, gaps between engines explained by specific techniques.

### Task 19: ES load + verify + compare

- [ ] Start ES: `docker run -p 9200:9200 -e "discovery.type=single-node" elasticsearch:8.x`
- [ ] Create index with `english` analyzer, `refresh_interval: -1`
- [ ] Write `cmd/esload/main.go` — bulk NDJSON loader (batches of 500)
- [ ] Load corpus
- [ ] Verify analyzer: `curl localhost:9200/wiki/_analyze` vs your analyzer on same input
- [ ] Verify scores: `curl localhost:9200/wiki/_explain/<id>` vs your BM25
- [ ] Write `cmd/eseval/main.go` — query ES, compute same metrics
- [ ] Print comparison table
- [ ] Write 1-paragraph explanation of where ES beats your engine and why
- [ ] Commit: `feat: ES loader, eval comparison — Sprint 9 complete`

---

## Key Invariants

> These must hold at every commit. If a test reveals a violation, fix before continuing.

1. **Analyzer symmetry** — index-time and query-time analysis identical
2. **Posting list sorted** — entries always sorted by DocID ascending
3. **Position gaps preserved** — stopword drop leaves positional gap, no renumbering
4. **Eval score stability** — after Sprint 5, NDCG must match in-memory exactly
5. **Even-line rule** — dump slices always use even line counts

---

## Learnings Log

> Fill in after each sprint. One sentence per key insight.

| Sprint | Key insight |
|--------|-------------|
| 1 | |
| 2 | |
| 3 | |
| 4 | |
| 5 | |
| 6 | |
| 7 | |
| 8 | |
| 9 | |
