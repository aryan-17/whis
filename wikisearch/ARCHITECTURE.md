# wikisearch — Architecture

> Living document. Updated after each sprint.

---

## Big picture

```
Corpus (dump file)
      │
      ▼
┌─────────────┐
│   Reader    │  streams Document one at a time (never loads full corpus)
└─────────────┘
      │  Document{ID, Title, Text, Links, Length}
      ▼
┌─────────────┐
│  Analyzer   │  tokenize → lowercase → NFC normalize → stopword → stem
└─────────────┘
      │  []Token{Term, Position}
      ▼
┌─────────────┐
│ MemoryIndex │  term → PostingList → []PostingEntry{DocID, TermFreq}
└─────────────┘
      │  Index interface
      ▼
┌─────────────┐     ┌──────────────┐
│   Postings  │     │    Scorer    │  BM25 / TF-IDF (Sprint 4)
│    ops      │     │   + Top-k   │
│ (AND/OR/NOT)│     └──────────────┘
└─────────────┘
      │  PostingList
      ▼
┌─────────────┐
│ cmd/search  │  REPL — reads query, returns ranked titles
└─────────────┘
```

---

## Package map

```
wikisearch/
├── cmd/
│   ├── index/       CLI: read dump → build index → (future: write segment)
│   ├── search/      CLI: interactive REPL
│   ├── eval/        CLI: run eval harness, print P@10/MRR/NDCG (Sprint 4)
│   └── baseline/    CLI: SQLite FTS5 comparison (Sprint 4)
│
├── internal/
│   ├── corpus/      Document type + dump Reader
│   ├── analysis/    Analyzer pipeline (Token, filters, Analyzer)
│   ├── index/       Index interface, PostingList/PostingEntry types, MemoryIndex
│   ├── postings/    Set ops on PostingLists (Intersect, Union, Difference)
│   ├── rank/        Scorers (TF-IDF, BM25), top-k heap, snippets (Sprint 4+)
│   ├── query/       Lexer, parser, AST for structured queries (Sprint 6)
│   ├── link/        Link graph + PageRank (Sprint 7)
│   └── eval/        Metrics: P@k, MRR, NDCG (Sprint 4)
│
├── testdata/
│   └── queries.json relevance judgments for eval harness
└── data/            dumps + built indexes (gitignored)
```

---

## Core types

```go
// corpus — leaf package, no internal imports
type Document struct {
    ID     uint32   // sequential, assigned by Reader (not Wikipedia's page ID)
    Title  string
    Text   string
    Links  []string // outgoing_link — used for PageRank (Sprint 7)
    Length uint32   // token count, set by indexer after analysis
}

// analysis
type Token struct {
    Term     string
    Position int     // ordinal index; gaps preserved when stopwords dropped
}

// index — central types shared by all packages
type PostingEntry struct {
    DocID     uint32
    TermFreq  uint32
    Positions []uint32 // nil until Sprint 6 (positional index)
}

type PostingList struct {
    Term    string
    DocFreq uint32
    Entries []PostingEntry // INVARIANT: always sorted by DocID ascending
}

// Index interface — all implementations satisfy this
type Index interface {
    Lookup(term string) (PostingList, bool)
    NumDocs() uint32
    AvgDocLen() float64
    DocLen(id uint32) uint32
    Doc(id uint32) (corpus.Document, error)
}
```

---

## The Index interface — why it exists

`cmd/search` depends on `index.Index`, never on `MemoryIndex` or `SegmentIndex` directly.

```
cmd/search ──imports──▶ index.Index (interface)
                              ▲
                    ┌─────────┴──────────┐
             MemoryIndex          SegmentIndex
            (Sprint 3)            (Sprint 5)
```

Swapping implementations requires zero changes to query code. This is the Open/Closed principle in practice: extend by adding a new implementation, not by modifying existing callers.

---

## Data flow: indexing

```
dump file
    │
    │ NewReader(path)  ← sniffs .bz2 vs .gz by extension
    ▼
Reader.Next()          ← skips odd lines (action), decodes even lines (doc)
    │
    │ Document{ID=0, Title="Photosynthesis", Text="...", Links=[...]}
    ▼
Analyzer.Analyze(title + " " + text)
    │
    │ []Token{{"photosynthesi",0}, {"process",1}, {"plant",3}, ...}
    │                                              ↑ gap at 2 — "a" was stopword
    ▼
MemoryIndex.Add(doc, analyzer)
    │
    │ counts term freq per doc, appends to postings map
    ▼
MemoryIndex.Finalize()
    │
    └── sorts every posting list by DocID  ← invariant established here
```

---

## Data flow: querying (Sprint 3, boolean)

```
query string: "photosynthesis plant"
    │
    ▼
Analyzer.Analyze(query)        ← SAME analyzer as index time
    │
    │ []Token{{"photosynthesi",0}, {"plant",1}}
    ▼
idx.Lookup("photosynthesi")    → PostingList{entries:[{0,2},{1,1},{2,1}]}
idx.Lookup("plant")            → PostingList{entries:[{0,1},{1,3}]}
    │
    ▼
postings.Intersect(a, b)       → PostingList{entries:[{0,3},{1,4}]}
    │                                          ↑ docIDs in both lists
    ▼
print top 10 titles            ← docID order (no ranking yet)
```

---

## Data flow: querying (Sprint 4+, ranked)

```
... (same up to Intersect) ...
    │
    ▼
for each entry in result:
    score += BM25.Score(entry, docFreq, DocLen(entry.DocID))
    │
    ▼
rank.TopK(results, k=10)       ← min-heap, O(n log k)
    │
    ▼
print titles with scores
```

---

## Analyzer invariant

**Index-time and query-time analysis must be identical.**

The same `Analyzer` instance (or identical config) must process both document text and query strings. Any divergence = silent misses.

```
"Photosynthesis" ──index──▶ "photosynthesi"  ✓ match
"photosynthesis" ──query──▶ "photosynthesi"  ✓

"Photosynthesis" ──index──▶ "photosynthesi"  ✗ miss
"photosynthesis" ──query──▶ "photosynthesis" ✗ (forgot to stem query)
```

---

## Posting list invariant

Entries in every `PostingList` are **always sorted by DocID ascending**.

- `MemoryIndex.Finalize()` establishes this after build.
- `SegmentIndex` (Sprint 5) writes and reads in sorted order.
- All two-pointer ops (`Intersect`, `Union`, `Difference`) rely on it.
- Tests assert it explicitly (`TestPostingsSortedByDocID`).

---

## Package dependency rules

```
corpus    ──▶  (nothing internal)
analysis  ──▶  (nothing internal)
postings  ──▶  index (types only)
index     ──▶  corpus, analysis
rank      ──▶  index (interface only)
query     ──▶  analysis
link      ──▶  corpus, index (interface only)
eval      ──▶  (nothing internal)
cmd/*     ──▶  everything above, wires it together
```

**Forbidden:**
- `analysis` importing `index`
- `rank` importing `index.MemoryIndex` or `index.SegmentIndex` (concrete types)
- `postings` importing `rank`
- Any `internal/` package importing `cmd/`

---

## What changes each sprint

| Sprint | What's added | What changes |
|--------|-------------|--------------|
| ✅ 1 | `corpus`, `index` interface, `cmd/index` | — |
| ✅ 2 | `analysis` pipeline | — |
| ✅ 3 | `index.MemoryIndex`, `postings` ops, `cmd/search` REPL | — |
| 4 | `rank` (BM25, top-k), `eval` metrics, `cmd/eval`, `cmd/baseline` | `cmd/search` adds scoring |
| 5 | `index.SegmentIndex`, varint codec, segment writer/reader | `cmd/index` writes to disk; `cmd/search` loads from disk |
| 6 | `PostingEntry.Positions`, `postings.PhraseIntersect`, `query` parser, snippets | `MemoryIndex.Add` stores positions; `cmd/search` handles structured queries |
| 7 | `link` package (graph + PageRank) | `cmd/search` blends PageRank into BM25 score |
| 8 | Benchmarks, skip pointers in `Intersect`, WAND in `rank` | `postings/ops.go` gets galloping search |
| 9 | `cmd/esload`, `cmd/eseval` | no changes to core packages |

---

## Key design decisions

**Streaming reader, not `[]Document`**
`Reader.Next()` returns one document at a time. Never loads the full corpus (537MB) into memory. Enables SPIMI (Sprint 5): build partial indexes, spill to disk, merge.

**`Index` as interface, not struct**
`cmd/search` written against the interface in Sprint 3. Sprint 5 replaces the implementation without touching query code.

**Positions stored as nil until needed**
`PostingEntry.Positions` is `nil` in Sprints 1–5. Populated in Sprint 6. Avoids 2× memory cost until phrase queries are actually implemented.

**Two-pointer merge, not hash sets**
`Intersect`/`Union`/`Difference` exploit the sorted-DocID invariant to run in O(n+m). No allocations beyond the output slice. Skip pointers (Sprint 8) extend this with O(log n) jumps.
