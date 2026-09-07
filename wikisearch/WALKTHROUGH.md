# wikisearch — Complete Code Walkthrough

> End-to-end explanation of every step with concrete examples.
> One running example throughout: indexing and searching **"photosynthesis plant"**.

---

## Step 1 — Reading the Corpus

**File:** `internal/corpus/reader.go`

The Wikipedia dump is line-delimited JSON in **pairs**. Every odd line is an action line (metadata), every even line is the document.

```
Line 1 (odd)  → {"index":{"_id":"12345"}}         ← skip this
Line 2 (even) → {"title":"Photosynthesis","text":"Photosynthesis is a process...","outgoing_link":["Plant","Chlorophyll"]}
Line 3 (odd)  → {"index":{"_id":"12346"}}         ← skip this
Line 4 (even) → {"title":"Plant","text":"A plant is a living thing...","outgoing_link":["Photosynthesis"]}
```

Reader opens the file, wraps it in a gzip or bzip2 decompressor (sniffed by extension), creates a scanner with a **10MB buffer** (Wikipedia articles can be huge), then reads line by line:

```go
r.line++
if r.line%2 == 1 {
    continue  // skip odd (action) lines
}
json.Unmarshal(r.sc.Bytes(), &raw)
```

Each decoded document gets a **sequential ID** starting at 0, regardless of the Wikipedia page ID:

```
doc.ID=0  Title="Photosynthesis"  Links=["Plant","Chlorophyll"]
doc.ID=1  Title="Plant"           Links=["Photosynthesis"]
doc.ID=2  Title="Chlorophyll"     Links=["Photosynthesis","Plant"]
```

---

## Step 2 — Analyzer Pipeline

**File:** `internal/analysis/`

Every document's text and every query string goes through the exact same 5-step pipeline. This is the **analyzer symmetry invariant** — diverging here causes silent search misses.

### Input
```
"The Running Dogs jumped quickly"
```

### Step 2.1 — Tokenize

Scan rune by rune. Accumulate letters/digits; flush on anything else.

```
'T' letter  → buf=[T]
'h' letter  → buf=[T,h]
'e' letter  → buf=[T,h,e]
' ' space   → flush → Token{Term:"The", Position:0}  pos++
'R' letter  → buf=[R]
...
```

Result:
```
Token{Term:"The",     Position:0}
Token{Term:"Running", Position:1}
Token{Term:"Dogs",    Position:2}
Token{Term:"jumped",  Position:3}
Token{Term:"quickly", Position:4}
```

Why `unicode.IsLetter` not ASCII check:
- `café` → stays as one token (é is a letter)
- `C++` → splits into `["C"]` (+ is not a letter)
- `don't` → `["don", "t"]` (' is not a letter)

### Step 2.2 — Lowercase

```go
strings.ToLower(tok.Term)
```

```
"The"     → "the"
"Running" → "running"
"Dogs"    → "dogs"
"jumped"  → "jumped"
"quickly" → "quickly"
```

Must happen before stemming — Porter2 rules are written for lowercase. Without this, `"Running"` and `"running"` would produce different stems.

### Step 2.3 — NFC Normalize

```go
norm.NFC.String(tok.Term)
```

Unicode has two ways to represent `é`:
- Single code point: `U+00E9` (precomposed)
- Two code points: `U+0065` (e) + `U+0301` (combining accent)

Both look identical on screen but are different bytes. Without normalization, the same word typed on two keyboards might not match in the index. NFC forces the single precomposed form always.

No visible change on ASCII input — matters for Wikipedia articles with accented characters.

### Step 2.4 — Stopword Filter

```go
if _, drop := stopwords[tok.Term]; !drop {
    out = append(out, tok)
}
```

`"the"` is in the stopwords map → dropped. **Position NOT renumbered.**

```
Before: [{the,0}, {running,1}, {dogs,2}, {jumped,3}, {quickly,4}]
After:  [          {running,1}, {dogs,2}, {jumped,3}, {quickly,4}]
                       ↑
                  gap at position 0 preserved
```

Why gap preservation matters: phrase queries (Sprint 6) check `pos(word_b) == pos(word_a) + 1`. If "the" is dropped from `"the climate change"` and positions renumber, `"climate"` moves to 0 and `"change"` to 1 — making it falsely appear adjacent to any previous word in the document.

### Step 2.5 — Porter2 Stem

```go
english.Stem(tok.Term, false)
```

Porter2 applies 6 steps of suffix-stripping rules:

```
"running" → Step 1b: ends in "ing", vowel before → strip → "runn" → double n → "run"
"dogs"    → Step 1a: ends in "s", vowel before → strip → "dog"
"jumped"  → Step 1b: ends in "ed", vowel before → strip → "jump"
"quickly" → Step 1c: ends in "y", consonant before → y→i → "quickli"
           → Step 2: ends in "li", k is valid li-ender → strip "li" → "quick"
```

### Final output

```
Token{Term:"run",   Position:1}
Token{Term:"dog",   Position:2}
Token{Term:"jump",  Position:3}
Token{Term:"quick", Position:4}
```

**Same pipeline runs on queries.** User types `"dogs"` → analyzed to `"dog"` → hits the index entry `"dog"`. This is the only reason search works across word forms.

---

## Step 3 — Building the Inverted Index

**File:** `internal/index/memory.go`

An inverted index maps each term to the list of documents containing it. "Inverted" because the forward direction is document→terms; this inverts it to term→documents.

For three documents:
```
doc0: "photosynthesis is a process used by plants"
doc1: "a plant is a living thing that uses photosynthesis"
doc2: "an animal is a living thing that cannot do photosynthesis"
```

After analysis, `MemoryIndex.Add()` builds this internal map:

```go
postings["photosynthesi"] = [
    Entry{DocID:0, TermFreq:1, Positions:[0]},
    Entry{DocID:1, TermFreq:1, Positions:[7]},
    Entry{DocID:2, TermFreq:1, Positions:[7]},
]
postings["process"] = [
    Entry{DocID:0, TermFreq:1, Positions:[2]},
]
postings["plant"] = [
    Entry{DocID:0, TermFreq:1, Positions:[5]},   // "plants" stemmed to "plant"
    Entry{DocID:1, TermFreq:1, Positions:[1]},
]
postings["live"] = [
    Entry{DocID:1, TermFreq:1, Positions:[3]},   // "living" → "live"
    Entry{DocID:2, TermFreq:1, Positions:[3]},
]
```

After `Finalize()`, every list is sorted by DocID ascending. This is the invariant that makes two-pointer merge work — all downstream operations depend on it.

---

## Step 4 — Writing to Disk (Segment Format)

**File:** `internal/index/writer.go` + `segment.go`

After indexing the full corpus in RAM, write to disk so the next startup takes milliseconds instead of minutes of re-indexing.

### segment.post — Posting data

Gap-encode docIDs (store delta from previous docID), varint-encode everything:

```
Term "photosynthesi": docFreq=3, entries: [{0,tf=1},{1,tf=1},{2,tf=1}]

Encoded bytes:
[3]    ← varint(docFreq=3)     = 1 byte
[0][1] ← varint(gap=0-0=0), varint(tf=1)   doc0
[1][1] ← varint(gap=1-0=1), varint(tf=1)   doc1
[1][1] ← varint(gap=2-1=1), varint(tf=1)   doc2

Total: 7 bytes vs 12 bytes (3×uint32 for docIDs) + overhead
On real sparse corpus lists: ~4× compression overall
```

**Varint encoding** — each byte uses 7 bits for value, bit 8 is "more bytes follow":
```
value 42  → [00101010]           = 1 byte  (fits in 7 bits, no continuation)
value 128 → [10000000][00000001] = 2 bytes (needs 8 bits)
value 300 → [10101100][00000010] = 2 bytes (300 = 0b100101100)
```

Values < 128 encode in 1 byte. Sorted docID deltas are usually < 128 → most entries = 1 byte each.

### segment.dict — Term dictionary

Sorted alphabetically so binary search is possible. Each entry holds the byte offset and length of its posting list in segment.post:

```
[uint32 numTerms = 4]
[uint16 len=4]["live"][uint64 offset=0 ][uint64 len=4]
[uint16 len=5]["plant"][uint64 offset=4][uint64 len=6]
[uint16 len=13]["photosynthesi"][uint64 offset=10][uint64 len=7]
[uint16 len=7]["process"][uint64 offset=17][uint64 len=4]
```

Loaded fully into memory as a `map[string][2]uint64`. Lookup is O(1).

### segment.docs — Document metadata

One JSON line per document (only what scoring and display need):
```json
{"t":"Photosynthesis","l":5}
{"t":"Plant","l":6}
{"t":"Animal","l":7}
```

`t` = title, `l` = token length (needed for BM25 length normalisation).

### pagerank.json

JSON array of float64, index = docID. Written by `cmd/index` after PageRank converges, loaded by `cmd/search` on startup.

### Reading back

`OpenSegment()` loads dict into `map[string][2]uint64`, reads the whole `.post` file into a `[]byte`, and reads docs line by line. Lookups slice directly into the post bytes — no copy, OS page-cache handles actual disk I/O.

---

## Step 5 — Posting List Operations

**File:** `internal/postings/ops.go`, `gallop.go`, `phrase.go`

### AND query — Intersect (two-pointer merge)

Advance whichever pointer points to the smaller docID:

```
a: [1, 3, 5, 7]
b: [2, 3, 5, 8]

i=0,j=0: a[0]=1 < b[0]=2 → advance i
i=1,j=0: a[1]=3 > b[0]=2 → advance j
i=1,j=1: a[1]=3 == b[1]=3 → MATCH, emit 3, advance both
i=2,j=2: a[2]=5 == b[2]=5 → MATCH, emit 5, advance both
i=3,j=3: a[3]=7 < b[3]=8 → advance i
i=4: end of a → done

Result: [3, 5]   O(n+m)
```

No hashing, no allocation beyond the output slice. Works because both lists are sorted.

### OR query — Union

Same two-pointer but emit from whichever side is smaller (or both on tie):

```
a: [1, 3]
b: [2, 3, 4]

i=0,j=0: 1 < 2 → emit 1 from a, advance i
i=1,j=0: 3 > 2 → emit 2 from b, advance j
i=1,j=1: 3 == 3 → emit 3 (merge), advance both
i=2 (end): append remaining b → emit 4

Result: [1, 2, 3, 4]
```

### NOT query — Difference

Walk both lists; emit from `a` only when `a[i] != b[j]`:

```
a: [1, 2, 3, 4]
b: [2, 4]

i=0,j=0: a[0]=1 < b[0]=2 → emit 1, advance i
i=1,j=0: a[1]=2 == b[0]=2 → skip, advance both
i=2,j=1: a[2]=3 < b[1]=4 → emit 3, advance i
i=3,j=1: a[3]=4 == b[1]=4 → skip, advance both

Result: [1, 3]
```

### Galloping Intersect (IntersectGallop)

For sparse lists — when one list is much shorter or has large gaps between entries. Instead of advancing one step at a time, probe exponentially then binary search:

```
a (short, dense): [1, 2, 3]
b (long, sparse): [0, 100, 200, 300, 400, 500, 600, 700, ...]

Looking for first b entry ≥ a[0]=1:
  b[start=0]=0, step=1: b[1]=100 ≥ 1 → overshoot at step 1
  Binary search [0,1]: b[0]=0 < 1, b[1]=100 ≥ 1 → j=1

No match (1 < 100). Advance a.
Looking for first b entry ≥ a[1]=2:
  b[j=1]=100 ≥ 2 already → j stays at 1
...

All a entries < 100 → no matches. Found in O(log n) not O(n).
```

Measured speedup on 10k dense × 10k sparse lists: **22× faster, 64× less memory**.

### Phrase Intersect (PhraseIntersect)

For `"climate change"` — words must appear at adjacent positions:

```
climate: [{doc0, positions:[2,5]}, {doc1, positions:[0]}]
change:  [{doc0, positions:[3,9]}, {doc2, positions:[1]}]

Both have doc0 → check positions with gap=1:
  posA=[2,5]  posB=[3,9]
  p=0,q=0: posA[0]+1 = 2+1 = 3 == posB[0]=3 → MATCH

doc1 in climate but not change → skip
doc2 in change but not climate → skip

Result: [{doc0}]  ← only doc0 has "climate" immediately before "change"
```

The position gap from the stopword filter is critical here. `"climate the change"` with "the" dropped results in climate@pos=0, change@pos=2. Gap check: 0+1=1 ≠ 2 → correctly does NOT match the phrase `"climate change"`.

---

## Step 6 — Query Parser

**File:** `internal/query/`

User types: `"climate change" AND ocean NOT pollution`

### Lexer

Scans character by character, emits tokens:

```
'"'  → start phrase → scan to closing '"' → PHRASE("climate change")
' '  → skip whitespace
'A'  → start word  → scan to space → "AND" → AND token
' '  → skip
'o'  → start word  → "ocean" → WORD("ocean")
' '  → skip
'N'  → start word  → "NOT" → NOT token
' '  → skip
'p'  → start word  → "pollution" → WORD("pollution")
EOF  → EOF token
```

Token stream: `[PHRASE, AND, WORD("ocean"), NOT, WORD("pollution"), EOF]`

### Recursive Descent Parser

Grammar rules map directly to Go functions. Each function handles one precedence level:

```
Grammar:
  expr   := term (OR term)*          ← lowest precedence
  term   := factor (AND? factor)*    ← AND is optional between words
  factor := NOT? atom
  atom   := WORD | PHRASE | FIELD:atom | '(' expr ')'

parseExpr():
  → parseTerm()
      → parseFactor() → parseAtom() → PHRASE → PhraseNode["climate","change"]
      → peek()=AND → consume AND
      → parseFactor() → parseAtom() → WORD("ocean") → TermNode["ocean"]
      → peek()=NOT → parseFactor():
          consume NOT → parseAtom() → WORD("pollution") → TermNode["pollution"]
          return NotNode{TermNode["pollution"]}
      → peek()=EOF → stop
      → 3 children → return AndNode{PhraseNode, TermNode, NotNode}
  → peek()=EOF → no OR
  → return AndNode
```

AST produced:
```
AndNode
├── PhraseNode{Terms:["climate","change"]}
├── TermNode{Term:"ocean"}
└── NotNode{Child: TermNode{Term:"pollution"}}
```

### AST Evaluation

`evalNode()` in `cmd/search` recursively evaluates each node:

```
evalNode(AndNode):
  child0 = evalNode(PhraseNode["climate","change"]):
    analyze("climate") → "climat"
    analyze("change")  → "chang"
    Lookup("climat") → ListA
    Lookup("chang")  → ListB
    PhraseIntersect(ListA, ListB, gap=1) → ListPhrase

  child1 = evalNode(TermNode["ocean"]):
    analyze("ocean") → "ocean"
    Lookup("ocean") → ListC

  Intersect(ListPhrase, ListC) → ListAnd

  child2 = evalNode(NotNode[pollution]):
    analyze("pollution") → "pollut"
    Lookup("pollut") → ListD
    → NotNode returns ListD

  Difference(ListAnd, ListD) → final candidate list
```

---

## Step 7 — Scoring (BM25)

**File:** `internal/rank/bm25.go`

For each candidate document, score it for every query term and sum:

```
BM25 formula:
  idf(t)     = log(1 + (N - df + 0.5) / (df + 0.5))
  score(t,d) = idf(t) × tf(t,d)×(k1+1) / (tf(t,d) + k1×(1 - b + b×|d|/avgdl))

Constants: k1=1.2, b=0.75
Corpus:    N=250,000 docs, avgdl=120 tokens
```

Working through term `"climat"` in two documents:

```
df = 1200  (appears in 1200 of 250000 docs)
idf = log(1 + (250000 - 1200 + 0.5) / (1200 + 0.5))
    = log(1 + 207.3) = 5.34

Doc0: tf=3 (mentioned 3 times), docLen=90 (shorter than average 120)
  norm = 3×(1.2+1) / (3 + 1.2×(1 - 0.75 + 0.75×90/120))
       = 6.6 / (3 + 1.2×0.8125)
       = 6.6 / 3.975 = 1.66
  score = 5.34 × 1.66 = 8.86  ← higher (short focused doc)

Doc1: tf=1 (mentioned once), docLen=200 (longer than average)
  norm = 1×2.2 / (1 + 1.2×(1 - 0.75 + 0.75×200/120))
       = 2.2 / (1 + 1.2×1.5)
       = 2.2 / 2.8 = 0.786
  score = 5.34 × 0.786 = 4.20  ← lower (long generic doc)
```

**What k1=1.2 controls — TF saturation:**
The 10th occurrence of "climate" contributes far less than the 3rd. Without saturation, a document that repeats a word 100 times would dominate a document that covers the topic in 5 well-chosen sentences.

```
tf=1:  norm ≈ 0.79
tf=3:  norm ≈ 1.66   (+0.87 from tf=1)
tf=10: norm ≈ 1.97   (+0.31 from tf=3)  ← diminishing returns
tf=50: norm ≈ 2.09   (+0.12 from tf=10) ← nearly saturated
```

**What b=0.75 controls — length normalisation:**
Long documents are penalised because a 5000-word article mentioning "photosynthesis" once should not outrank a 300-word article about it.

```
b=0:    ignore doc length entirely (short docs unfairly penalised)
b=0.75: partial normalisation (standard — works well empirically)
b=1:    full normalisation (very long docs heavily penalised)
```

---

## Step 8 — PageRank Blend

**File:** `internal/link/pagerank.go`

Wikipedia's link graph is dense and editorially curated. Links are meaningful — an editor chose to link "Plant" from "Photosynthesis". PageRank exploits this signal.

### Power iteration

```
3 docs, initial scores: all = 1/3 = 0.333

Links:
  doc0 (Photosynthesis) → [doc1 Plant, doc2 Chlorophyll]  outdeg=2
  doc1 (Plant)          → [doc0 Photosynthesis]            outdeg=1
  doc2 (Chlorophyll)    → [doc0 Photosynthesis]            outdeg=1

Formula: PR(p) = (1-d)/N + d × Σ PR(q)/outdeg(q)  for all q linking to p
d = 0.85 (damping factor)

Iteration 1:
  PR(doc0) = (0.15/3) + 0.85 × (PR(doc1)/1 + PR(doc2)/1)
           = 0.05 + 0.85 × (0.333 + 0.333) = 0.617

  PR(doc1) = 0.05 + 0.85 × (PR(doc0)/2)
           = 0.05 + 0.85 × 0.167 = 0.192

  PR(doc2) = 0.05 + 0.85 × (PR(doc0)/2) = 0.192

After 30 iterations (converged):
  doc0 (Photosynthesis): ~0.48  ← most linked-to, highest rank
  doc1 (Plant):          ~0.26
  doc2 (Chlorophyll):    ~0.26
```

**Damping factor d=0.85:** Models a random web surfer — 85% probability of following a link, 15% probability of teleporting to a random page. Without damping, nodes with no outlinks drain all rank from the system.

**Dangling nodes** (no outlinks) — their rank is redistributed uniformly each iteration. Without handling them, rank leaks out of the system and scores no longer sum to 1.

### Blend into final score

```
final = bm25 + w × log(1 + pagerank)

Doc0: bm25=8.86, pr=0.48, w=1.0
  final = 8.86 + log(1 + 0.48) = 8.86 + 0.39 = 9.25

Doc1: bm25=4.20, pr=0.26, w=1.0
  final = 4.20 + log(1 + 0.26) = 4.20 + 0.23 = 4.43
```

`log(1+x)` smoothing: PageRank values span orders of magnitude across 250k articles. Without the log, the most-linked article would dominate every query regardless of BM25. The log compresses the range so both signals contribute meaningfully.

Tune `w` via `cmd/eval` output, not by intuition. Start at 1.0, measure NDCG change.

---

## Step 9 — Top-k Selection

**File:** `internal/rank/topk.go`

Given 47 candidate documents, pick top 10 without sorting all 47.

Uses a **min-heap of size k** (lowest score at the top, so it can be cheaply evicted):

```
Process doc0  score=9.25: heap=[9.25]            size=1 ≤ 10, keep
Process doc1  score=4.43: heap=[4.43, 9.25]      size=2
Process doc2  score=7.11: heap=[4.43, 7.11, 9.25]
... (fill heap to 10 entries) ...
heap=[3.1, 4.43, 5.2, 6.0, 7.1, 7.5, 8.0, 8.5, 8.86, 9.25]
      ↑ min at top

Process doc11 score=2.8:  2.8 < heap_min=3.1 → discard (cannot beat top 10)
Process doc12 score=9.9:  9.9 > heap_min=3.1 → push 9.9, pop 3.1
                          heap=[4.43, 5.2, 6.0, 7.1, 7.5, 8.0, 8.5, 8.86, 9.25, 9.9]

After all docs: drain heap in reverse → descending order
```

O(n log k) — much cheaper than O(n log n) sort when k << n.

Why min-heap not max-heap: keeping the minimum at the top lets us instantly check whether a new score can displace the current worst in the top-k. A max-heap would require scanning all k entries to find the minimum.

---

## Step 10 — Snippet Generation

**File:** `internal/rank/snippet.go`

For a 5000-word article, show the most relevant ~30-word passage instead of the full text.

```
Text: "...The quick brown fox... Photosynthesis is a process
       used by plants to convert light into energy using
       chlorophyll in their leaves... The dog barked..."

queryTerms: ["photosynthesi", "plant"]

Split into words, slide a window of ~26 words (windowSize/6):
  Window at word 0  ("The quick"...): 0 hits
  Window at word 5  ("Photosynthesis is"...): 2 hits  ← most hits
  Window at word 25 ("The dog barked"): 0 hits

Extract text from word 5 to word 31:
  "Photosynthesis is a process used by plants to convert light..."

Highlight query terms with ANSI codes:
  "\033[1mPhotosynthesis\033[0m is a process used by \033[1mplants\033[0m to convert light..."
```

ANSI codes: `\033[1m` = bold on, `\033[0m` = reset. Renders as **bold** in any terminal.

---

## Step 11 — Evaluation Metrics

**File:** `internal/eval/metrics.go`, `testdata/queries.json`

Three metrics measure quality. Each runs against 15 hand-judged queries:

### Precision@10 (P@10)
Of the top 10 results, what fraction are relevant?

```
Query: "photosynthesis"
Relevant: ["Photosynthesis", "Plant", "Chlorophyll", "Leaf"]
Top 10 returned: ["Photosynthesis", "Plant", "Animal", "Water", "Chlorophyll", ...]
Relevant in top 10: 3 (Photosynthesis, Plant, Chlorophyll)
P@10 = 3/10 = 0.30
```

### Mean Reciprocal Rank (MRR)
How early does the first relevant result appear?

```
Top 10: ["Animal", "Water", "Photosynthesis", ...]
         rank 1    rank 2   rank 3 ← first relevant

MRR = 1/3 = 0.333
```

Higher is better. MRR=1.0 means the first result is always relevant.

### NDCG@10 (Normalized Discounted Cumulative Gain)
Rank-weighted relevance — relevant results at rank 1 count more than at rank 10.

```
DCG = Σ relevance(i) / log2(rank+1)

Results: [relevant, irrelevant, relevant, ...]
DCG = 1/log2(2) + 0/log2(3) + 1/log2(4) + ...
    = 1.0 + 0 + 0.5 + ...

Ideal DCG (all relevant at top):
  IDCG = 1/log2(2) + 1/log2(3) + 1/log2(4) + ...
       = 1.0 + 0.63 + 0.5 + ...

NDCG = DCG / IDCG  (0 to 1, 1.0 = perfect ordering)
```

### Running eval

```bash
go run ./cmd/eval     -dump data/sample.json.gz   # your BM25+PageRank
go run ./cmd/baseline -dump data/sample.json.gz   # SQLite FTS5

Output:
ranker                P@10     MRR   NDCG@10
mine (bm25)          0.xxx   0.xxx    0.xxx
sqlite fts5          0.xxx   0.xxx    0.xxx
```

---

## Full End-to-End Example

User runs the REPL and types: `photosynthesis plant`

```
> photosynthesis plant

── Parse ──────────────────────────────────────────────
query.Parse("photosynthesis plant")
  Lex:   [WORD("photosynthesis"), WORD("plant"), EOF]
  Parse: implicit AND between two words
  AST:   AndNode{TermNode["photosynthesis"], TermNode["plant"]}

── Evaluate ───────────────────────────────────────────
evalNode(AndNode):

  evalNode(TermNode["photosynthesis"]):
    Analyze("photosynthesis") → [{Term:"photosynthesi", Pos:0}]
    Lookup("photosynthesi") → List{
      DocFreq:3,
      Entries:[{DocID:0,tf:1}, {DocID:1,tf:1}, {DocID:2,tf:1}]
    }

  evalNode(TermNode["plant"]):
    Analyze("plant") → [{Term:"plant", Pos:0}]
    Lookup("plant") → List{
      DocFreq:2,
      Entries:[{DocID:0,tf:1}, {DocID:1,tf:3}]
    }

  Intersect(listA, listB) — two-pointer:
    docID 0 in both → emit {DocID:0}
    docID 1 in both → emit {DocID:1}
    docID 2 only in A → skip
    Result: [{DocID:0}, {DocID:1}]

── Score ───────────────────────────────────────────────
queryTerms = ["photosynthesi", "plant"]

Doc0 (Photosynthesis, len=5):
  BM25("photosynthesi", tf=1, df=3) = 3.21
  BM25("plant",         tf=1, df=2) = 4.18
  bm25_total = 7.39
  PageRank[0] = 0.48
  final = 7.39 + 1.0×log(1.48) = 7.39 + 0.39 = 7.78

Doc1 (Plant, len=6):
  BM25("photosynthesi", tf=1, df=3) = 3.05
  BM25("plant",         tf=3, df=2) = 5.87
  bm25_total = 8.92
  PageRank[1] = 0.26
  final = 8.92 + 1.0×log(1.26) = 8.92 + 0.23 = 9.15

── Rank ────────────────────────────────────────────────
TopK(k=10):
  Push doc0=7.78, push doc1=9.15
  Both fit (only 2 candidates)
  Drain in reverse: [doc1=9.15, doc0=7.78]

── Output ──────────────────────────────────────────────
found 2 documents in 1.2ms
1. Plant (9.1500)
   a plant is a living thing that uses photosynthesis
2. Photosynthesis (7.7800)
   photosynthesis is a process used by plants to convert light
```

Note: "Plant" scores higher here because it has tf=3 for "plant" (the word appears 3 times in a short doc).

---

## How All Packages Connect

```
corpus.Reader
    └─ produces corpus.Document{ID, Title, Text, Links, Length}
         │
         └─ consumed by index.MemoryIndex.Add(doc, analyzer)
              │
              ├─ uses analysis.Analyzer.Analyze(title + " " + text)
              │    └─ produces []analysis.Token{Term, Position}
              │         ├─ Position → postings.Entry.Positions
              │         └─ Term+freq → postings.Entry{DocID, TermFreq, Positions}
              │
              └─ stores postings.List{Term, DocFreq, Entries}
                   ├─ in memory: MemoryIndex.postings map
                   └─ on disk:   index.WriteSegment → segment.dict/.post/.docs
                                 index.OpenSegment  → segmentIndex
                                 (both satisfy index.Index interface)

query.Parse(input string) → query.Node (AST)
    └─ evaluated by cmd/search.evalNode(node, idx, analyzer)
         ├─ TermNode   → analyzer.Analyze + idx.Lookup
         ├─ PhraseNode → idx.Lookup × N terms + postings.PhraseIntersect
         ├─ AndNode    → evalNode children + postings.Intersect
         │               OR postings.IntersectGallop (sparse lists)
         ├─ OrNode     → evalNode children + postings.Union
         └─ NotNode    → postings.Difference

rank.BM25Scorer.Score(entry, docFreq, docLen)
    └─ reads idx.NumDocs(), idx.AvgDocLen(), idx.DocLen()
    └─ returns float64 contribution for one term

link.PageRank(graph, N=250000, d=0.85, iters=30)
    └─ graph built from corpus.Document.Links via link.BuildGraph
    └─ returns []float64 (one per docID, sums to 1.0)

final_score = Σ BM25.Score(term) + w × log(1 + PageRank[docID])

rank.TopK(results []rank.Result, k=10)
    └─ min-heap internally (container/heap)
    └─ returns []rank.Result sorted descending

rank.Snippet(text, queryTerms, windowSize=160)
    └─ sliding word window, density scoring
    └─ returns passage with ANSI bold highlights

eval.PrecisionAtK / eval.MRR / eval.NDCGAtK
    └─ compare top-k titles against testdata/queries.json judgments
    └─ used by cmd/eval and cmd/baseline to print comparison table
```
