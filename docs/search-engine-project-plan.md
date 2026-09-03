# Building a Search Engine in Go

A terminal search engine over Simple English Wikipedia, built from scratch,
then rebuilt on Elasticsearch for comparison.

**Goal:** understand every layer of a search engine — not just how to use one.

**Language:** Go
**Corpus:** `simplewiki_content-20260830-00000.json.bz2` (537MB compressed, ~250k articles)
**Approach:** hand-rolled first, Elasticsearch second

---

## Table of contents

1. [Topics covered](#topics-covered)
2. [Corpus](#corpus)
3. [Project layout](#project-layout)
4. [Core types](#core-types)
5. [Phase 0 — Setup](#phase-0--setup)
6. [Phase 1 — Minimum viable search](#phase-1--minimum-viable-search)
7. [Phase 2 — Ranking and measurement](#phase-2--ranking-and-measurement)
8. [Phase 3 — Persistence](#phase-3--persistence)
9. [Phase 4 — Richer retrieval](#phase-4--richer-retrieval)
10. [Phase 5 — Performance](#phase-5--performance)
11. [Phase 6 — Elasticsearch](#phase-6--elasticsearch)
12. [Dependencies](#dependencies)
13. [Glossary](#glossary)
14. [Further reading](#further-reading)

---

## Topics covered

### Corpus and ingestion
- Wikimedia dump formats and how they differ (`pages-articles`, `abstracts`, `pagelinks`, CirrusSearch)
- Line-delimited JSON, OpenSearch bulk-insert format (alternating action/document lines)
- Streaming decompression (bzip2, gzip) without loading the file into memory
- Memory-bounded batch processing
- Internal document IDs vs. external identifiers
- The forward store: docID → title, URL, metadata

### Text analysis
- Tokenization: whitespace, punctuation, the awkward cases (`don't`, `C++`, `U.S.A.`)
- Unicode normalization (NFC/NFKC) and case folding
- Stopword removal and what it costs you (phrase queries break)
- Stemming (Porter/Snowball) vs. lemmatization
- Analyzer pipelines as composable filters
- **The invariant:** index-time and query-time analysis must be identical

### Index construction
- The inverted index: term → posting list
- Term dictionary vs. postings file
- In-memory construction and why it stops scaling
- SPIMI (Single-Pass In-Memory Indexing): build blocks, spill to disk, k-way merge
- External sorting
- Segments as immutable units

### Compression and storage
- Delta (gap) encoding on sorted doc IDs
- Variable-byte integers / uvarint
- Front-coded term dictionaries
- Sparse offset indexes (store every Nth term, scan the rest)
- `mmap` and letting the OS page cache do the work
- Measuring bytes-per-posting; the size/speed tradeoff

### Query processing
- Boolean retrieval: AND, OR, NOT
- Sorted-list intersection; the two-pointer merge
- Skip pointers and galloping (exponential) search
- Recursive-descent parsing into an AST
- Field-scoped queries (`title:foo`), quoted phrases, grouping

### Ranking
- Term frequency, document frequency, inverse document frequency
- TF-IDF and the vector space model
- BM25: term frequency saturation (`k1`), length normalization (`b`)
- Why BM25 beats TF-IDF in practice
- Query-independent signals and score blending

### Top-k and performance
- Heap-based top-k selection
- WAND and block-max WAND early termination
- Benchmarking with `testing.B`
- CPU and allocation profiling with `pprof`
- Reducing allocations in hot loops

### Phrase and proximity
- Positional postings
- Phrase matching by position intersection
- Proximity scoring (terms near each other rank higher)
- The index size cost — expect roughly 2x

### Link analysis
- Building a link graph from `outgoing_link`
- PageRank by power iteration
- Damping factor, dangling nodes, convergence
- Blending a query-independent prior into a query-dependent score

### Evaluation
- Relevance judgments (qrels)
- Precision@k, Recall, MRR, NDCG
- A/B comparing two rankers on the same query set
- Overfitting to a small query set — the trap

### Query-side features
- Snippet generation and term highlighting
- Spelling correction: edit distance over a term bigram index
- Prefix autocomplete
- Query result caching

### Incremental updates
- Segment-based writes
- Merge policy (when to combine segments)
- Deletes as tombstones
- Visibility and refresh semantics

### Go engineering
- Interface design for swappable implementations
- Binary formats with `encoding/binary`
- Table-driven tests
- Benchmarks and profiling as routine, not special occasions

### Elasticsearch (Phase 6)
- Index mappings and field types
- Analyzer configuration
- Query DSL: `match`, `bool`, `multi_match`, `function_score`
- `_analyze` and `_explain` as verification tools
- Bulk API, `refresh_interval`, merge pressure
- Shards, replicas, and when they matter

---

## Corpus

### Source

Wikimedia CirrusSearch dumps — the content Wikimedia feeds their own production
OpenSearch cluster. Markup is already stripped, so no wikitext parser needed.

```
https://dumps.wikimedia.org/other/cirrus_search_index/20260830/
  index_name%3Dsimplewiki_content/
    _SUCCESS
    simplewiki_content-20260830-00000.json.bz2   (537MB)
```

`_SUCCESS` confirms the run completed. The `%3D` is an encoded `=` — these are
Hive-style partition directories.

```bash
DIR=https://dumps.wikimedia.org/other/cirrus_search_index/20260830
wget -c "$DIR/index_name%3Dsimplewiki_content/simplewiki_content-20260830-00000.json.bz2"
```

### Format

Line-delimited JSON in **pairs**. An action line, then a document line:

```json
{"index":{"_type":"page","_id":"12345"}}
{"title":"Photosynthesis","text":"Photosynthesis is a process...","outgoing_link":["Plant","Chlorophyll"],...}
```

Your reader takes every second line. Any slicing of this file must use an
**even** line count or you'll cut a pair in half.

### Fields of interest

| Field | Use |
|---|---|
| `title` | display, and a boosted search field |
| `text` | the main body — primary index field |
| `opening_text` | good default snippet |
| `heading` | optional secondary field |
| `category` | faceting, filtering |
| `outgoing_link` | link graph for PageRank (Phase 4) |
| `incoming_links` | a ready-made popularity signal |
| `popularity_score` | pageview-derived prior |

**Verify these against the actual file before coding.** The format has changed
between dump versions:

```bash
bzcat simplewiki_content-20260830-00000.json.bz2 | head -4 | jq .
```

### Development slice

Do not iterate against 537MB while debugging a line reader.

```bash
# 10,000 articles (20,000 lines — must be even)
bzcat simplewiki_content-20260830-00000.json.bz2 | head -20000 | gzip > data/sample.json.gz
```

Optionally recompress the full file to gzip. Go's `compress/bzip2` is
decompress-only and several times slower than gzip; if you re-read the corpus
often it's worth the one-time cost:

```bash
bzcat simplewiki_content-20260830-00000.json.bz2 | gzip > data/simplewiki.json.gz
```

Support both in your reader by sniffing the file extension.

---

## Project layout

```
wikisearch/
├── cmd/
│   ├── index/       build an index from a dump
│   ├── search/      query REPL
│   ├── eval/        run the evaluation harness
│   └── baseline/    SQLite FTS5 comparison
├── internal/
│   ├── corpus/      dump reader, Document type
│   ├── analysis/    tokenizer, filters, analyzer pipeline
│   ├── postings/    Posting, PostingList, varint codec, list ops
│   ├── index/       builder, segment writer/reader, Index interface
│   ├── store/       docID → title/URL/length
│   ├── query/       lexer, parser, AST
│   ├── rank/        BM25, top-k, score blending
│   ├── link/        graph construction, PageRank
│   └── eval/        metrics: P@k, MRR, NDCG
├── testdata/
│   └── queries.json relevance judgments
├── data/            dumps and built indexes (gitignored)
└── go.mod
```

```bash
mkdir -p wikisearch/{cmd/{index,search,eval,baseline},internal/{corpus,analysis,postings,index,store,query,rank,link,eval},testdata,data}
cd wikisearch && go mod init wikisearch
```

---

## Core types

Define these early. The `Index` interface is the important one — your in-memory
and on-disk implementations both satisfy it, so `cmd/search` never changes when
you swap them in Phase 3.

```go
// internal/corpus
type Document struct {
    ID     uint32   // internal, sequential
    Title  string
    Text   string
    Links  []string
    Length uint32   // token count, needed for BM25
}

// internal/analysis
type Token struct {
    Term     string
    Position int
}

type Analyzer interface {
    Analyze(text string) []Token
}

type Filter interface {
    Filter(tokens []Token) []Token
}

// internal/postings
type Posting struct {
    DocID     uint32
    TermFreq  uint32
    Positions []uint32 // Phase 4; nil before then
}

type PostingList struct {
    Term     string
    DocFreq  uint32
    Postings []Posting // sorted by DocID — invariant
}

// internal/index
type Index interface {
    Lookup(term string) (PostingList, bool)
    NumDocs() uint32
    AvgDocLen() float64
    DocLen(id uint32) uint32
    Doc(id uint32) (Document, error)
}

// internal/rank
type Scorer interface {
    Score(p Posting, docFreq uint32, docLen uint32) float64
}

type Result struct {
    DocID uint32
    Score float64
}
```

---

## Phase 0 — Setup

**~1 evening**

### Steps

1. Download the dump (see [Corpus](#corpus)).
2. Inspect it: `bzcat ... | head -4 | jq .` — confirm the field names against
   the table above and note any differences.
3. Create the directory skeleton and `go mod init`.
4. Carve off `data/sample.json.gz` (even line count).
5. Write `internal/corpus/document.go` with the `Document` type.
6. Write `internal/index/index.go` with the `Index` interface.

### Done when

`go build ./...` passes, and you can articulate what's in a dump record.

---

## Phase 1 — Minimum viable search

**~3–5 evenings**

The goal is a working boolean search engine. Quality doesn't matter yet.

### 1.1 Dump reader

`internal/corpus/reader.go`

- Open file, sniff extension, wrap in `bzip2.NewReader` or `gzip.NewReader`
- `bufio.Scanner` over that — **increase the buffer**, some articles blow past
  the 64KB default:
  ```go
  sc := bufio.NewScanner(r)
  sc.Buffer(make([]byte, 1024*1024), 10*1024*1024)
  ```
- Skip odd lines (action lines), decode even lines into `Document`
- Assign sequential IDs
- Expose it as an iterator or a channel, not a `[]Document` — you want this to
  stream from the start

**Checkpoint:** `cmd/index` prints the document count and the first title.

### 1.2 Analyzer

`internal/analysis/`

Build as a pipeline: tokenizer, then an ordered list of filters.

- **Tokenizer** — split on non-letter/non-digit runs. Handle Unicode via
  `unicode.IsLetter`, not ASCII ranges. Record positions.
- **Lowercase filter** — `strings.ToLower`
- **Normalize filter** — NFC via `golang.org/x/text/unicode/norm`
- **Stopword filter** — a `map[string]struct{}` of ~50 common words
- **Stem filter** — `github.com/kljensen/snowball/english`

Keep positions correct through the pipeline. Dropping a stopword should leave a
positional gap, not renumber — otherwise phrase queries break in Phase 4.

**Checkpoint:** table-driven tests. `"The Running Dogs"` → `[running(1), dog(2)]`.

### 1.3 In-memory index

`internal/index/memory.go`

- `map[string][]Posting`
- Build: for each document, analyze, count term frequencies, append postings
- Postings naturally arrive in docID order — assert it
- Compute and store doc lengths and average doc length
- Implement the `Index` interface

**Checkpoint:** index the sample, print vocabulary size and total postings.

### 1.4 Boolean queries

`internal/postings/ops.go`

- `Intersect(a, b PostingList) PostingList` — two-pointer merge
- `Union(a, b PostingList) PostingList`
- `Difference(a, b PostingList) PostingList`
- Intersect shortest-first when combining more than two lists

### 1.5 REPL

`cmd/search/main.go`

- Read a line, analyze it with the *same* analyzer, AND the terms together
- Print the first 10 titles
- No ranking yet — results are in docID order

### Done when

```
> photosynthesis plant
1. Photosynthesis
2. Leaf
3. Chlorophyll
...
found 47 documents in 12ms
```

**You now have a search engine.** Everything after this is making it good.

---

## Phase 2 — Ranking and measurement

**~3–4 evenings**

The most important phase. After this, every change is measurable.

### 2.1 TF-IDF

`internal/rank/tfidf.go`

```
idf(t)   = log(N / df(t))
tf(t,d)  = 1 + log(freq(t,d))
score    = Σ tf(t,d) × idf(t)
```

Implement it, look at the results, notice they're mediocre. That's the point —
you need the baseline to appreciate BM25.

### 2.2 BM25

`internal/rank/bm25.go`

```
idf(t) = log(1 + (N - df + 0.5) / (df + 0.5))

score(t,d) = idf(t) × ( f(t,d) × (k1+1) ) /
             ( f(t,d) + k1 × (1 - b + b × |d|/avgdl) )
```

Defaults: `k1 = 1.2`, `b = 0.75`.

Understand what each does before tuning:
- **`k1`** controls saturation. Higher = term frequency keeps mattering. Lower =
  the 10th occurrence counts barely more than the 3rd.
- **`b`** controls length normalization. `b=0` ignores document length entirely;
  `b=1` fully normalizes. Long Wikipedia articles will dominate at `b=0`.

### 2.3 Top-k

`internal/rank/topk.go`

`container/heap` with a min-heap of size k. Push, and pop the minimum when the
heap exceeds k. Avoids sorting the full result set.

### 2.4 Evaluation harness

`testdata/queries.json`:

```json
[
  {
    "query": "photosynthesis",
    "relevant": ["Photosynthesis", "Plant", "Chlorophyll", "Leaf"]
  },
  {
    "query": "world war two",
    "relevant": ["World War II", "Adolf Hitler", "Nazi Germany"]
  },
  {
    "query": "mercury",
    "relevant": ["Mercury (planet)", "Mercury (element)"]
  }
]
```

Write 15–20. Deliberately mix:
- **Easy single terms** — `photosynthesis`. Should always work.
- **Multi-word** — `world war two`. Tests term combination.
- **Ambiguous** — `mercury`, `java`, `python`. These are the interesting ones:
  matching is trivial, *ranking* is the whole problem. They're what will tell
  you whether BM25 and PageRank are earning their keep.

`internal/eval/metrics.go`:

- **P@10** — of the top 10, how many were relevant
- **MRR** — 1 / rank of the first relevant result, averaged
- **NDCG@10** — rank-weighted, the standard IR metric

`cmd/eval` runs all queries and prints a table.

### 2.5 SQLite baseline

`cmd/baseline/main.go`

```sql
CREATE VIRTUAL TABLE docs USING fts5(title, text);
SELECT title FROM docs WHERE docs MATCH ? ORDER BY bm25(docs) LIMIT 10;
```

~20 lines. Run the same eval against it. Now you have a number to beat.

### Done when

```
$ go run ./cmd/eval
             P@10    MRR    NDCG@10
mine (bm25)  0.62    0.78     0.71
sqlite fts5  0.68    0.81     0.75
```

Being slightly behind SQLite here is normal and fine.

---

## Phase 3 — Persistence

**~5–8 evenings. The hard one.**

This is where hobby search engines die. You have a working in-memory version and
the disk rewrite is tedious. Push through — it's also where the most real
learning is.

### 3.1 Segment format

Three files per segment:

```
segment.dict    term dictionary → offsets into .post
segment.post    posting lists, compressed
segment.docs    docID → title, URL, length
```

**Postings encoding**, per list:

```
[uvarint docFreq]
[uvarint gap(docID)] [uvarint termFreq]    × docFreq
```

Gaps because sorted doc IDs delta to small numbers, and uvarint stores small
numbers in one byte. Expect roughly 4x smaller than fixed-width `uint32`.

**Dictionary encoding:**

```
[uvarint sharedPrefixLen] [uvarint suffixLen] [suffix bytes]
[uvarint docFreq] [uvarint postingsOffset] [uvarint postingsLen]
```

Terms sorted, front-coded against the previous term. Keep every 32nd term's
file offset in a sparse in-memory slice: binary search that, then linear scan
up to 32 entries on disk. The full dictionary never enters the heap.

### 3.2 Writer

`internal/index/writer.go`

Sort terms, write postings sequentially recording offsets, then write the
dictionary with those offsets. Use `encoding/binary.PutUvarint`.

### 3.3 Reader

`internal/index/segment.go`

`mmap` the postings and dictionary files (`golang.org/x/exp/mmap`, or
`syscall.Mmap` directly). Decode lazily — only touch the bytes for terms the
query actually needs. The OS page cache handles the rest.

**Must satisfy the same `Index` interface.** `cmd/search` should not change.

**Checkpoint:** re-run eval. Scores must be *identical* to the in-memory
version. Any difference is a codec bug.

### 3.4 SPIMI

`internal/index/spimi.go`

Right now you hold the whole index in RAM while building. That's fine for
simplewiki and fatal for enwiki. Fix it:

1. Build in memory until a size threshold
2. Sort terms, flush to a numbered segment on disk, free the memory
3. Repeat to end of corpus
4. K-way merge the segments (`container/heap` over segment iterators) into one

This is exactly how Lucene builds indexes, and it means you can point the same
binary at full enwiki later without a rewrite.

### Done when

- Startup is milliseconds instead of a rebuild
- Eval scores unchanged
- Peak build memory is bounded regardless of corpus size
- You can state your bytes-per-posting

---

## Phase 4 — Richer retrieval

**~4–6 evenings**

### 4.1 Positional index

Add `Positions []uint32` to the encoded posting:

```
[uvarint gap(docID)] [uvarint termFreq] [uvarint gap(position)] × termFreq
```

Positions delta-encoded within a document.

**Watch the index roughly double.** Understand why: you've gone from one entry
per (term, document) to one per (term, occurrence).

### 4.2 Phrase queries

`"climate change"` — intersect the documents, then for each candidate check
whether `pos(change) == pos(climate) + 1` for any position pair. Two-pointer
over the position lists.

Proximity scoring: score by the minimum window containing all query terms.
Closer = better.

### 4.3 Query parser

`internal/query/`

Lexer → recursive-descent parser → AST.

Grammar sketch:

```
expr    := term (OR term)*
term    := factor (AND? factor)*
factor  := NOT? atom
atom    := WORD | PHRASE | FIELD ':' atom | '(' expr ')'
```

Supports `"exact phrase"`, `AND`/`OR`/`NOT`, parentheses, `title:foo`.

Evaluate the AST recursively into a posting list.

### 4.4 Snippets

`internal/rank/snippet.go`

Find the passage with the highest density of query terms, extract ~200
characters around it, highlight the matches with ANSI codes. Re-analyzing the
document text at query time is fine — you only do it for the top 10.

### 4.5 PageRank

`internal/link/`

1. Build the graph: `outgoing_link` holds titles, so you need title → docID.
   Ignore links to nonexistent pages (redlinks).
2. Power iteration:
   ```
   PR(p) = (1-d)/N + d × Σ PR(q)/outdeg(q)   for q linking to p
   ```
   `d = 0.85`, ~30 iterations or until convergence.
3. Dangling nodes (no outlinks) leak rank — redistribute uniformly.
4. Store per-doc, blend into the score:
   ```
   final = bm25 + w × log(1 + pagerank)
   ```
5. **Tune `w` against your eval harness, not by eye.**

Wikipedia is one of the few corpora where link analysis genuinely helps, because
the link graph is dense and editorially curated.

### Done when

- `"climate change"` outranks `climate AND change`
- Field queries work
- Snippets render with highlighting
- PageRank's effect on NDCG is *measured* — in whichever direction

---

## Phase 5 — Performance

**~3–5 evenings**

### 5.1 Benchmarks

`testing.B` over a fixed query set. Record a baseline before optimizing
anything. You cannot claim an improvement without a before.

### 5.2 Profile

```bash
go test -bench=. -cpuprofile=cpu.out -memprofile=mem.out
go tool pprof -http=:8080 cpu.out
```

Find where the time actually goes. It's usually not where you guessed — often
allocation in the posting decode loop.

### 5.3 Skip pointers

Every √n postings, store a (docID, offset) pair. Intersection can then jump
ahead instead of scanning. Combine with galloping search: probe at exponentially
increasing strides, then binary search the bracket.

### 5.4 WAND

Store `maxScore` per term (its highest possible contribution). During top-k, if
the sum of max scores for the remaining candidates can't beat the current
k-th-best, skip them entirely.

Block-max WAND refines this to per-block maxima. Large speedups on common terms,
and a genuinely satisfying algorithm to implement.

### 5.5 Cache

LRU over query → results. Measure your hit rate before assuming it helps.

### 5.6 Incremental updates

- New documents → new segment
- Deletes → a tombstone bitmap, filtered at query time
- Merge policy: combine segments when count or size crosses a threshold
- Merge in a background goroutine, swap atomically

This is Lucene's architecture in miniature.

### Done when

You can show a before/after latency distribution and explain exactly what WAND
skipped and why it was safe to skip.

---

## Phase 6 — Elasticsearch

**~1 weekend**

Now that you've built one, use one.

### 6.1 Load

```bash
docker run -p 9200:9200 -e "discovery.type=single-node" \
  docker.elastic.co/elasticsearch/elasticsearch:8.x
```

Convert your `Document` stream to bulk-API format and load it. Set
`refresh_interval: -1` during load, restore afterward — and note that you now
know *why* that helps, because you've written a segment merger.

### 6.2 Verify your analyzer

```bash
curl -X POST "localhost:9200/wiki/_analyze" -H 'Content-Type: application/json' -d'
{ "analyzer": "english", "text": "The running dogs quickly jumped" }'
```

Diff against your tokenizer on the same input. Stemming and Unicode edge cases
are where hand-rolled analyzers quietly diverge.

### 6.3 Verify your scores

```bash
curl "localhost:9200/wiki/_explain/12345" -H 'Content-Type: application/json' -d'
{ "query": { "match": { "text": "photosynthesis" } } }'
```

`_explain` breaks the score into idf, tf, doc length, and avgdl. Compare against
your BM25 on the same document. If they diverge, you have a bug and a precise
place to look.

**This single technique is worth the whole phase.**

### 6.4 Compare

Point your eval harness at Elasticsearch. Its NDCG is a realistic target. Then
tune — field boosts, `multi_match`, `function_score` with your PageRank — and
watch how far the gap closes.

### Done when

You can articulate exactly where Elasticsearch beats your engine and why, in
terms of specific techniques rather than "it's more optimized."

---

## Dependencies

Deliberately minimal — the point is writing this yourself.

| Package | Use |
|---|---|
| `github.com/kljensen/snowball` | Porter2 stemming |
| `golang.org/x/text/unicode/norm` | Unicode normalization |
| `golang.org/x/exp/mmap` | memory-mapped reads |
| `github.com/mattn/go-sqlite3` | FTS5 baseline only |

Everything else is stdlib: `compress/bzip2`, `compress/gzip`, `encoding/json`,
`encoding/binary`, `container/heap`, `bufio`, `sort`.

---

## Glossary

**Posting** — one (document, term-frequency, [positions]) entry in a term's list.

**Posting list** — all documents containing a term, sorted by doc ID.

**Inverted index** — term → posting list. "Inverted" relative to the forward
index (document → terms).

**Forward store** — doc ID → the document's metadata and content.

**Segment** — an immutable, self-contained chunk of index. Multiple segments
merge into one.

**df / idf** — document frequency: how many docs contain a term. Inverse
document frequency: the log-scaled inverse, so rare terms count more.

**tf** — term frequency: occurrences within one document.

**avgdl** — average document length across the corpus, for BM25 normalization.

**SPIMI** — Single-Pass In-Memory Indexing: build blocks in RAM, spill sorted
blocks to disk, k-way merge.

**WAND** — Weak AND: skip documents that provably can't reach the current top-k.

**NDCG** — Normalized Discounted Cumulative Gain: rank-weighted relevance,
normalized against a perfect ordering.

**Tombstone** — a delete marker, since segments are immutable.

---

## Further reading

I'm recalling these from memory rather than a database, so verify the details
before relying on them:

- **Manning, Raghavan & Schütze, *Introduction to Information Retrieval*** —
  the standard textbook, and I believe still freely available from Stanford.
  Chapters 1–6 map almost exactly onto Phases 1–3 here.
- **Büttcher, Clarke & Cormack, *Information Retrieval: Implementing and
  Evaluating Search Engines*** — more implementation-focused, good on
  compression and index construction.
- **The original BM25 papers** by Robertson and Sparck Jones, for where the
  formula comes from.
- **The Lucene source** — dense, but the segment format and codec packages are
  the production version of what Phase 3 builds.
- **Bleve** — a full-text search library in Go. Worth reading once you've
  written your own version, as a comparison.

---

## Progress

- [ ] Phase 0 — Setup
- [ ] Phase 1 — Minimum viable search
- [ ] Phase 2 — Ranking and measurement
- [ ] Phase 3 — Persistence
- [ ] Phase 4 — Richer retrieval
- [ ] Phase 5 — Performance
- [ ] Phase 6 — Elasticsearch
