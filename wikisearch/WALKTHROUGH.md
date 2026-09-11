# wikisearch — Complete Code Walkthrough

One example traces every step from raw dump file to ranked search results, then compares against Elasticsearch.

**The query:** `"climate change" AND ocean`  
**Two documents in our tiny corpus:**

```
doc0: title="Climate Change"   text="Climate change affects the ocean and plant life globally."
      outgoing_link=["Ocean","Greenhouse gas"]

doc1: title="Ocean"            text="The ocean is a large body of water covering most of Earth."
      outgoing_link=["Water","Earth"]
```

By the end you will see exactly why doc0 matches and doc1 does not.

---

## Step 1 — Reading the Corpus

**File:** `internal/corpus/reader.go`

The Wikipedia dump is line-delimited JSON in **pairs**. Odd line = action (metadata), even line = document.

```
Line 1 (odd)  → {"index":{"_id":"11111"}}
Line 2 (even) → {"title":"Climate Change","text":"Climate change affects the ocean...","outgoing_link":["Ocean","Greenhouse gas"]}
Line 3 (odd)  → {"index":{"_id":"22222"}}
Line 4 (even) → {"title":"Ocean","text":"The ocean is a large body of water...","outgoing_link":["Water","Earth"]}
```

Reader wraps the file in a gzip/bzip2 decompressor (sniffed by extension), creates a scanner with a **10MB buffer** (some articles exceed the 64KB default), then reads line by line:

```go
r.line++
if r.line%2 == 1 {
    continue  // odd = action line, skip
}
json.Unmarshal(r.sc.Bytes(), &raw)
```

Assigns sequential IDs starting at 0 — not the Wikipedia page ID:

```
doc.ID=0  Title="Climate Change"  Text="Climate change affects..."  Links=["Ocean","Greenhouse gas"]
doc.ID=1  Title="Ocean"           Text="The ocean is a large..."    Links=["Water","Earth"]
```

---

## Step 2 — Analyzer Pipeline

**File:** `internal/analysis/`

Every piece of text — at index-time AND query-time — goes through the exact same 5 steps. Any divergence = silent miss.

We analyze doc0's text: `"Climate change affects the ocean and plant life globally."`

### 2.1 Tokenize

Scan rune by rune. Accumulate letters/digits, flush on anything else. Record position as ordinal index.

```
"Climate change affects the ocean and plant life globally."

'C','l','i','m','a','t','e' → buf=[Climate]
' ' → flush → Token{Term:"Climate", Position:0}

'c','h','a','n','g','e' → buf=[change]
' ' → flush → Token{Term:"change", Position:1}

... and so on
'.' → flush last word
```

Result:
```
{Term:"Climate",  Position:0}
{Term:"change",   Position:1}
{Term:"affects",  Position:2}
{Term:"the",      Position:3}
{Term:"ocean",    Position:4}
{Term:"and",      Position:5}
{Term:"plant",    Position:6}
{Term:"life",     Position:7}
{Term:"globally", Position:8}
```

### 2.2 Lowercase

```go
strings.ToLower(tok.Term)
```

```
Climate → climate
change  → change
affects → affects
the     → the
ocean   → ocean
and     → and
plant   → plant
life    → life
globally→ globally
```

Must happen before stemming — Porter2 rules are written for lowercase.

### 2.3 NFC Normalize

```go
norm.NFC.String(tok.Term)
```

Forces Unicode composed form. `é` written as `e + combining accent` becomes the single `U+00E9` code point. No visible change on our ASCII example — matters for accented characters in other Wikipedia articles.

### 2.4 Stopword Filter

Check each term against a `map[string]struct{}` of ~50 common words. Drop matches. **Do NOT renumber positions.**

Stopwords hit: `"the"` (position 3), `"and"` (position 5)

```
Before: [climate@0, change@1, affects@2, THE@3,  ocean@4, AND@5,  plant@6, life@7, globally@8]
After:  [climate@0, change@1, affects@2,          ocean@4,         plant@6, life@7, globally@8]
                                          ↑ gap                ↑ gap
                                       pos 3 gone          pos 5 gone
                                       positions NOT renumbered
```

**Why gaps matter:** phrase queries check `pos(word_b) == pos(word_a) + 1`. If we renumbered, "change" would move from position 1 to position 1 (fine here), but "ocean" would move from position 4 to position 2 — making it look adjacent to "affects" in position space. That would cause false phrase matches. Gaps preserve the truth.

### 2.5 Porter2 Stem

```go
english.Stem(tok.Term, false)
```

```
climate  → Step 4: ends in "ate" in R2 → strip → "clim"... actually:
           Step 0-3: no match
           Step 4: "imate" not a handled suffix
           → "climat"          (Step 1a: ends in "e" not in this context)

Actually Porter2 on "climate":
  Step 5a: ends in "e", in R1 → strip → "climat"

change   → Step 1b: no "ed"/"ing" → Step 4: no match → Step 5a: ends in "e" → "chang"
affects  → Step 1a: ends in "s", vowel before → strip → "affect"
ocean    → no suffix rules match → "ocean"
plant    → no suffix rules match → "plant"
life     → Step 5a: ends in "e", in R1 → "lif"
globally → Step 1c: ends in "y", consonant before → "globalli"
           Step 2: ends in "li", l is valid → strip "li" → "global"
```

Final token stream for doc0:

```
{Term:"climat",  Position:0}
{Term:"chang",   Position:1}
{Term:"affect",  Position:2}
{Term:"ocean",   Position:4}   ← gap at 3 (was "the")
{Term:"plant",   Position:6}   ← gap at 5 (was "and")
{Term:"lif",     Position:7}
{Term:"global",  Position:8}
```

Same pipeline on doc1 text `"The ocean is a large body of water covering most of Earth."`:

```
{Term:"ocean",  Position:1}   ← "The" dropped at pos 0
{Term:"larg",   Position:3}   ← "is","a" dropped at pos 2,3... wait:
```

Actually tracing doc1:
```
Tokenize: [The@0, ocean@1, is@2, a@3, large@4, body@5, of@6, water@7, covering@8, most@9, of@10, Earth@11]
Stopword: drop "The"@0, "is"@2, "a"@3, "of"@6, "of"@10
Remaining: [ocean@1, large@4, body@5, water@7, covering@8, most@9, Earth@11]
Stem:      [ocean@1, larg@4, bodi@5, water@7, cover@8, most@9, earth@11]
```

---

## Step 3 — Building the Inverted Index

**File:** `internal/index/memory.go`

`MemoryIndex.Add()` processes both documents. It counts term frequencies and records positions:

```
After doc0:
postings["climat"] = [{DocID:0, tf:1, pos:[0]}]
postings["chang"]  = [{DocID:0, tf:1, pos:[1]}]
postings["affect"] = [{DocID:0, tf:1, pos:[2]}]
postings["ocean"]  = [{DocID:0, tf:1, pos:[4]}]
postings["plant"]  = [{DocID:0, tf:1, pos:[6]}]

After doc1:
postings["ocean"]  = [{DocID:0, tf:1, pos:[4]}, {DocID:1, tf:1, pos:[1]}]
postings["larg"]   = [{DocID:1, tf:1, pos:[4]}]
postings["water"]  = [{DocID:1, tf:1, pos:[7]}]
...
```

After `Finalize()` — sorts every posting list by DocID:

```
postings["ocean"] = [{DocID:0, tf:1, pos:[4]}, {DocID:1, tf:1, pos:[1]}]
                          ↑ sorted                    ↑ sorted
```

This sort is the invariant that makes all downstream operations work.

---

## Step 4 — Writing to Disk

**File:** `internal/index/writer.go` + `segment.go`

Three files written after indexing. On the next startup, `cmd/search -index data/index` calls `OpenSegment()` to load them — milliseconds instead of re-reading the dump.

### segment.post

Posting data, concatenated, for every term in alphabetical order. DocIDs gap-encoded, everything varint:

```
Term "affect" (only in doc0): docFreq=1
  buf = [01]           ← varint(docFreq=1)
        [00]           ← varint(gap=0-0=0, first entry)
        [01]           ← varint(tf=1)
  → 3 bytes at offset 0

Term "chang" (only in doc0): docFreq=1
  buf = [01][00][01]   ← same pattern
  → 3 bytes at offset 3

Term "climat" (only in doc0): docFreq=1
  buf = [01][00][01]
  → 3 bytes at offset 6

Term "ocean" (in both docs): docFreq=2
  buf = [02]           ← varint(docFreq=2)
        [00][01]       ← gap=0→docID=0, tf=1
        [01][01]       ← gap=1→docID=1, tf=1
  → 5 bytes at offset 9
```

Varint: values < 128 fit in 1 byte. DocID gaps between adjacent docs = 1 → 1 byte each. Fixed uint32 would cost 4 bytes each. ~4× savings.

### segment.dict

Sorted term directory with byte offsets into segment.post:

```
[uint32 numTerms = 4]
[uint16 len=6]["affect"][uint64 offset=0][uint64 len=3]
[uint16 len=5]["chang" ][uint64 offset=3][uint64 len=3]
[uint16 len=6]["climat"][uint64 offset=6][uint64 len=3]
[uint16 len=5]["ocean" ][uint64 offset=9][uint64 len=5]
```

Loaded into memory as `map[string][2]uint64`. Lookup is O(1).

### segment.docs

One JSON line per document. Line index = docID:

```
line 0 → {"t":"Climate Change","l":8}   ← docID 0, 8 tokens
line 1 → {"t":"Ocean","l":7}            ← docID 1, 7 tokens
```

### pagerank.json

```json
[0.62, 0.38]
```

doc0 (Climate Change) has higher PageRank because doc1 links to it via "Earth" and indirectly through the link graph.

---

## Step 5 — Query Parsing

**File:** `internal/query/`

User types: `"climate change" AND ocean`

### Lexer

```
'"'        → start phrase scan → to closing '"' → PHRASE("climate change")
' '        → skip
'A','N','D'→ "AND" keyword      → AND token
' '        → skip
'o'...'n'  → scan word → "ocean" → WORD token
EOF        → EOF token
```

Token stream: `[PHRASE("climate change"), AND, WORD("ocean"), EOF]`

### Recursive Descent Parser

```
parseExpr()
  → parseTerm()
       peek = PHRASE → parseFactor() → parseAtom()
         consume PHRASE → PhraseNode{Terms:["climate","change"]}
       peek = AND → consume AND
       parseFactor() → parseAtom()
         consume WORD("ocean") → TermNode{Term:"ocean"}
       peek = EOF → stop
       2 children → AndNode{PhraseNode, TermNode}
  peek = EOF → no OR
  return AndNode
```

AST:
```
AndNode
├── PhraseNode{Terms:["climate","change"]}
└── TermNode{Term:"ocean"}
```

---

## Step 6 — Query Evaluation

**File:** `cmd/search/main.go` → `evalNode()`

The AST is evaluated recursively.

### evalNode(PhraseNode["climate","change"])

```
analyze("climate") → [{Term:"climat", Position:0}]
analyze("change")  → [{Term:"chang",  Position:0}]

Lookup("climat") → List{DocFreq:1, Entries:[{DocID:0, tf:1, pos:[0]}]}
Lookup("chang")  → List{DocFreq:1, Entries:[{DocID:0, tf:1, pos:[1]}]}

PhraseIntersect(listA, listB, gap=1):
  Both have doc0 → check positions:
    posA=[0], posB=[1], gap=1
    posA[0] + 1 = 0 + 1 = 1 == posB[0] = 1 → MATCH

  Result: [{DocID:0}]
```

doc1 not in either list → not a candidate.

**Why doc1 fails the phrase check:** doc1 doesn't contain "climat" at all. Even if it did, the positions would need to be exactly 1 apart. The position gap preservation from Step 2.4 is what makes this check reliable.

### evalNode(TermNode["ocean"])

```
analyze("ocean") → [{Term:"ocean", Position:0}]
Lookup("ocean")  → List{DocFreq:2, Entries:[{DocID:0, tf:1}, {DocID:1, tf:1}]}
```

Both doc0 and doc1 contain "ocean".

### evalNode(AndNode) — Intersect

```
phraseResult: [{DocID:0}]
oceanResult:  [{DocID:0}, {DocID:1}]

Intersect — two-pointer merge:
  i=0, j=0: phraseResult[0].DocID=0 == oceanResult[0].DocID=0 → MATCH, emit {DocID:0}
  i=1: end of phraseResult → done

Final candidates: [{DocID:0}]
```

**doc1 eliminated here.** It contains "ocean" but not the phrase "climate change" — the Intersect drops it.

---

## Step 7 — Scoring

**File:** `internal/rank/bm25.go`

### Cache posting lists first (one lookup per term, not per doc)

```go
termPLs["climat"] = Lookup("climat") → {DocFreq:1}
termPLs["chang"]  = Lookup("chang")  → {DocFreq:1}
termPLs["ocean"]  = Lookup("ocean")  → {DocFreq:2}
```

### BM25 for doc0

```
Corpus: N=2, avgdl=(8+7)/2=7.5
Doc0: docLen=8

Term "climat": df=1, tf=1
  idf = log(1 + (2 - 1 + 0.5) / (1 + 0.5)) = log(1 + 1.0) = log(2.0) = 0.693
  norm = 1×2.2 / (1 + 1.2×(1 - 0.75 + 0.75×8/7.5))
       = 2.2 / (1 + 1.2×1.05)
       = 2.2 / 2.26 = 0.973
  score = 0.693 × 0.973 = 0.674

Term "chang": df=1, tf=1
  idf = 0.693 (same — also in 1 of 2 docs)
  norm = same (same tf and docLen) = 0.973
  score = 0.674

Term "ocean": df=2, tf=1
  idf = log(1 + (2 - 2 + 0.5) / (2 + 0.5)) = log(1 + 0.2) = log(1.2) = 0.182
  norm = 0.973 (same tf and docLen)
  score = 0.182 × 0.973 = 0.177

BM25 total for doc0 = 0.674 + 0.674 + 0.177 = 1.525
```

"ocean" scores lower than "climat"/"chang" because it appears in BOTH documents (df=2 out of N=2) — it's not a discriminating term. "climat" and "chang" only appear in doc0 — rarer, more informative.

### What k1=1.2 and b=0.75 do

**k1=1.2 — TF saturation:** With tf=1 here score is 0.674. If tf were 50:

```
norm(tf=50) = 50×2.2 / (50 + 1.2×1.05) = 110 / 51.26 = 2.145
score(tf=50) = 0.693 × 2.145 = 1.486   ← barely more than tf=1 (0.674)
```

The 50th mention of "climat" adds almost nothing over the 1st. Prevents keyword-stuffing spam.

**b=0.75 — length normalisation:** If doc0 were 100 tokens instead of 8:

```
norm(docLen=100) = 1×2.2 / (1 + 1.2×(0.25 + 0.75×100/7.5))
                = 2.2 / (1 + 1.2×10.25)
                = 2.2 / 13.3 = 0.165   ← much lower
```

Long articles don't win just by being long.

---

## Step 8 — PageRank Blend

**File:** `internal/link/pagerank.go`

```
prScores = [0.62, 0.38]   ← doc0=Climate Change, doc1=Ocean

prWeight = 1.0

doc0 final score = BM25 + w × log(1 + PR)
                 = 1.525 + 1.0 × log(1 + 0.62)
                 = 1.525 + log(1.62)
                 = 1.525 + 0.482
                 = 2.007
```

`log(1+x)` compression: Without it, a PR of 0.62 vs 0.38 would be a 63% difference in the additive term. With log, the difference is `log(1.62) - log(1.38) = 0.482 - 0.322 = 0.16` — meaningful but doesn't overpower BM25.

**How PageRank got computed for these 2 docs:**

```
Links:
  doc0 (Climate Change) → ["Ocean","Greenhouse gas"]  outdeg=2
  doc1 (Ocean)          → ["Water","Earth"]            outdeg=2

Both pages link outward but neither links to each other in our 2-doc corpus.
With only 2 nodes and no in-links, both converge to ~0.5 each.
Slight difference comes from dangling node redistribution.
```

In the real 250k-doc corpus, "Climate Change" has thousands of incoming links — its PR would be much higher.

---

## Step 9 — Top-k Selection

**File:** `internal/rank/topk.go`

Only one candidate (doc0). k=10. Trivial:

```
heap = [{DocID:0, Score:2.007}]
Drain in reverse: [{DocID:0, Score:2.007}]
```

With 47 real candidates and k=10, the min-heap maintains the 10 highest scores. When a new score arrives: push → if heap size > 10, pop the minimum. O(n log k).

---

## Step 10 — Snippet Generation

**File:** `internal/rank/snippet.go`

```
doc0.Text = "Climate change affects the ocean and plant life globally."
queryTerms = ["climat", "chang", "ocean"]

Split into words:
  [Climate@0, change@5, affects@13, the@21, ocean@25, and@31, plant@35, ...]

Window size = 160 chars / 6 ≈ 26 words. Slide window:

Window at word 0 ("Climate change affects..."):
  "climat" hit at word 0  ✓
  "chang"  hit at word 1  ✓
  "ocean"  hit at word 4  ✓
  → 3 hits (best possible)

Extract text span from word 0 to word 26:
  "Climate change affects the ocean and plant life globally."

Lowercase snippet once: "climate change affects the ocean..."

Highlight "climat":
  strings.Index(lower, "climat") = 0
  orig = "Climate"  (from original case snippet)
  snippet = "\033[1mClimate\033[0m change affects the ocean..."

Highlight "chang":
  strings.Index(lower, "chang") = 8
  orig = "change"
  snippet = "\033[1mClimate\033[0m \033[1mchange\033[0m affects the ocean..."

Highlight "ocean":
  orig = "ocean"
  snippet = "\033[1mClimate\033[0m \033[1mchange\033[0m affects the \033[1mocean\033[0m..."
```

`\033[1m` = ANSI bold on. `\033[0m` = reset. Terminal renders these as **bold**.

---

## Full output

```
> "climate change" AND ocean

found 1 documents in 0.4ms
1. Climate Change (2.0070)
   Climate change affects the ocean and plant life globally.
```

doc1 ("Ocean") is absent because it does not contain the phrase "climate change" — the phrase check eliminated it in Step 6, before scoring even ran.

---

## Complete data flow — one diagram

```
dump file
  │
  │ Reader.Next()
  │   skip odd lines (action), decode even lines (doc JSON)
  │   assign sequential IDs from 0
  ▼
doc0: {ID:0, Title:"Climate Change", Text:"Climate change affects...", Links:[...]}
doc1: {ID:1, Title:"Ocean",          Text:"The ocean is a large...",  Links:[...]}
  │
  │ Analyzer.Analyze(title + " " + text)
  │   tokenize → lowercase → NFC → stopword (gaps preserved) → Porter2 stem
  ▼
doc0 tokens: [{climat,0},{chang,1},{affect,2},{ocean,4},{plant,6},{lif,7},{global,8}]
doc1 tokens: [{ocean,1},{larg,4},{bodi,5},{water,7},{cover,8},{most,9},{earth,11}]
  │
  │ MemoryIndex.Add() — count tf, record positions
  │ MemoryIndex.Finalize() — sort all posting lists by DocID
  ▼
postings["climat"] = [{DocID:0, tf:1, pos:[0]}]
postings["chang"]  = [{DocID:0, tf:1, pos:[1]}]
postings["ocean"]  = [{DocID:0, tf:1, pos:[4]}, {DocID:1, tf:1, pos:[1]}]
  │
  │ WriteSegment()
  │   gap-encode docIDs, varint compress → segment.post
  │   sorted term dict with byte offsets → segment.dict
  │   title+length per doc → segment.docs
  │   PageRank power iteration → pagerank.json
  ▼
data/index/ (persisted to disk)

═══════════════════════════════════════

User types: "climate change" AND ocean
  │
  │ query.Parse()
  │   lex → [PHRASE("climate change"), AND, WORD("ocean"), EOF]
  │   parse → AndNode{PhraseNode["climate","change"], TermNode["ocean"]}
  ▼
AST
  │
  │ evalNode(AndNode):
  │   evalNode(PhraseNode):
  │     Lookup("climat") → [{DocID:0, pos:[0]}]
  │     Lookup("chang")  → [{DocID:0, pos:[1]}]
  │     PhraseIntersect(gap=1): pos[0]+1=1 == pos[1][0]=1 → doc0 matches
  │     result: [{DocID:0}]
  │
  │   evalNode(TermNode["ocean"]):
  │     Lookup("ocean") → [{DocID:0}, {DocID:1}]
  │
  │   Intersect(phrase_result, ocean_result):
  │     two-pointer: docID 0 in both → emit; docID 1 only in ocean → skip
  │     result: [{DocID:0}]
  ▼
candidates: [{DocID:0}]
  │
  │ Cache term posting lists (once per query, not per doc):
  │   termPLs["climat"] = {DocFreq:1}
  │   termPLs["chang"]  = {DocFreq:1}
  │   termPLs["ocean"]  = {DocFreq:2}
  │
  │ BM25.Score(entry, docFreq, DocLen) per term, sum:
  │   score("climat") = 0.674
  │   score("chang")  = 0.674
  │   score("ocean")  = 0.177
  │   bm25_total      = 1.525
  │
  │ PageRank blend:
  │   final = 1.525 + 1.0 × log(1 + 0.62) = 2.007
  ▼
results: [{DocID:0, Score:2.007}]
  │
  │ TopK(k=10): 1 result fits, return as-is
  │
  │ Snippet(doc0.Text, queryTerms):
  │   best window → "Climate change affects the ocean and plant life globally."
  │   highlight → "\033[1mClimate\033[0m \033[1mchange\033[0m affects the \033[1mocean\033[0m..."
  ▼
1. Climate Change (2.0070)
   Climate change affects the ocean and plant life globally.
```

---

## Step 11 — Elasticsearch Comparison

**Files:** `cmd/esload/main.go`, `cmd/compare/main.go`

After building your own engine, load the same corpus into Elasticsearch and run the same 15 queries against both.

### Loading into ES (`cmd/esload`)

Creates an index with the `english` analyzer, then bulk-loads in NDJSON pairs:

```
Action line:   {"index":{"_id":"0"}}
Document line: {"title":"Climate Change","text":"Climate change affects the ocean..."}
Action line:   {"index":{"_id":"1"}}
Document line: {"title":"Ocean","text":"The ocean is a large body..."}
```

The `english` analyzer uses the same core ideas as ours — lowercase, stopwords, Porter stemming — but is a dictionary-backed production implementation with more edge cases handled correctly.

Loading with `refresh_interval: -1` disables per-document refresh. ES merges segments and refreshes once at the end — faster. Analogous to our `Finalize()` sorting all posting lists once, not after each `Add()`.

### Running the comparison (`cmd/compare`)

For each of the 15 queries in `testdata/queries.json`:

**My engine:**
```
analyze("climate change") → ["climat", "chang"]
Lookup("climat") → candidates
Intersect with Lookup("chang") → filtered
BM25 score each + PageRank blend → TopK(10)
```

**Elasticsearch — HTTP call:**
```json
POST localhost:9200/wiki/_search
{
  "query": {
    "multi_match": {
      "query": "climate change",
      "fields": ["title^2", "text"],
      "type": "best_fields"
    }
  }
}
```

Both produce a ranked list of 10 titles. Compute P@10 / MRR / NDCG@10 against the same relevance judgments. Print side-by-side.

### What the output looks like

```
my engine: loaded segment (10000 docs)
elasticsearch: connected at http://localhost:9200

ranker                   P@10     MRR  NDCG@10
--------------------------------------------------
mine (bm25+pagerank)    0.620   0.780    0.710
elasticsearch           0.680   0.810    0.750

query                            my NDCG   es NDCG  winner
--------------------------------------------------------------
photosynthesis                     0.850     0.910  es ✓
world war two                      0.720     0.680  mine ✓
mercury                            0.450     0.600  es ✓
...
```

### Why ES usually wins

**1. Field boosts.** `multi_match` with `title^2` gives title matches 2× weight. Our engine scores both fields with a flat BM25. Adding field-weighted scoring would close this gap.

**2. Irregular word forms.** Porter2 stems `"went"` to `"went"` — no rule matches. ES's `english` analyzer dictionary maps it to `"go"`, correctly matching documents about "going". These silent misses compound across a large corpus.

**3. Production-quality statistics.** ES tracks exact per-segment document frequencies and normalises across them. Our `segmentIndex` works correctly on a single segment but would diverge on a multi-segment index.

### Verifying the analyzers match

```bash
# What ES produces for the same text
curl -s 'localhost:9200/wiki/_analyze' \
  -H 'Content-Type: application/json' \
  -d '{"analyzer":"english","text":"The running dogs jumped quickly"}' | jq .

# Expected from ours: [run, dog, jump, quick]
# ES may differ on: irregular plurals, possessives, hyphenated words
```

### Verifying BM25 scores match

```bash
# ES score breakdown for doc 0, term "photosynthesis"
curl 'localhost:9200/wiki/_explain/0' \
  -H 'Content-Type: application/json' \
  -d '{"query":{"match":{"text":"photosynthesis"}}}' | jq .

# Response includes: idf value, tf, field length, avgFieldLength
# Compare each against your BM25 — divergence pinpoints the bug
```
