# wikisearch — Deep Dive

One query. Three documents. Every step from raw bytes to ranked output — including what gets eliminated and exactly why.

---

## The Setup

**Query:** `"climate change" AND ocean NOT pollution`

**Three documents:**

```
doc0  title="Climate Change"
      text="Climate change affects the ocean and plant life globally."
      links=["Ocean", "Greenhouse gas", "Carbon dioxide"]

doc1  title="Ocean"
      text="The ocean is a large body of water. Ocean pollution affects marine life."
      links=["Water", "Marine biology", "Climate Change"]

doc2  title="Ocean Pollution"
      text="Ocean pollution is caused by industrial waste and climate change."
      links=["Ocean", "Industrial waste", "Climate Change"]
```

**What should happen:**

- doc0 → has the phrase "climate change" + has "ocean" + no "pollution" → **should match**
- doc1 → has "ocean" but NOT the phrase "climate change" adjacent → **eliminated at phrase step**
- doc2 → has the phrase + ocean BUT also has "pollution" → **eliminated at NOT step**

---

## Step 1 — Ingestion

**File:** `internal/corpus/reader.go`

The dump file is compressed JSON. Every two lines = one document:

```
Line 1 (odd)  → {"index":{"_id":"111"}}
Line 2 (even) → {"title":"Climate Change","text":"Climate change affects the ocean...","outgoing_link":["Ocean","Greenhouse gas","Carbon dioxide"]}
Line 3 (odd)  → {"index":{"_id":"222"}}
Line 4 (even) → {"title":"Ocean","text":"The ocean is a large body...","outgoing_link":["Water","Marine biology","Climate Change"]}
Line 5 (odd)  → {"index":{"_id":"333"}}
Line 6 (even) → {"title":"Ocean Pollution","text":"Ocean pollution is caused...","outgoing_link":["Ocean","Industrial waste","Climate Change"]}
```

The reader:

```go
sc := bufio.NewScanner(raw)
sc.Buffer(make([]byte, 1024*1024), 10*1024*1024)  // 10MB — some articles are huge

for sc.Scan() {
    r.line++
    if r.line%2 == 1 { continue }  // skip action lines (odd)
    json.Unmarshal(r.sc.Bytes(), &raw)
    doc.ID = r.nextID               // sequential, not Wikipedia's ID
    r.nextID++
}
```

Result after reading all 6 lines:

```
Document{ID:0, Title:"Climate Change",   Text:"Climate change affects...", Links:["Ocean","Greenhouse gas","Carbon dioxide"]}
Document{ID:1, Title:"Ocean",            Text:"The ocean is a large...",   Links:["Water","Marine biology","Climate Change"]}
Document{ID:2, Title:"Ocean Pollution",  Text:"Ocean pollution is caused...",Links:["Ocean","Industrial waste","Climate Change"]}
```

**Why sequential IDs, not Wikipedia IDs:** Wikipedia IDs have gaps (deleted articles, redirects). Sequential IDs starting from 0 allow doc metadata to be stored as a flat array — `docs[id]` is an O(1) array access. The Wikipedia ID is irrelevant once we have our own.

---

## Step 2 — Text Analysis

**File:** `internal/analysis/`

Identical pipeline for index-time (on document text) and query-time (on query string). Any difference = silent misses.

### 2.1 What gets analyzed

```
doc0: "Climate Change" + " " + "Climate change affects the ocean and plant life globally."
    = "Climate Change Climate change affects the ocean and plant life globally."

doc1: "Ocean" + " " + "The ocean is a large body of water. Ocean pollution affects marine life."
    = "Ocean The ocean is a large body of water. Ocean pollution affects marine life."

doc2: "Ocean Pollution" + " " + "Ocean pollution is caused by industrial waste and climate change."
    = "Ocean Pollution Ocean pollution is caused by industrial waste and climate change."
```

Title is prepended to text so title terms get higher TF naturally.

### 2.2 Tokenize

Split on non-letter/non-digit runs using `unicode.IsLetter` (not ASCII — handles accented chars). Record ordinal position starting at 0.

**doc0:**

```
"Climate Change Climate change affects the ocean and plant life globally."

Tokens:
  {Climate,0} {Change,1} {Climate,2} {change,3} {affects,4} {the,5}
  {ocean,6} {and,7} {plant,8} {life,9} {globally,10}
```

**doc1:**

```
"Ocean The ocean is a large body of water Ocean pollution affects marine life."

Tokens:
  {Ocean,0} {The,1} {ocean,2} {is,3} {a,4} {large,5} {body,6}
  {of,7} {water,8} {Ocean,9} {pollution,10} {affects,11} {marine,12} {life,13}
```

**doc2:**

```
"Ocean Pollution Ocean pollution is caused by industrial waste and climate change."

Tokens:
  {Ocean,0} {Pollution,1} {Ocean,2} {pollution,3} {is,4} {caused,5}
  {by,6} {industrial,7} {waste,8} {and,9} {climate,10} {change,11}
```

### 2.3 Lowercase

`strings.ToLower` on every term:

**doc0:** `{climate,0} {change,1} {climate,2} {change,3} {affects,4} {the,5} {ocean,6} {and,7} {plant,8} {life,9} {globally,10}`

**doc1:** `{ocean,0} {the,1} {ocean,2} {is,3} {a,4} {large,5} {body,6} {of,7} {water,8} {ocean,9} {pollution,10} {affects,11} {marine,12} {life,13}`

**doc2:** `{ocean,0} {pollution,1} {ocean,2} {pollution,3} {is,4} {caused,5} {by,6} {industrial,7} {waste,8} {and,9} {climate,10} {change,11}`

### 2.4 NFC Normalize

`norm.NFC.String()` — forces Unicode composed form. `é` as two codepoints (`e` + combining accent) → single `U+00E9`. No visible change on ASCII text. Matters for Wikipedia articles in French, Spanish, German.

### 2.5 Stopword Filter

Drop common function words. **Do NOT renumber positions — leave gaps.**

Stopwords hit: `the`, `is`, `a`, `of`, `and`, `by`

**doc0 before:** `{climate,0} {change,1} {climate,2} {change,3} {affects,4} {THE,5} {ocean,6} {AND,7} {plant,8} {life,9} {globally,10}`

**doc0 after:** `{climate,0} {change,1} {climate,2} {change,3} {affects,4}          {ocean,6}          {plant,8} {life,9} {globally,10}`

```
                                                                            ↑ gap                   ↑ gap
                                                                         pos 5 gone              pos 7 gone
```

**doc1 before:** `{ocean,0} {THE,1} {ocean,2} {IS,3} {A,4} {large,5} {body,6} {OF,7} {water,8} {ocean,9} {pollution,10} {affects,11} {marine,12} {life,13}`

**doc1 after:** `{ocean,0}          {ocean,2}               {large,5} {body,6}          {water,8} {ocean,9} {pollution,10} {affects,11} {marine,12} {life,13}`

**doc2 before:** `{ocean,0} {pollution,1} {ocean,2} {pollution,3} {IS,4} {caused,5} {BY,6} {industrial,7} {waste,8} {AND,9} {climate,10} {change,11}`

**doc2 after:** `{ocean,0} {pollution,1} {ocean,2} {pollution,3}          {caused,5}          {industrial,7} {waste,8}          {climate,10} {change,11}`

**Why gaps matter for phrase queries:**
In doc2, "climate" is at position 10 and "change" at position 11. Gap = 1 → adjacent → phrase match.
If positions were renumbered after dropping stopwords, the positions would shift and the phrase check would break.

### 2.6 Porter2 Stem

Applied after lowercase. Key rule groups:

| Input        | Steps applied                                                               | Output     |
| ------------ | --------------------------------------------------------------------------- | ---------- |
| `climate`    | Step 5a: ends in `e` in R1 → strip                                          | `climat`   |
| `change`     | Step 5a: ends in `e` in R1 → strip                                          | `chang`    |
| `affects`    | Step 1a: ends in `s`, vowel before → strip                                  | `affect`   |
| `ocean`      | No rules match                                                              | `ocean`    |
| `plant`      | No rules match                                                              | `plant`    |
| `life`       | Step 5a: ends in `e` in R1 → strip                                          | `lif`      |
| `globally`   | Step 1c: `y`→`i` → `globalli`; Step 2: ends in `li`, `l` valid → strip `li` | `global`   |
| `pollution`  | Step 4: ends in `ion` in R2, preceded by `t` → strip                        | `pollut`   |
| `large`      | Step 5a: ends in `e` in R1 → strip                                          | `larg`     |
| `body`       | Step 1c: `y`→`i`, consonant before → `bodi`                                 | `bodi`     |
| `water`      | No rules match                                                              | `water`    |
| `marine`     | Step 5a: ends in `e` in R1 → strip                                          | `marin`    |
| `caused`     | Step 1b: ends in `ed`, vowel before → strip → `caus`                        | `caus`     |
| `industrial` | Step 3: ends in `al` in R1 → strip → `industri`                             | `industri` |
| `waste`      | Step 5a: ends in `e` in R1 → strip                                          | `wast`     |

**Final token streams:**

```
doc0: [{climat,0},{chang,1},{climat,2},{chang,3},{affect,4},{ocean,6},{plant,8},{lif,9},{global,10}]

doc1: [{ocean,0},{ocean,2},{larg,5},{bodi,6},{water,8},{ocean,9},{pollut,10},{affect,11},{marin,12},{lif,13}]

doc2: [{ocean,0},{pollut,1},{ocean,2},{pollut,3},{caus,5},{industri,7},{wast,8},{climat,10},{chang,11}]
```

---

## Step 3 — Building the Inverted Index

**File:** `internal/index/memory.go`

`MemoryIndex.Add()` processes each document. For each term, count `TermFreq` and record `Positions`:

**After doc0 (ID=0):**

```
postings["climat"] = [{DocID:0, tf:2, pos:[0,2]}]    ← "Climate Change Climate change..."
postings["chang"]  = [{DocID:0, tf:2, pos:[1,3]}]
postings["affect"] = [{DocID:0, tf:1, pos:[4]}]
postings["ocean"]  = [{DocID:0, tf:1, pos:[6]}]
postings["plant"]  = [{DocID:0, tf:1, pos:[8]}]
postings["lif"]    = [{DocID:0, tf:1, pos:[9]}]
postings["global"] = [{DocID:0, tf:1, pos:[10]}]
```

**After doc1 (ID=1):**

```
postings["ocean"]  = [{DocID:0, tf:1, pos:[6]}, {DocID:1, tf:3, pos:[0,2,9]}]
postings["larg"]   = [{DocID:1, tf:1, pos:[5]}]
postings["bodi"]   = [{DocID:1, tf:1, pos:[6]}]
postings["water"]  = [{DocID:1, tf:1, pos:[8]}]
postings["pollut"] = [{DocID:1, tf:1, pos:[10]}]
postings["affect"] = [{DocID:0, tf:1, pos:[4]}, {DocID:1, tf:1, pos:[11]}]
postings["marin"]  = [{DocID:1, tf:1, pos:[12]}]
postings["lif"]    = [{DocID:0, tf:1, pos:[9]}, {DocID:1, tf:1, pos:[13]}]
```

**After doc2 (ID=2):**

```
postings["ocean"]  = [{DocID:0, tf:1, pos:[6]}, {DocID:1, tf:3, pos:[0,2,9]}, {DocID:2, tf:2, pos:[0,2]}]
postings["pollut"] = [{DocID:1, tf:1, pos:[10]}, {DocID:2, tf:2, pos:[1,3]}]
postings["caus"]   = [{DocID:2, tf:1, pos:[5]}]
postings["industri"]= [{DocID:2, tf:1, pos:[7]}]
postings["wast"]   = [{DocID:2, tf:1, pos:[8]}]
postings["climat"] = [{DocID:0, tf:2, pos:[0,2]}, {DocID:2, tf:1, pos:[10]}]
postings["chang"]  = [{DocID:0, tf:2, pos:[1,3]}, {DocID:2, tf:1, pos:[11]}]
```

**After `Finalize()` — sort every posting list by DocID ascending:**

```
postings["climat"] = [{DocID:0, tf:2, pos:[0,2]}, {DocID:2, tf:1, pos:[10]}]
postings["chang"]  = [{DocID:0, tf:2, pos:[1,3]}, {DocID:2, tf:1, pos:[11]}]
postings["ocean"]  = [{DocID:0, tf:1, pos:[6]}, {DocID:1, tf:3, pos:[0,2,9]}, {DocID:2, tf:2, pos:[0,2]}]
postings["pollut"] = [{DocID:1, tf:1, pos:[10]}, {DocID:2, tf:2, pos:[1,3]}]
```

This sort is the invariant that enables all downstream two-pointer merge operations.

---

## Step 4 — Writing to Disk

**File:** `internal/index/writer.go`

Three files per segment. Written once; loaded on every subsequent startup.

### segment.post — Gap-encoded varint posting data

Terms written in alphabetical order. DocIDs gap-encoded (delta from previous docID):

```
Term "affect": docFreq=2, entries=[{0,tf:1},{1,tf:1}]

buf = [02]          ← varint(docFreq=2)
      [00][01]      ← varint(gap=0, first entry), varint(tf=1)  → DocID 0
      [01][01]      ← varint(gap=1-0=1), varint(tf=1)           → DocID 1

7 bytes at offset 0
```

```
Term "chang": docFreq=2, entries=[{0,tf:2},{2,tf:1}]

buf = [02]          ← docFreq=2
      [00][02]      ← gap=0, tf=2    → DocID 0
      [02][01]      ← gap=2-0=2, tf=1 → DocID 2

7 bytes at offset 7
```

```
Term "climat": docFreq=2, entries=[{0,tf:2},{2,tf:1}]

buf = [02][00][02][02][01]

7 bytes at offset 14
```

```
Term "ocean": docFreq=3, entries=[{0,tf:1},{1,tf:3},{2,tf:2}]

buf = [03]          ← docFreq=3
      [00][01]      ← gap=0,  tf=1   → DocID 0
      [01][03]      ← gap=1,  tf=3   → DocID 1
      [01][02]      ← gap=1,  tf=2   → DocID 2

9 bytes at offset 21
```

```
Term "pollut": docFreq=2, entries=[{1,tf:1},{2,tf:2}]

buf = [02][01][01][01][02]

7 bytes at offset 30
```

**Varint encoding** — bit 8 of each byte is "more follows" flag, bits 1-7 carry value:

```
value 0   → [00000000]           = 1 byte
value 1   → [00000001]           = 1 byte
value 127 → [01111111]           = 1 byte
value 128 → [10000000][00000001] = 2 bytes
value 300 → [10101100][00000010] = 2 bytes
```

Values < 128 = 1 byte. DocID gaps between adjacent docs = 1 = 1 byte. Fixed uint32 = always 4 bytes. ~4× savings on dense posting lists.

### segment.dict — Term directory

```
[uint32 numTerms=5]

[uint16 len=6]["affect"][uint64 offset=0 ][uint64 len=7]
[uint16 len=5]["chang" ][uint64 offset=7 ][uint64 len=7]
[uint16 len=6]["climat"][uint64 offset=14][uint64 len=7]
[uint16 len=5]["ocean" ][uint64 offset=21][uint64 len=9]
[uint16 len=6]["pollut"][uint64 offset=30][uint64 len=7]
```

Loaded into memory as `map[string][2]uint64`. Lookup: hash → (offset, length) → slice `postData[offset:offset+length]` → decode varint. O(1), no disk I/O per query.

### segment.docs — Document metadata

Line number = docID:

```
line 0 → {"t":"Climate Change","l":9}
line 1 → {"t":"Ocean","l":10}
line 2 → {"t":"Ocean Pollution","l":11}
```

Only `title` and token count `length` stored — everything needed for display and BM25 length normalisation.

### pagerank.json — PageRank scores

```json
[0.45, 0.32, 0.23]
```

doc0 = 0.45, doc1 = 0.32, doc2 = 0.23. Computed after indexing. (See Step 9.)

---

## Step 5 — Startup

**File:** `cmd/search/main.go`

**Slow path (first time or no segment):**

```bash
go run ./cmd/search -dump data/sample.json.gz
```

Rebuilds MemoryIndex from scratch. Reads dump → analyzes → builds posting map → Finalize. Minutes on full corpus.

**Fast path (pre-built segment):**

```bash
go run ./cmd/search -index data/index
```

```go
idx, err := index.OpenSegment(*idxDir)   // milliseconds
```

Reads `segment.dict` into map, reads `segment.post` into `[]byte`, scans `segment.docs` line-by-line. No analysis, no posting list sorting. Same `index.Index` interface — query code is identical.

Loading `pagerank.json` if present → `[]float64` array.

---

## Step 6 — Query Parsing

**File:** `internal/query/`

**Input:** `"climate change" AND ocean NOT pollution`

### 6.1 Lexer

Scans character by character:

```
'"'          → start phrase scan → scan to closing '"'
              phrase = "climate change"
              emit: PHRASE("climate change")
' '          → skip
'A','N','D'  → scan word → "AND" keyword
              emit: AND
' '          → skip
'o'...'n'    → scan word → "ocean"
              emit: WORD("ocean")
' '          → skip
'N','O','T'  → scan word → "NOT" keyword
              emit: NOT
' '          → skip
'p'...'n'    → scan word → "pollution"
              emit: WORD("pollution")
EOF          → emit: EOF
```

**Token stream:**

```
[PHRASE("climate change"), AND, WORD("ocean"), NOT, WORD("pollution"), EOF]
```

### 6.2 Recursive Descent Parser

Grammar (higher = higher precedence):

```
expr   := term (OR term)*
term   := factor (AND? factor)*
factor := NOT? atom
atom   := WORD | PHRASE | FIELD:atom | '(' expr ')'
```

```
parseExpr()
  → parseTerm()
       → parseFactor()
            → parseAtom() → consume PHRASE → PhraseNode{Terms:["climate","change"]}
         peek=AND → consume AND
       → parseFactor()
            → parseAtom() → consume WORD("ocean") → TermNode{Term:"ocean"}
         peek=NOT
       → parseFactor()
            → consume NOT
            → parseAtom() → consume WORD("pollution") → TermNode{Term:"pollution"}
            → return NotNode{Child: TermNode{"pollution"}}
         peek=EOF → stop term
         3 children → AndNode{PhraseNode, TermNode, NotNode}
  peek=EOF → no OR
  return AndNode
```

**AST:**

```
AndNode
├── PhraseNode{Terms:["climate","change"]}
├── TermNode{Term:"ocean"}
└── NotNode
    └── TermNode{Term:"pollution"}
```

---

## Step 7 — AST Evaluation

**File:** `cmd/search/main.go` → `evalNode()`

The AST is walked recursively. Each node type produces a `postings.List`.

### 7.1 evalNode(PhraseNode["climate","change"])

```
analyze("climate") → [{Term:"climat", Position:0}]
analyze("change")  → [{Term:"chang",  Position:0}]

Lookup("climat"):
  dict["climat"] → {offset:14, length:7}
  decode postData[14:21] → List{DocFreq:2, Entries:[{DocID:0,tf:2,pos:[0,2]}, {DocID:2,tf:1,pos:[10]}]}

Lookup("chang"):
  dict["chang"] → {offset:7, length:7}
  decode postData[7:14] → List{DocFreq:2, Entries:[{DocID:0,tf:2,pos:[1,3]}, {DocID:2,tf:1,pos:[11]}]}

PhraseIntersect(climat_list, chang_list, gap=1):
```

**Two-pointer over docs, position check for matches:**

```
i=0 (DocID:0), j=0 (DocID:0) → same doc

  Check positions: posA=[0,2], posB=[1,3], gap=1
    p=0, q=0: posA[0]+1 = 0+1 = 1 == posB[0]=1 → MATCH ✓
    Doc0 matches phrase!

i=1 (DocID:2), j=1 (DocID:2) → same doc

  Check positions: posA=[10], posB=[11], gap=1
    p=0, q=0: posA[0]+1 = 10+1 = 11 == posB[0]=11 → MATCH ✓
    Doc2 matches phrase!

phraseResult = [{DocID:0}, {DocID:2}]
```

**doc1 is absent** from both posting lists — it never contains "climat" at all (doc1 text was "The ocean is a large body..."). Correctly excluded.

### 7.2 evalNode(TermNode["ocean"])

```
analyze("ocean") → [{Term:"ocean", Position:0}]

Lookup("ocean"):
  dict["ocean"] → {offset:21, length:9}
  decode postData[21:30]:
    docFreq=3
    gap=0→DocID=0, tf=1
    gap=1→DocID=1, tf=3
    gap=1→DocID=2, tf=2

oceanResult = [{DocID:0,tf:1}, {DocID:1,tf:3}, {DocID:2,tf:2}]
```

All 3 docs contain "ocean".

### 7.3 evalNode(NotNode) → returns pollution list

```
analyze("pollution") → [{Term:"pollut", Position:0}]

Lookup("pollut"):
  decode → List{Entries:[{DocID:1,tf:1}, {DocID:2,tf:2}]}

pollutionResult = [{DocID:1}, {DocID:2}]
```

### 7.4 evalNode(AndNode) — combining all three

**Step A: Intersect(phraseResult, oceanResult)**

```
phraseResult = [{DocID:0}, {DocID:2}]
oceanResult  = [{DocID:0}, {DocID:1}, {DocID:2}]

Two-pointer merge:
  i=0 (0), j=0 (0): match → emit DocID:0, advance both
  i=1 (2), j=1 (1): 2 > 1 → advance j
  i=1 (2), j=2 (2): match → emit DocID:2, advance both
  i=2: end

andResult = [{DocID:0}, {DocID:2}]
```

**Step B: Difference(andResult, pollutionResult)** — remove NOT terms

```
andResult       = [{DocID:0}, {DocID:2}]
pollutionResult = [{DocID:1}, {DocID:2}]

Two-pointer:
  i=0 (0), j=0 (1): 0 < 1 → emit DocID:0, advance i
  i=1 (2), j=0 (1): 2 > 1 → advance j
  i=1 (2), j=1 (2): match → skip (this is a NOT), advance both
  i=2: end

finalResult = [{DocID:0}]
```

**Elimination summary:**
```
doc0 (Climate Change):   ✓ phrase match + ✓ has ocean + ✓ no pollution → SURVIVES
doc1 (Ocean):            ✗ no phrase match → eliminated at Step 7.1
doc2 (Ocean Pollution):  ✓ phrase match + ✓ has ocean + ✗ has pollution → eliminated at Step 7.4B
```

---

## Step 8 — BM25 Scoring

**File:** `internal/rank/bm25.go`

Only doc0 survived. Score it for all three query terms.

**Corpus stats:**
```
N     = 3       (total documents)
avgdl = (9 + 10 + 11) / 3 = 10   (avg token count across all 3 docs)
doc0_len = 9    (token count after analysis)
k1 = 1.2, b = 0.75
```

**Cache posting lists once before the scoring loop:**
```go
termPLs := make(map[string]postings.List)
termPLs["climat"] = {DocFreq:2}   ← found in 2 of 3 docs
termPLs["chang"]  = {DocFreq:2}
termPLs["ocean"]  = {DocFreq:3}   ← found in all 3 docs
```

This lookup happens once per query term (3 lookups), not once per candidate per term (which would be 3 × however many candidates).

**Scoring doc0 for term "climat":**
```
df = 2 (appears in doc0 and doc2)
tf = 2 (appears twice in doc0: "Climate Change Climate change...")
dl = 9

idf = log(1 + (N - df + 0.5) / (df + 0.5))
    = log(1 + (3 - 2 + 0.5) / (2 + 0.5))
    = log(1 + 1.5/2.5)
    = log(1 + 0.6)
    = log(1.6) = 0.470

norm = tf × (k1+1) / (tf + k1 × (1 - b + b × dl/avgdl))
     = 2 × 2.2 / (2 + 1.2 × (1 - 0.75 + 0.75 × 9/10))
     = 4.4 / (2 + 1.2 × (0.25 + 0.675))
     = 4.4 / (2 + 1.2 × 0.925)
     = 4.4 / (2 + 1.11)
     = 4.4 / 3.11 = 1.415

score("climat") = 0.470 × 1.415 = 0.665
```

**Scoring doc0 for term "chang":**

```
df = 2, tf = 2, dl = 9   (same as "climat" — both appear twice)
idf = 0.470
norm = 1.415
score("chang") = 0.665
```

**Scoring doc0 for term "ocean":**

```
df = 3 (appears in ALL docs)
tf = 1 (appears once in doc0)
dl = 9

idf = log(1 + (3 - 3 + 0.5) / (3 + 0.5))
    = log(1 + 0.5/3.5)
    = log(1 + 0.143)
    = log(1.143) = 0.134   ← MUCH lower — not discriminating

norm = 1 × 2.2 / (1 + 1.2 × 0.925)
     = 2.2 / 2.11 = 1.043

score("ocean") = 0.134 × 1.043 = 0.140
```

**Why "ocean" scores low:** IDF measures rarity. "ocean" appears in all 3 documents → `df=N=3` → IDF near zero. It's not a discriminating term — finding it in doc0 tells you nothing about whether doc0 is relevant. This is correct: the query is about "climate change", not "ocean" generically.

**Total BM25 for doc0:**

```
0.665 + 0.665 + 0.140 = 1.470
```

**k1=1.2 effect on "climat" (tf=2 vs hypothetical tf=10):**

```
tf=2:  norm = 2×2.2 / (2 + 1.11)  = 4.4 / 3.11  = 1.415
tf=10: norm = 10×2.2 / (10 + 1.11) = 22 / 11.11 = 1.980  ← barely 40% more for 5× the occurrences
tf=50: norm = 50×2.2 / (50 + 1.11) = 110 / 51.11 = 2.152 ← almost no increase from tf=10 to tf=50
```

TF saturates → keyword stuffing doesn't work.

**b=0.75 effect — if doc0 were 100 tokens instead of 9:**

```
norm(dl=100) = 2×2.2 / (2 + 1.2×(0.25 + 0.75×100/10))
             = 4.4 / (2 + 1.2×7.75)
             = 4.4 / 11.3 = 0.389   ← vs 1.415 for dl=9
```

Long articles are penalised — the same 2 occurrences in a 100-token article count far less than in a 9-token article.

---

## Step 9 — PageRank

**File:** `internal/link/pagerank.go`

Computed once at index time. Link graph from `outgoing_link` field:

```
doc0 (Climate Change) → links to: doc1 (Ocean), [Greenhouse gas (redlink)], [Carbon dioxide (redlink)]
                        valid outlinks = [doc1]     outdeg = 1

doc1 (Ocean)          → links to: [Water (redlink)], [Marine biology (redlink)], doc0 (Climate Change)
                        valid outlinks = [doc0]     outdeg = 1

doc2 (Ocean Pollution) → links to: doc0 (Climate Change), doc1 (Ocean), [Industrial waste (redlink)]
                         valid outlinks = [doc0, doc1]  outdeg = 2
```

Red links = titles not in our 3-doc corpus → ignored.

**In-link lists (precomputed before iteration):**

```
inLinks[doc0] = [doc1, doc2]    ← both doc1 and doc2 link to doc0
inLinks[doc1] = [doc0, doc2]    ← doc0 and doc2 link to doc1
inLinks[doc2] = []              ← nobody links to doc2
```

**Initial scores:** `[1/3, 1/3, 1/3] = [0.333, 0.333, 0.333]`

**Iteration 1 (d=0.85, N=3):**

Dangling check: doc2 has outlinks (doc0, doc1), doc0 has outlinks (doc1), doc1 has outlinks (doc0). No dangling nodes. `dangling = 0`.

```
PR(doc0) = (1-0.85)/3 + 0.85 × (PR(doc1)/outdeg(doc1) + PR(doc2)/outdeg(doc2))
         = 0.05 + 0.85 × (0.333/1 + 0.333/2)
         = 0.05 + 0.85 × (0.333 + 0.167)
         = 0.05 + 0.85 × 0.500
         = 0.05 + 0.425 = 0.475

PR(doc1) = 0.05 + 0.85 × (PR(doc0)/1 + PR(doc2)/2)
         = 0.05 + 0.85 × (0.333 + 0.167)
         = 0.475

PR(doc2) = 0.05 + 0.85 × 0
         = 0.05   ← nobody links to doc2
```

**After 30 iterations (converged):**

```
doc0 (Climate Change):  ~0.45  ← gets links from doc1 and doc2
doc1 (Ocean):           ~0.45  ← gets links from doc0 and doc2
doc2 (Ocean Pollution): ~0.05  ← nobody links here (only the teleport floor)
```

Scores sum to ~1.0.

In the full 250k-doc corpus, "Climate Change" would receive hundreds of incoming links from articles about greenhouse gas, global warming, IPCC, etc. Its PageRank would be far higher.

---

## Step 10 — Final Score Blend

**File:** `cmd/search/main.go`

```
final = bm25 + prWeight × log(1 + pagerank)

prWeight = 1.0 (default, tunable via -pr-weight flag)

doc0: BM25=1.470, PR=0.45
  final = 1.470 + 1.0 × log(1 + 0.45)
        = 1.470 + log(1.45)
        = 1.470 + 0.372
        = 1.842
```

**Why `log(1+x)` not raw PageRank:**

PageRank values span orders of magnitude. In the full corpus:

- A major article: PR ≈ 0.001
- A top-linked article: PR ≈ 0.0001 (sounds small but it's 10× average)
- A stub article: PR ≈ 0.000001

Raw values are tiny decimals. Without log, the PageRank term barely registers. With log: `log(1.001) = 0.001` vs `log(1.0001) = 0.0001` — the ratio is preserved but at a scale that competes meaningfully with BM25 scores.

If doc2 had not been eliminated by NOT, its score would be:

```
doc2: BM25≈1.3 (phrase matches + ocean matches), PR=0.05
  final = 1.3 + log(1.05) = 1.3 + 0.049 = 1.349
```

doc0 would still win, but the gap would narrow for high-PageRank low-BM25 articles.

---

## Step 11 — Top-k Selection

**File:** `internal/rank/topk.go`

Only 1 candidate (doc0). With k=10, trivial.

In a real query with 47 candidates and k=10:

```
min-heap of size 10:
  Process doc3  score=0.82: heap=[0.82]                  size=1
  Process doc7  score=1.20: heap=[0.82, 1.20]            size=2
  Process doc0  score=1.84: heap=[0.82, 1.20, 1.84]      size=3
  ...
  (heap fills to 10)
  heap=[0.60, 0.71, 0.82, 0.90, 1.00, 1.10, 1.20, 1.40, 1.60, 1.84]
         ↑ minimum sits at top (min-heap property)

  Process docN  score=0.45: 0.45 < heap_min=0.60 → discard
  Process docM  score=2.10: push → heap_min=0.60 pops
                             new heap min = 0.71
  ...

Drain heap (pop gives ascending order → reverse for descending):
  [1.84, 1.60, 1.40, 1.20, 1.10, 1.00, 0.90, 0.82, 0.71, 2.10]
→ sort descending:
  [2.10, 1.84, 1.60, ...]
```

O(n log k) — much cheaper than sorting all 47 results O(n log n) when k=10.

---

## Step 12 — Snippet Generation

**File:** `internal/rank/snippet.go`

```
doc0.Text = "Climate change affects the ocean and plant life globally."
queryTerms = ["climat", "chang", "ocean"]

Split into words with start positions:
  [Climate@0, change@8, affects@15, the@23, ocean@27, and@33, plant@37, life@43, globally@48]

windowSize = 160 → word window ≈ 160/6 = 26 words
(Only 9 words in doc0, so entire text fits in window)

Window at word 0 hits: "climat"@0 ✓, "chang"@1 ✓, "ocean"@4 ✓ → 3 hits (max)

Extract text from words[0].start to words[8].end:
  "Climate change affects the ocean and plant life globally."

Lowercase once: "climate change affects the ocean and plant life globally."

Highlight loop:
  term "climat":
    Index(lower, "climat") = 0  → orig = "Climate" (from original)
    ReplaceAll: "\033[1mClimate\033[0m change affects..."

  term "chang":
    Index(lower, "chang") = 8  → orig = "change"
    ReplaceAll: "\033[1mClimate\033[0m \033[1mchange\033[0m affects..."

  term "ocean":
    Index(lower, "ocean") = 27  → orig = "ocean"
    ReplaceAll: "\033[1mClimate\033[0m \033[1mchange\033[0m affects the \033[1mocean\033[0m..."
```

`\033[1m` = ANSI bold on. `\033[0m` = reset. Renders as **bold** in terminal.

`ReplaceAll` (not `Replace`) ensures ALL occurrences are highlighted. If "ocean" appeared three times, all three get bold.

---

## Final Output

```
> "climate change" AND ocean NOT pollution

found 1 documents in 0.6ms
1. Climate Change (1.8420)
   Climate change affects the ocean and plant life globally.
```

**What was eliminated and when:**

| Document               | Eliminated at              | Reason                                                          |
| ---------------------- | -------------------------- | --------------------------------------------------------------- |
| doc1 (Ocean)           | Step 7.1 — PhraseIntersect | No "climat" in posting lists — never contained the phrase       |
| doc2 (Ocean Pollution) | Step 7.4B — Difference     | Posting list for "pollut" includes doc2 — NOT clause removes it |
| doc0 (Climate Change)  | —                          | Matches all conditions — survives                               |

---

## Complete Data Flow

```
dump file
  │
  │ Reader.Next() — skip odd lines, decode even, assign sequential IDs
  ▼
doc0={ID:0, Title:"Climate Change",   Text:"Climate change affects..."}
doc1={ID:1, Title:"Ocean",            Text:"The ocean is a large..."}
doc2={ID:2, Title:"Ocean Pollution",  Text:"Ocean pollution is caused..."}
  │
  │ Analyzer.Analyze(title + " " + text)
  │   tokenize → lowercase → NFC → stopword (GAPS PRESERVED) → Porter2 stem
  ▼
doc0 tokens: [{climat,0},{chang,1},{climat,2},{chang,3},{affect,4},{ocean,6},{plant,8},{lif,9},{global,10}]
doc1 tokens: [{ocean,0},{ocean,2},{larg,5},{bodi,6},{water,8},{ocean,9},{pollut,10},{affect,11},{marin,12},{lif,13}]
doc2 tokens: [{ocean,0},{pollut,1},{ocean,2},{pollut,3},{caus,5},{industri,7},{wast,8},{climat,10},{chang,11}]
  │
  │ MemoryIndex.Add() — count TF, record Positions per term per doc
  │ MemoryIndex.Finalize() — sort all posting lists by DocID ascending
  ▼
postings["climat"] = [{doc0,tf:2,pos:[0,2]}, {doc2,tf:1,pos:[10]}]
postings["chang"]  = [{doc0,tf:2,pos:[1,3]}, {doc2,tf:1,pos:[11]}]
postings["ocean"]  = [{doc0,tf:1,pos:[6]}, {doc1,tf:3,pos:[0,2,9]}, {doc2,tf:2,pos:[0,2]}]
postings["pollut"] = [{doc1,tf:1,pos:[10]}, {doc2,tf:2,pos:[1,3]}]
  │
  │ WriteSegment() — gap-encode docIDs, varint compress → segment.post
  │                — sorted term dict + byte offsets  → segment.dict
  │                — title+length per doc             → segment.docs
  │ PageRank power iteration (d=0.85, 30 iters)      → pagerank.json
  ▼
data/index/ (segment files on disk)

═══════════════════════════════════════════════

go run ./cmd/search -index data/index
  │
  │ OpenSegment(dir) — load dict into map, post into []byte, docs into []Document
  │ Load pagerank.json → []float64
  │ (milliseconds — no analysis, no sorting)
  ▼
idx (segmentIndex satisfying index.Index interface)

User types: "climate change" AND ocean NOT pollution
  │
  │ query.Parse()
  │   lex  → [PHRASE("climate change"), AND, WORD("ocean"), NOT, WORD("pollution"), EOF]
  │   parse → AndNode{PhraseNode["climate","change"], TermNode["ocean"], NotNode[TermNode["pollution"]]}
  ▼
AST evaluated:

  evalNode(PhraseNode):
    Lookup("climat") → [{doc0,pos:[0,2]}, {doc2,pos:[10]}]
    Lookup("chang")  → [{doc0,pos:[1,3]}, {doc2,pos:[11]}]
    PhraseIntersect(gap=1):
      doc0: posA=[0,2], posB=[1,3] → 0+1=1 ✓ MATCH
      doc2: posA=[10],  posB=[11]  → 10+1=11 ✓ MATCH
    → [{doc0}, {doc2}]

  evalNode(TermNode["ocean"]):
    Lookup("ocean") → [{doc0}, {doc1}, {doc2}]

  Intersect(phrase, ocean):            ← two-pointer O(n+m)
    doc0 in both → emit; doc1 only in ocean → skip; doc2 in both → emit
    → [{doc0}, {doc2}]

  evalNode(NotNode → TermNode["pollution"]):
    Lookup("pollut") → [{doc1}, {doc2}]

  Difference(and_result, pollution):   ← two-pointer O(n+m)
    doc0 < doc1 → emit doc0
    doc2 == doc2 → skip (NOT)
    → [{doc0}]

Final candidates: [{doc0}]
  │
  │ Cache posting lists once:
  │   termPLs["climat"] = {DocFreq:2}
  │   termPLs["chang"]  = {DocFreq:2}
  │   termPLs["ocean"]  = {DocFreq:3}
  │
  │ BM25.Score per term, summed:
  │   score("climat") = idf(0.470) × norm(1.415) = 0.665
  │   score("chang")  = 0.665
  │   score("ocean")  = idf(0.134) × norm(1.043) = 0.140
  │   bm25_total = 1.470
  │
  │ PageRank blend:
  │   final = 1.470 + 1.0 × log(1 + 0.45) = 1.470 + 0.372 = 1.842
  ▼
results: [{doc0, Score:1.842}]
  │
  │ TopK(k=10): 1 result, fits as-is
  │
  │ Snippet(doc0.Text, ["climat","chang","ocean"], windowSize=160):
  │   entire text fits in one window (9 words)
  │   highlight all 3 terms with ANSI bold
  ▼
1. Climate Change (1.8420)
   Climate change affects the ocean and plant life globally.
```

---

## What Would Change for Different Query Types

### Plain AND query: `climate change ocean`

No phrase — `climate` and `change` and `ocean` just need to appear anywhere:

```
evalNode(AndNode):
  Lookup("climat") → [{doc0}, {doc2}]
  Lookup("chang")  → [{doc0}, {doc2}]
  Lookup("ocean")  → [{doc0}, {doc1}, {doc2}]

  Intersect(climat, chang) → [{doc0}, {doc2}]
  Intersect(result, ocean) → [{doc0}, {doc2}]
```

**doc2 now matches** — it has "climat" (at pos 10), "chang" (pos 11), and "ocean" (pos 0). Without the phrase constraint, position doesn't matter. AND just requires presence.

### OR query: `climate OR ocean`

```
evalNode(OrNode):
  Lookup("climat") → [{doc0}, {doc2}]
  Lookup("ocean")  → [{doc0}, {doc1}, {doc2}]

  Union(climat, ocean) → [{doc0}, {doc1}, {doc2}]
```

All 3 docs match — any doc with either term.

### Field query: `title:climate`

```
evalNode(FieldNode{Field:"title", Child:TermNode["climate"]}):
  → evalNode(TermNode["climate"])   (field scoping not fully implemented — treated as term)
  → Lookup("climat") → [{doc0}, {doc2}]
```

Both have "climat" (from title analysis). Full field-scoped implementation would filter by source field — a future enhancement.
