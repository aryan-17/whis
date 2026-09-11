# wikisearch — Architecture

> Living document. Current as of Sprint 9 (all sprints complete).

---

## Big picture

```
Corpus (dump file)
      │
      ▼
┌─────────────┐
│   Reader    │  streams Document one at a time — never loads full corpus into RAM
└─────────────┘
      │  corpus.Document{ID, Title, Text, Links, Length}
      ▼
┌─────────────┐
│  Analyzer   │  tokenize → lowercase → NFC normalize → stopword → Porter2 stem
└─────────────┘
      │  []analysis.Token{Term, Position}
      ▼
┌─────────────┐                        ┌──────────────────┐
│ MemoryIndex │  term → postings.List  │  segmentIndex    │
│  (in-RAM)   │ ←──── Index iface ────▶│  (on-disk)       │
└─────────────┘                        └──────────────────┘
      │  postings.List{Entries:[{DocID,TermFreq,Positions},...]}
      ▼
┌─────────────────────────────────────────────────────┐
│  Query layer                                        │
│  query.Parse() → AST → evalNode() → postings.List  │
│  Intersect / Union / Difference / PhraseIntersect  │
│  (two-pointer merge; IntersectGallop for sparse)   │
└─────────────────────────────────────────────────────┘
      │  []postings.Entry candidates
      ▼
┌─────────────────────────────────────────────────────┐
│  Ranking layer                                      │
│  BM25.Score() per term  +  w × log(1 + PageRank)   │
│  rank.TopK(k=10)  →  min-heap O(n log k)           │
└─────────────────────────────────────────────────────┘
      │  []rank.Result{DocID, Score}
      ▼
┌─────────────┐
│ cmd/search  │  prints title + score + snippet (ANSI highlighted)
└─────────────┘
```

---

## Package map

```
wikisearch/
├── cmd/
│   ├── index/       build index from dump, write segment + pagerank.json
│   ├── search/      interactive REPL (query parser, BM25+PR, snippets)
│   ├── eval/        P@10 / MRR / NDCG@10 harness
│   ├── baseline/    SQLite FTS5 comparison
│   ├── esload/      bulk-load corpus into Elasticsearch (english analyzer, title^2)
│   └── compare/     side-by-side eval: my engine vs Elasticsearch per query
│
├── internal/
│   ├── corpus/      Document type, dump Reader (bzip2/gzip, streaming)
│   ├── analysis/    Token type, Tokenizer, filters, Analyzer pipeline
│   ├── postings/    Entry, List types; Intersect/Union/Difference/PhraseIntersect;
│   │                IntersectGallop (exponential search); varint codec
│   ├── index/       Index interface; MemoryIndex; segmentIndex + WriteSegment/OpenSegment
│   ├── rank/        Scorer interface; TFIDFScorer; BM25Scorer; TopK heap; Snippet
│   ├── query/       AST node types; Lexer; recursive-descent Parser
│   ├── link/        Graph type; BuildGraph; PageRank power iteration
│   └── eval/        PrecisionAtK; MRR; NDCGAtK
│
├── testdata/
│   └── queries.json 15 queries with relevant title judgments
└── data/
    ├── sample.json.gz       10k-article dev slice (gitignored)
    ├── index/               segment files (gitignored)
    │   ├── segment.dict
    │   ├── segment.post
    │   ├── segment.docs
    │   └── pagerank.json
    └── bench_baseline.txt   benchmark numbers before optimisation
```

---

## Core types

```go
// corpus — leaf package, no internal imports
type Document struct {
    ID     uint32   // sequential from 0; NOT the Wikipedia page ID
    Title  string
    Text   string
    Links  []string // outgoing_link titles — used for PageRank
    Length uint32   // token count after analysis, set during Add()
}

// analysis — leaf package, no internal imports
type Token struct {
    Term     string
    Position int     // ordinal index (0-based); gaps preserved when stopwords drop
}

// postings — defines the core data types used everywhere
type Entry struct {
    DocID     uint32
    TermFreq  uint32
    Positions []uint32 // nil until Sprint 6 (positional index); gaps delta-encoded on disk
}

type List struct {
    Term    string
    DocFreq uint32
    Entries []Entry  // INVARIANT: always sorted by DocID ascending
}

// index — Index interface; MemoryIndex and segmentIndex both satisfy it
type Index interface {
    Lookup(term string) (postings.List, bool)
    NumDocs() uint32
    AvgDocLen() float64
    DocLen(id uint32) uint32
    Doc(id uint32) (corpus.Document, error)
}

// rank
type Result struct {
    DocID uint32
    Score float64
}
```

---

## Package dependency rules

**Current (Sprint 8):**

```
corpus    ──▶  (nothing internal)
analysis  ──▶  (nothing internal)
postings  ──▶  (nothing internal)   ← types live here; no import of index
index     ──▶  corpus, analysis, postings
rank      ──▶  postings             ← uses postings.Entry; no concrete index import
query     ──▶  analysis
link      ──▶  (nothing internal)   ← Graph is just map[uint32][]uint32
eval      ──▶  (nothing internal)
cmd/*     ──▶  everything above
```

**Why postings imports nothing:** In Sprint 5, `PostingList`/`PostingEntry` lived in `index`. Moving them to `postings` broke a circular import (`index` needed `postings` for varint; `postings` needed `index` for its own types). The fix was correct architecturally — types belong where operations on them live.

**Forbidden (will cause import cycles or break OCP):**
- `analysis` importing `index` or `postings`
- `rank` importing `index.MemoryIndex` or `index.segmentIndex` (concrete)
- `postings` importing `rank` or `index`
- Any `internal/` package importing `cmd/`

---

## The Index interface — why it exists

```
cmd/search ──imports──▶ index.Index (interface)
                               ▲
                   ┌───────────┴────────────┐
            MemoryIndex               segmentIndex
         (built at runtime)        (loaded from disk)
         slow, no disk needed      fast startup (ms)
```

`cmd/search` takes `-index` flag → calls `index.OpenSegment()` and starts in milliseconds. Without it, rebuilds `MemoryIndex` from the dump. Query code is identical either way — the interface hides the difference. This is the Open/Closed principle: new implementations, zero caller changes.

---

## Data flow: full indexing pipeline

```
dump file (.json.gz or .json.bz2)
    │
    │  NewReader(path)
    │  — sniffs extension → picks bzip2 or gzip decompressor
    │  — 10MB scanner buffer (articles exceed 64KB default)
    ▼
Reader.Next()
    — odd lines = action lines {"index":{"_id":"..."}} → skip
    — even lines = document JSON → decode
    — assigns sequential ID from 0
    │
    │  Document{ID=0, Title="Photosynthesis", Text="...", Links=["Plant",...]}
    ▼
Analyzer.Analyze(title + " " + text)
    — Tokenize:   split on non-letter/non-digit (unicode-aware)
    — Lowercase:  strings.ToLower
    — Normalize:  NFC via golang.org/x/text
    — Stopword:   drop "the","is","a",... — GAPS PRESERVED in position
    — Stem:       Porter2 via kljensen/snowball
    │
    │  []Token{{"photosynthesi",0},{"process",1},{"plant",3},...}
    │              ↑ gap at pos 2 — "a" was stopword, position NOT renumbered
    ▼
MemoryIndex.Add(doc, analyzer)
    — counts TermFreq per term per doc
    — records Positions per term per doc (for phrase queries)
    — appends Entry to postings map
    — stores doc metadata and length
    ▼
MemoryIndex.Finalize()
    — sorts every posting list by DocID ascending (establishes invariant)
    ▼
WriteSegment(mem, dir)                  BuildGraph + PageRank
    — segment.dict: sorted terms        — title→ID lookup table
      with offsets into .post           — power iteration (d=0.85, 30 iters)
    — segment.post: gap-encoded         — dangling node redistribution
      varint docIDs + termFreq          — saves pagerank.json
    — segment.docs: JSON lines
      (title, length per doc)
```

---

## Data flow: full query pipeline (Sprint 6–7)

```
User types: '"climate change" AND ocean NOT pollution'
    │
    ▼
query.Parse(input)
    — Lexer: tokenises words, "phrases", AND/OR/NOT, (), field:
    — Parser: recursive descent
      expr → term → factor → atom
    — Returns AST:
        AndNode
          ├── PhraseNode["climate","change"]
          ├── TermNode["ocean"]
          └── NotNode → TermNode["pollution"]
    │
    ▼
evalNode(ast, idx, analyzer)
    — PhraseNode:
        Lookup("climat") → List A
        Lookup("chang")  → List B
        PhraseIntersect(A, B, gap=1)
          — two-pointer over docs, then position gap check
          — returns docs where terms adjacent
    — TermNode["ocean"]:
        Analyze("ocean") → "ocean"
        Lookup("ocean")  → List C
    — AND: Intersect(phrase_result, C) → List D
    — NotNode: Difference(D, Lookup("pollut")) → List E
    │
    │  postings.List — candidate documents
    ▼
Score each entry:
    bm25 = Σ BM25.Score(entry, pl.DocFreq, DocLen(entry.DocID))
             for each query term
    pr   = prScores[entry.DocID]
    final = bm25 + w × log(1 + pr)   ← PageRank blend
    │
    ▼
rank.TopK(results, k=10)
    — min-heap of size k
    — push all, pop when > k (evicts current lowest)
    — drain in reverse → descending score order
    │
    ▼
For each top result:
    doc = idx.Doc(res.DocID)
    snip = rank.Snippet(doc.Text, queryTerms, 160)
      — word-count sliding window
      — find window with most query term hits
      — highlight matches with ANSI bold \033[1m...\033[0m
    Print: "1. Title (score)\n   ...snippet..."
```

---

## Segment format (disk layout)

Three files written by `WriteSegment`, read by `OpenSegment`:

### segment.post

Raw posting bytes. One posting list per term, concatenated:

```
[uvarint docFreq]
[uvarint gap(docID₀)] [uvarint termFreq₀]
[uvarint gap(docID₁)] [uvarint termFreq₁]
...
```

Gap encoding: store `docID - prevDocID`, not `docID`. Sorted docIDs produce small gaps → small varints → ~4× smaller than fixed uint32.

### segment.dict

Term dictionary, sorted alphabetically:

```
[uint32 numTerms]
for each term:
  [uint16 termLen][termBytes][uint64 postOffset][uint64 postLen]
```

Loaded fully into memory as `map[string][2]uint64` (offset, length). Lookup: O(1) hash → slice into `postData`.

### segment.docs

One JSON line per document:

```json
{"t":"Photosynthesis","l":42}
```

`t` = title, `l` = token length. Minimal — only what scoring and display need.

### pagerank.json

JSON array of float64, index = docID. Written by `cmd/index`, loaded by `cmd/search`.

---

## Varint encoding

Used in `segment.post` for space efficiency.

Each byte: 7 bits of value + 1 continuation bit (bit 8).

```
value 42  → [00101010]          = 1 byte
value 128 → [10000000][00000001] = 2 bytes
value 300 → [10101100][00000010] = 2 bytes
```

Values < 128 = 1 byte. Gap-encoded sorted docIDs are usually small → most entries = 1-2 bytes. Full uint32 would always be 4 bytes. Savings: ~4× for a typical corpus.

---

## Posting list operations

All operations exploit the sorted-DocID invariant.

### Two-pointer merge (Intersect, Union, Difference)

```
a: [1, 3, 5, 7]
b: [2, 3, 5, 8]

i→1, j→2: a[i]<b[j] → i++
i→3, j→2: a[i]>b[j] → j++
i→3, j→3: match → emit 3, i++ j++
i→5, j→5: match → emit 5, i++ j++
...

Result: [3, 5]   O(n+m)
```

### Galloping search (IntersectGallop)

For sparse lists — when one list is much shorter or has large gaps.

```
Advance pointer in long list using exponential probe:
  step=1: check position start+1
  step=2: check position start+2
  step=4: check position start+4
  step=8: check position start+8
  ...until overshoot, then binary search the bracket

O(log n) per advance vs O(n) for linear scan
```

Benchmark result on 10k dense vs 10k sparse:
```
Intersect:       ~87µs, 594KB alloc
IntersectGallop: ~3.8µs, 9KB alloc   → 22× faster, 64× less memory
```

### PhraseIntersect

```
For each doc in both lists:
  Compare position lists: does any posA + gap == posB?
  Two-pointer over positions — O(p+q) per doc

"climate change" (gap=1):
  climate positions: [2, 5]
  change  positions: [3, 9]
  2+1=3 → found → doc matches phrase
```

Position gaps preserved through stopword filter are critical here. A dropped stopword creates a positional gap, so `"climate the change"` with "the" dropped results in climate@0, change@2 (gap=2, not 1) — correctly not matching the phrase `"climate change"`.

---

## Ranking: BM25 + PageRank

### BM25

```
idf(t)     = log(1 + (N - df + 0.5) / (df + 0.5))
score(t,d) = idf(t) × tf(t,d)×(k1+1) / (tf(t,d) + k1×(1 - b + b×|d|/avgdl))

k1 = 1.2  — TF saturation: 10th occurrence adds less than 3rd
b  = 0.75 — length normalisation: long docs penalised
```

Sum across all query terms for the full document score.

### PageRank blend

```
final = bm25 + w × log(1 + pagerank)
```

`log(1+x)` smooths the very wide PageRank distribution (top articles have PR orders of magnitude above median). Default `w=1.0`, tunable via `-pr-weight` flag. Measure effect via `cmd/eval` before and after — do not tune by eye.

### PageRank (power iteration)

```
PR(p) = (1-d)/N + d × Σ PR(q)/outdeg(q)   for q linking to p
```

`d=0.85` (damping), 30 iterations. Dangling nodes (no outlinks) leak rank — redistributed uniformly each iteration to maintain sum=1.

---

## Query parser grammar

```
expr   := term (OR term)*
term   := factor (AND? factor)*    AND is optional (implicit between words)
factor := NOT? atom
atom   := WORD | PHRASE | FIELD:atom | '(' expr ')'
```

Recursive descent: each rule = one function. Returns AST node, evaluated by `evalNode()` in `cmd/search`.

Supported:
- `photosynthesis plant` → AND (implicit)
- `"climate change"` → PhraseNode
- `foo OR bar` → OrNode
- `foo NOT bar` → AndNode[foo, NotNode[bar]]
- `title:darwin` → FieldNode (currently treated as term; field boost future work)
- `(foo OR bar) baz` → AndNode[OrNode, TermNode]

---

## Invariants (never break)

| # | Invariant | Where established | Tests |
|---|-----------|------------------|-------|
| 1 | Analyzer symmetry — index and query use identical pipeline | `cmd/search` creates one `Analyzer` | `TestAnalyzerIdempotent` |
| 2 | Posting list sorted by DocID ascending | `MemoryIndex.Finalize()`, `WriteSegment` order | `TestPostingsSortedByDocID` |
| 3 | Position gaps preserved through stopword filter | `StopwordFilter` — no renumbering | `TestStopwordFilterPreservesGap` |
| 4 | Segment eval scores == MemoryIndex scores | `TestSegment*` roundtrip | `TestSegmentLookupDocIDs`, `TestSegmentLookupDocFreq` |
| 5 | Even-line rule — dump slices use even line counts | `Reader` skips odd lines | `TestReaderSkipsOddLines` |

---

## Sprint completion

| Sprint | What was added | Commits |
|--------|---------------|---------|
| ✅ 1 | `corpus`, `index` interface, dump `Reader`, `cmd/index` skeleton | `dcd7a48` |
| ✅ 2 | `analysis` pipeline (tokenize→normalize→stopword→stem) | `cc7d201` |
| ✅ 3 | `postings.Entry/List` types, `MemoryIndex`, boolean ops, `cmd/search` REPL | `0f8bc18` |
| ✅ 4 | `rank` (TF-IDF, BM25, top-k), `eval` metrics, `cmd/eval`, `cmd/baseline` | `16c19c3` |
| ✅ 5 | Varint codec, segment writer/reader, `segmentIndex` on disk | `ca9772e` |
| ✅ 6 | Positional index, `PhraseIntersect`, query lexer+parser+AST, `Snippet` | `4dcdf86` |
| ✅ 7 | `link.Graph`, `PageRank`, BM25+PR score blending | `8be292b` |
| ✅ 8 | `IntersectGallop` (22× speedup), benchmarks, `bench_baseline.txt` | `efdb79f` |
| ✅ 9 | `cmd/esload` (bulk loader), `cmd/compare` (side-by-side eval table) | `63370f3` |

---

## Elasticsearch comparison (Sprint 9)

`cmd/esload` creates an index with the `english` analyzer (same stemming/stopwords as ours) and bulk-loads the corpus. `cmd/compare` queries both engines with the same 15 queries and prints a table.

**ES index config:**
```json
{
  "title": { "type": "text", "analyzer": "english", "boost": 2 },
  "text":  { "type": "text", "analyzer": "english" }
}
```

**ES query per search:**
```json
{
  "query": {
    "multi_match": {
      "query": "<user input>",
      "fields": ["title^2", "text"],
      "type": "best_fields"
    }
  }
}
```

**What to compare:**
- Aggregate P@10 / MRR / NDCG@10 side by side
- Per-query NDCG with winner column
- Use `_analyze` endpoint to diff tokenization vs your pipeline
- Use `_explain` endpoint to diff BM25 scores on same doc+term

**Why ES often wins:** its BM25 implementation handles edge cases (numeric fields, position information, index statistics) more precisely. Its `english` analyzer also uses a dictionary-based approach for irregular forms that Porter2 misses (e.g. `"went"` → `"go"`).

---

## Key design decisions

**Streaming reader, not `[]Document`**
`Reader.Next()` never loads the full corpus (537MB) into RAM. Required for SPIMI (build partial blocks, spill, merge) if pointed at full enwiki.

**`Index` as interface from day one**
Written in Sprint 3, `segmentIndex` dropped in Sprint 5 with zero changes to query code. Every sprint after Sprint 3 used the interface — OCP proven in practice.

**`PostingList`/`PostingEntry` in `postings`, not `index`**
Sprint 5 discovered: `index` needed to import `postings` for varint; `postings` originally imported `index` for its own types → cycle. Moving types to `postings` (where operations live) broke the cycle and is the correct design.

**Positions nil until Sprint 6**
`Entry.Positions` starts nil. Populated in Sprint 6 `MemoryIndex.Add()`. Avoids 2× memory cost (one entry per occurrence vs per document) until phrase queries actually exist.

**Gap encoding on docIDs**
Sorted docIDs deltas to small numbers. varint encodes small numbers in 1 byte. Combined: ~4× smaller than fixed uint32. Enables keeping the full posting file in memory.

**IntersectGallop for sparse lists**
Two-pointer is O(n+m) — optimal when lists are similar density. When one list is much shorter or has large sparse gaps, galloping reduces the advance from O(n) to O(log n). The shorter list always drives; the longer list is galloped. 22× measured speedup in the benchmark.

**BM25 k1=1.2, b=0.75**
Standard defaults. `k1` controls TF saturation — the 10th occurrence of a word contributes far less than the 3rd. `b=0.75` normalises for document length — long Wikipedia articles don't dominate just because they're long. Tune against `cmd/eval` output, not by intuition.
