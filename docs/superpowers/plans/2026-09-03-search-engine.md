# Search Engine in Go — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a full-text search engine over Simple English Wikipedia from scratch in Go, learning every layer from tokenization to ranking to disk persistence.

**Architecture:** Inverted index maps terms → posting lists. Analyzer pipeline (tokenize → lowercase → normalize → stopword → stem) runs identically at index and query time. `Index` interface lets in-memory and on-disk implementations swap without touching query code.

**Tech Stack:** Go 1.22+, `github.com/kljensen/snowball`, `golang.org/x/text/unicode/norm`, `golang.org/x/exp/mmap`, `github.com/mattn/go-sqlite3`

**Spec:** `search-engine-project-plan.md`

## Global Constraints

- Go 1.22+
- All analyzer steps must be identical at index-time and query-time
- Postings sorted by docID — invariant never broken
- `Index` interface never changes between implementations
- No external search libraries (build it yourself)
- `bufio.Scanner` buffer must be at least 10MB (articles exceed 64KB default)
- Term positions must preserve gaps when stopwords dropped (do not renumber)
- Odd lines in dump = action lines, skip them; decode even lines only

---

## Sprint 1 — Project Setup + Dump Reader

### Task 1: Directory skeleton + go.mod

**Files:**
- Create: `wikisearch/go.mod`
- Create: `wikisearch/internal/corpus/document.go`
- Create: `wikisearch/internal/index/index.go`

**Interfaces:**
- Produces: `corpus.Document` struct, `index.Index` interface

- [ ] **Step 1: Create directory skeleton**

```bash
cd /Users/ctme/Documents/projects/whis
mkdir -p wikisearch/{cmd/{index,search,eval,baseline},internal/{corpus,analysis,postings,index,store,query,rank,link,eval},testdata,data}
cd wikisearch
go mod init wikisearch
go mod tidy
```

- [ ] **Step 2: Write core Document type**

`internal/corpus/document.go`:
```go
package corpus

// Document is one Wikipedia article after decoding from the dump.
// ID is assigned sequentially by the reader — it is not the Wikipedia page ID.
type Document struct {
	ID     uint32
	Title  string
	Text   string
	Links  []string // outgoing_link field — used for PageRank (Phase 4)
	Length uint32   // token count, set by the indexer after analysis
}
```

- [ ] **Step 3: Write Index interface**

`internal/index/index.go`:
```go
package index

import "wikisearch/internal/corpus"

// PostingEntry is a single (docID, termFreq) pair in a posting list.
// Positions are nil until Phase 4.
type PostingEntry struct {
	DocID     uint32
	TermFreq  uint32
	Positions []uint32
}

// PostingList holds all documents containing a term.
// Entries MUST be sorted by DocID — callers may assert this.
type PostingList struct {
	Term     string
	DocFreq  uint32
	Entries  []PostingEntry
}

// Index is the interface all index implementations satisfy.
// cmd/search uses only this interface so it never changes when
// you swap in-memory for on-disk in Phase 3.
type Index interface {
	Lookup(term string) (PostingList, bool)
	NumDocs() uint32
	AvgDocLen() float64
	DocLen(id uint32) uint32
	Doc(id uint32) (corpus.Document, error)
}
```

- [ ] **Step 4: Verify build passes**

```bash
cd wikisearch
go build ./...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add wikisearch/
git commit -m "feat: project skeleton, Document type, Index interface"
```

---

### Task 2: Dump reader

**Files:**
- Create: `wikisearch/internal/corpus/reader.go`
- Create: `wikisearch/internal/corpus/reader_test.go`

**Interfaces:**
- Consumes: `corpus.Document`
- Produces: `NewReader(path string) (*Reader, error)`, `Reader.Next() (Document, bool, error)`

The dump is line-delimited JSON in pairs: action line (odd), document line (even). Skip odd lines. Sniff extension to pick bzip2 vs gzip decompressor.

- [ ] **Step 1: Add dependencies**

```bash
cd wikisearch
# no extra deps needed — compress/bzip2 and compress/gzip are stdlib
go mod tidy
```

- [ ] **Step 2: Write failing test**

`internal/corpus/reader_test.go`:
```go
package corpus_test

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"wikisearch/internal/corpus"
)

// makeTestDump writes a minimal dump file (2 articles) as gzip and returns its path.
func makeTestDump(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "dump-*.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)

	type action struct {
		Index struct {
			ID string `json:"_id"`
		} `json:"index"`
	}
	type doc struct {
		Title        string   `json:"title"`
		Text         string   `json:"text"`
		OutgoingLink []string `json:"outgoing_link"`
	}

	enc := json.NewEncoder(gw)
	enc.Encode(action{})
	enc.Encode(doc{Title: "Photosynthesis", Text: "Photosynthesis is a process.", OutgoingLink: []string{"Plant"}})
	enc.Encode(action{})
	enc.Encode(doc{Title: "Plant", Text: "A plant is a living thing.", OutgoingLink: []string{"Photosynthesis"}})

	gw.Close()
	f.Close()
	return f.Name()
}

func TestReaderCounts(t *testing.T) {
	path := makeTestDump(t)
	r, err := corpus.NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var count int
	for {
		doc, ok, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		count++
		if count == 1 && doc.Title != "Photosynthesis" {
			t.Errorf("got title %q, want Photosynthesis", doc.Title)
		}
	}
	if count != 2 {
		t.Errorf("got %d docs, want 2", count)
	}
}

func TestReaderAssignsSequentialIDs(t *testing.T) {
	path := makeTestDump(t)
	r, _ := corpus.NewReader(path)
	defer r.Close()

	d0, _, _ := r.Next()
	d1, _, _ := r.Next()
	if d0.ID != 0 || d1.ID != 1 {
		t.Errorf("IDs = %d, %d; want 0, 1", d0.ID, d1.ID)
	}
}
```

- [ ] **Step 3: Run test — expect FAIL**

```bash
cd wikisearch
go test ./internal/corpus/... -v
```
Expected: compile error (NewReader undefined).

- [ ] **Step 4: Implement reader**

`internal/corpus/reader.go`:
```go
package corpus

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// rawDoc matches the fields we care about from the CirrusSearch dump.
type rawDoc struct {
	Title        string   `json:"title"`
	Text         string   `json:"text"`
	OutgoingLink []string `json:"outgoing_link"`
}

// Reader streams Documents from a bzip2 or gzip CirrusSearch dump.
type Reader struct {
	f       *os.File
	scanner *bufio.Scanner
	nextID  uint32
	line    int // tracks parity: even lines are document JSON
}

// NewReader opens the dump at path and returns a Reader.
// Compression is detected by file extension (.bz2 → bzip2, else gzip).
func NewReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dump: %w", err)
	}

	var raw io.Reader
	if strings.HasSuffix(path, ".bz2") {
		raw = bzip2.NewReader(f)
	} else {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		raw = gz
	}

	sc := bufio.NewScanner(raw)
	sc.Buffer(make([]byte, 1024*1024), 10*1024*1024) // articles can be large

	return &Reader{f: f, scanner: sc}, nil
}

// Next returns the next Document. ok is false when the stream is exhausted.
func (r *Reader) Next() (Document, bool, error) {
	for {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return Document{}, false, err
			}
			return Document{}, false, nil
		}
		r.line++
		if r.line%2 == 1 {
			// odd line = action line, skip
			continue
		}

		var raw rawDoc
		if err := json.Unmarshal(r.scanner.Bytes(), &raw); err != nil {
			return Document{}, false, fmt.Errorf("line %d: %w", r.line, err)
		}

		doc := Document{
			ID:    r.nextID,
			Title: raw.Title,
			Text:  raw.Text,
			Links: raw.OutgoingLink,
		}
		r.nextID++
		return doc, true, nil
	}
}

// Close releases the underlying file.
func (r *Reader) Close() error {
	return r.f.Close()
}
```

- [ ] **Step 5: Run test — expect PASS**

```bash
cd wikisearch
go test ./internal/corpus/... -v
```
Expected: PASS.

- [ ] **Step 6: Write cmd/index to print doc count + first title**

`cmd/index/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"log"

	"wikisearch/internal/corpus"
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	var count uint32
	var firstTitle string
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		if count == 0 {
			firstTitle = doc.Title
		}
		count++
	}
	fmt.Printf("docs: %d\nfirst title: %s\n", count, firstTitle)
}
```

- [ ] **Step 7: Smoke-test against sample data**

First create a sample (10k articles):
```bash
cd /Users/ctme/Documents/projects/whis
# if .bz2 available:
bzcat simplewiki_content-20260830-00000.json.bz2 | head -20000 | gzip > wikisearch/data/sample.json.gz
# or from decompressed:
head -20000 simplewiki_content-20260830-00000.json | gzip > wikisearch/data/sample.json.gz
```

Run:
```bash
cd wikisearch
go run ./cmd/index -dump data/sample.json.gz
```
Expected: prints doc count ~10000 and a title.

- [ ] **Step 8: Commit**

```bash
git add wikisearch/
git commit -m "feat: dump reader with bzip2/gzip support, sequential IDs"
```

---

## Sprint 2 — Analyzer Pipeline

**Learning goal:** Understand why analysis must be identical at index-time and query-time. A stemmed query token must match a stemmed index token — any divergence = silent miss.

### Task 3: Tokenizer + Token type

**Files:**
- Create: `wikisearch/internal/analysis/token.go`
- Create: `wikisearch/internal/analysis/tokenizer.go`
- Create: `wikisearch/internal/analysis/tokenizer_test.go`

**Interfaces:**
- Produces: `analysis.Token{Term string; Position int}`, `analysis.Tokenize(text string) []Token`

- [ ] **Step 1: Write failing test**

`internal/analysis/tokenizer_test.go`:
```go
package analysis_test

import (
	"testing"
	"wikisearch/internal/analysis"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"Hello, world!", []string{"Hello", "world"}},
		{"C++ programming", []string{"C", "programming"}},
		{"don't stop", []string{"don", "t", "stop"}},
		{"  spaces  ", []string{"spaces"}},
		{"", nil},
	}
	for _, c := range cases {
		got := analysis.Tokenize(c.input)
		if len(got) != len(c.want) {
			t.Errorf("Tokenize(%q): got %d tokens, want %d", c.input, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i].Term != c.want[i] {
				t.Errorf("Tokenize(%q)[%d]: got %q, want %q", c.input, i, got[i].Term, c.want[i])
			}
		}
	}
}

func TestTokenizePositions(t *testing.T) {
	tokens := analysis.Tokenize("the cat sat")
	for i, tok := range tokens {
		if tok.Position != i {
			t.Errorf("position[%d] = %d, want %d", i, tok.Position, i)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/analysis/... -v -run TestTokenize
```

- [ ] **Step 3: Implement token type and tokenizer**

`internal/analysis/token.go`:
```go
package analysis

// Token is one term extracted from text, with its position in the original stream.
// Position is the ordinal index in the token stream (0-based), not byte offset.
// Filters that drop tokens (e.g. stopwords) must leave a positional gap — do NOT renumber.
type Token struct {
	Term     string
	Position int
}
```

`internal/analysis/tokenizer.go`:
```go
package analysis

import "unicode"

// Tokenize splits text on non-letter/non-digit runs.
// Uses unicode.IsLetter so non-ASCII letters (é, ñ, etc.) are kept.
// Position is the ordinal index in the output slice (filters may create gaps later).
func Tokenize(text string) []Token {
	var tokens []Token
	var buf []rune
	pos := 0

	flush := func() {
		if len(buf) > 0 {
			tokens = append(tokens, Token{Term: string(buf), Position: pos})
			pos++
			buf = buf[:0]
		}
	}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf = append(buf, r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/analysis/... -v -run TestTokenize
```

- [ ] **Step 5: Commit**

```bash
git add wikisearch/internal/analysis/
git commit -m "feat: tokenizer, Token type with positions"
```

---

### Task 4: Filter chain (lowercase → normalize → stopword → stem)

**Files:**
- Create: `wikisearch/internal/analysis/filters.go`
- Create: `wikisearch/internal/analysis/analyzer.go`
- Create: `wikisearch/internal/analysis/filters_test.go`
- Create: `wikisearch/internal/analysis/analyzer_test.go`

**Interfaces:**
- Consumes: `analysis.Token`, `analysis.Tokenize`
- Produces: `analysis.Filter` interface, `analysis.Analyzer` struct with `Analyze(text string) []Token`

- [ ] **Step 1: Add dependencies**

```bash
cd wikisearch
go get github.com/kljensen/snowball
go get golang.org/x/text/unicode/norm
go mod tidy
```

- [ ] **Step 2: Write failing tests**

`internal/analysis/filters_test.go`:
```go
package analysis_test

import (
	"testing"
	"wikisearch/internal/analysis"
)

func TestLowercaseFilter(t *testing.T) {
	in := []analysis.Token{{Term: "Running", Position: 0}, {Term: "DOGS", Position: 1}}
	out := analysis.LowercaseFilter(in)
	if out[0].Term != "running" || out[1].Term != "dogs" {
		t.Errorf("got %v", out)
	}
}

func TestStopwordFilter(t *testing.T) {
	in := []analysis.Token{
		{Term: "the", Position: 0},
		{Term: "running", Position: 1},
		{Term: "dogs", Position: 2},
	}
	out := analysis.StopwordFilter(in)
	// "the" dropped; positions must NOT be renumbered
	if len(out) != 2 {
		t.Fatalf("got %d tokens, want 2", len(out))
	}
	if out[0].Position != 1 {
		t.Errorf("position gap lost: got position %d, want 1", out[0].Position)
	}
}

func TestStemFilter(t *testing.T) {
	in := []analysis.Token{{Term: "running", Position: 0}, {Term: "dogs", Position: 1}}
	out := analysis.StemFilter(in)
	// Porter2: "running" → "run", "dogs" → "dog"
	if out[0].Term != "run" {
		t.Errorf("stem(running) = %q, want run", out[0].Term)
	}
	if out[1].Term != "dog" {
		t.Errorf("stem(dogs) = %q, want dog", out[1].Term)
	}
}
```

`internal/analysis/analyzer_test.go`:
```go
package analysis_test

import (
	"testing"
	"wikisearch/internal/analysis"
)

func TestAnalyzerPipeline(t *testing.T) {
	a := analysis.NewAnalyzer()
	// "The Running Dogs" → skip "the" (stopword), stem rest
	tokens := a.Analyze("The Running Dogs")
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(tokens), tokens)
	}
	if tokens[0].Term != "run" {
		t.Errorf("tokens[0] = %q, want run", tokens[0].Term)
	}
	if tokens[1].Term != "dog" {
		t.Errorf("tokens[1] = %q, want dog", tokens[1].Term)
	}
	// positions: "the" was at 0, "running" at 1, "dogs" at 2
	// after stopword drop: running keeps position 1, dogs keeps 2
	if tokens[0].Position != 1 || tokens[1].Position != 2 {
		t.Errorf("positions = %d, %d; want 1, 2", tokens[0].Position, tokens[1].Position)
	}
}
```

- [ ] **Step 3: Run — expect FAIL**

```bash
go test ./internal/analysis/... -v
```

- [ ] **Step 4: Implement filters**

`internal/analysis/filters.go`:
```go
package analysis

import (
	"strings"

	"github.com/kljensen/snowball/english"
	"golang.org/x/text/unicode/norm"
)

// LowercaseFilter lowercases all token terms.
func LowercaseFilter(tokens []Token) []Token {
	for i := range tokens {
		tokens[i].Term = strings.ToLower(tokens[i].Term)
	}
	return tokens
}

// NormalizeFilter applies Unicode NFC normalization.
func NormalizeFilter(tokens []Token) []Token {
	for i := range tokens {
		tokens[i].Term = norm.NFC.String(tokens[i].Term)
	}
	return tokens
}

// stopwords is a minimal set of common English function words.
var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "but": {},
	"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {},
	"with": {}, "by": {}, "from": {}, "is": {}, "it": {}, "its": {},
	"as": {}, "be": {}, "was": {}, "are": {}, "were": {}, "has": {},
	"have": {}, "had": {}, "he": {}, "she": {}, "they": {}, "we": {},
	"i": {}, "you": {}, "that": {}, "this": {}, "which": {}, "who": {},
	"not": {}, "no": {}, "so": {}, "if": {}, "do": {}, "did": {},
	"will": {}, "can": {}, "may": {}, "about": {}, "than": {}, "into": {},
}

// StopwordFilter removes common English stopwords.
// IMPORTANT: positions are NOT renumbered — gaps are preserved for phrase queries.
func StopwordFilter(tokens []Token) []Token {
	out := tokens[:0]
	for _, tok := range tokens {
		if _, stop := stopwords[tok.Term]; !stop {
			out = append(out, tok)
		}
	}
	return out
}

// StemFilter applies Porter2 (Snowball) stemming.
func StemFilter(tokens []Token) []Token {
	for i := range tokens {
		tokens[i].Term = english.Stem(tokens[i].Term, false)
	}
	return tokens
}
```

`internal/analysis/analyzer.go`:
```go
package analysis

// Analyzer runs the full pipeline: tokenize → lowercase → normalize → stopword → stem.
// Use the same Analyzer instance at both index-time and query-time.
type Analyzer struct{}

func NewAnalyzer() *Analyzer { return &Analyzer{} }

// Analyze returns the analyzed token stream for text.
func (a *Analyzer) Analyze(text string) []Token {
	tokens := Tokenize(text)
	tokens = LowercaseFilter(tokens)
	tokens = NormalizeFilter(tokens)
	tokens = StopwordFilter(tokens)
	tokens = StemFilter(tokens)
	return tokens
}
```

- [ ] **Step 5: Run — expect PASS**

```bash
go test ./internal/analysis/... -v
```

- [ ] **Step 6: Commit**

```bash
git add wikisearch/internal/analysis/
git commit -m "feat: filter chain (lowercase, normalize, stopword, stem), Analyzer pipeline"
```

---

## Sprint 3 — In-Memory Index + Boolean Search + REPL

**Learning goal:** The inverted index maps every term to the list of documents containing it. This is "inverted" because the forward direction is document → terms; inverting it gives you term → documents.

### Task 5: In-memory index

**Files:**
- Create: `wikisearch/internal/index/memory.go`
- Create: `wikisearch/internal/index/memory_test.go`

**Interfaces:**
- Consumes: `corpus.Document`, `analysis.Analyzer`, `index.Index`, `index.PostingList`, `index.PostingEntry`
- Produces: `index.MemoryIndex` satisfying `index.Index`

- [ ] **Step 1: Write failing test**

`internal/index/memory_test.go`:
```go
package index_test

import (
	"testing"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

func buildTestIndex(t *testing.T) index.Index {
	t.Helper()
	docs := []corpus.Document{
		{ID: 0, Title: "Photosynthesis", Text: "photosynthesis is a process used by plants"},
		{ID: 1, Title: "Plant", Text: "a plant is a living thing that uses photosynthesis"},
		{ID: 2, Title: "Animal", Text: "an animal is a living thing that cannot do photosynthesis"},
	}
	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()
	for _, d := range docs {
		idx.Add(d, a)
	}
	idx.Finalize()
	return idx
}

func TestLookupExists(t *testing.T) {
	idx := buildTestIndex(t)
	pl, ok := idx.Lookup("photosynthesi") // stemmed form
	if !ok {
		t.Fatal("lookup photosynthesi: not found")
	}
	if pl.DocFreq != 3 {
		t.Errorf("docFreq = %d, want 3", pl.DocFreq)
	}
}

func TestLookupMissing(t *testing.T) {
	idx := buildTestIndex(t)
	_, ok := idx.Lookup("xyzzy")
	if ok {
		t.Error("lookup xyzzy: expected not found")
	}
}

func TestPostingsSortedByDocID(t *testing.T) {
	idx := buildTestIndex(t)
	pl, _ := idx.Lookup("photosynthesi")
	for i := 1; i < len(pl.Entries); i++ {
		if pl.Entries[i].DocID <= pl.Entries[i-1].DocID {
			t.Errorf("posting list not sorted at index %d: %d <= %d",
				i, pl.Entries[i].DocID, pl.Entries[i-1].DocID)
		}
	}
}

func TestNumDocs(t *testing.T) {
	idx := buildTestIndex(t)
	if idx.NumDocs() != 3 {
		t.Errorf("NumDocs = %d, want 3", idx.NumDocs())
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/index/... -v
```

- [ ] **Step 3: Implement MemoryIndex**

`internal/index/memory.go`:
```go
package index

import (
	"fmt"
	"sort"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
)

// MemoryIndex holds the entire inverted index in RAM.
// Call Add for each document, then Finalize before querying.
type MemoryIndex struct {
	postings   map[string][]PostingEntry // term → entries (unsorted during build)
	docs       []corpus.Document
	docLengths []uint32
	totalLen   uint64
}

func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{postings: make(map[string][]PostingEntry)}
}

// Add analyzes doc and updates the inverted index.
// Documents must be added in increasing ID order.
func (m *MemoryIndex) Add(doc corpus.Document, a *analysis.Analyzer) {
	tokens := a.Analyze(doc.Title + " " + doc.Text)
	doc.Length = uint32(len(tokens))

	// count term frequencies within this document
	freq := make(map[string]uint32)
	for _, tok := range tokens {
		freq[tok.Term]++
	}

	for term, tf := range freq {
		m.postings[term] = append(m.postings[term], PostingEntry{
			DocID:    doc.ID,
			TermFreq: tf,
		})
	}

	m.docs = append(m.docs, doc)
	m.docLengths = append(m.docLengths, doc.Length)
	m.totalLen += uint64(doc.Length)
}

// Finalize sorts all posting lists by DocID and computes DocFreq.
// Must be called after all Add calls and before any Lookup.
func (m *MemoryIndex) Finalize() {
	for term, entries := range m.postings {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].DocID < entries[j].DocID
		})
		m.postings[term] = entries
	}
}

func (m *MemoryIndex) Lookup(term string) (PostingList, bool) {
	entries, ok := m.postings[term]
	if !ok {
		return PostingList{}, false
	}
	return PostingList{
		Term:    term,
		DocFreq: uint32(len(entries)),
		Entries: entries,
	}, true
}

func (m *MemoryIndex) NumDocs() uint32   { return uint32(len(m.docs)) }
func (m *MemoryIndex) DocLen(id uint32) uint32 { return m.docLengths[id] }
func (m *MemoryIndex) AvgDocLen() float64 {
	if len(m.docs) == 0 {
		return 0
	}
	return float64(m.totalLen) / float64(len(m.docs))
}
func (m *MemoryIndex) Doc(id uint32) (corpus.Document, error) {
	if int(id) >= len(m.docs) {
		return corpus.Document{}, fmt.Errorf("doc %d not found", id)
	}
	return m.docs[id], nil
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/index/... -v
```

- [ ] **Step 5: Commit**

```bash
git add wikisearch/internal/index/
git commit -m "feat: in-memory inverted index with MemoryIndex"
```

---

### Task 6: Posting list operations (AND, OR, NOT)

**Files:**
- Create: `wikisearch/internal/postings/ops.go`
- Create: `wikisearch/internal/postings/ops_test.go`

**Interfaces:**
- Consumes: `index.PostingList`, `index.PostingEntry`
- Produces: `postings.Intersect`, `postings.Union`, `postings.Difference`

**Learning goal:** AND query = set intersection via two-pointer merge. Runs in O(n+m) — no hashing.

- [ ] **Step 1: Write failing tests**

`internal/postings/ops_test.go`:
```go
package postings_test

import (
	"testing"

	"wikisearch/internal/index"
	"wikisearch/internal/postings"
)

func pl(docIDs ...uint32) index.PostingList {
	entries := make([]index.PostingEntry, len(docIDs))
	for i, id := range docIDs {
		entries[i] = index.PostingEntry{DocID: id, TermFreq: 1}
	}
	return index.PostingList{DocFreq: uint32(len(entries)), Entries: entries}
}

func ids(pl index.PostingList) []uint32 {
	out := make([]uint32, len(pl.Entries))
	for i, e := range pl.Entries {
		out[i] = e.DocID
	}
	return out
}

func TestIntersect(t *testing.T) {
	a := pl(1, 3, 5, 7)
	b := pl(2, 3, 5, 8)
	got := ids(postings.Intersect(a, b))
	want := []uint32{3, 5}
	if !equal(got, want) {
		t.Errorf("Intersect = %v, want %v", got, want)
	}
}

func TestUnion(t *testing.T) {
	a := pl(1, 3)
	b := pl(2, 3, 4)
	got := ids(postings.Union(a, b))
	want := []uint32{1, 2, 3, 4}
	if !equal(got, want) {
		t.Errorf("Union = %v, want %v", got, want)
	}
}

func TestDifference(t *testing.T) {
	a := pl(1, 2, 3, 4)
	b := pl(2, 4)
	got := ids(postings.Difference(a, b))
	want := []uint32{1, 3}
	if !equal(got, want) {
		t.Errorf("Difference = %v, want %v", got, want)
	}
}

func equal(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/postings/... -v
```

- [ ] **Step 3: Implement ops**

`internal/postings/ops.go`:
```go
package postings

import "wikisearch/internal/index"

// Intersect returns entries in both a and b (AND). Two-pointer merge, O(n+m).
func Intersect(a, b index.PostingList) index.PostingList {
	var out []index.PostingEntry
	i, j := 0, 0
	for i < len(a.Entries) && j < len(b.Entries) {
		ai, bj := a.Entries[i].DocID, b.Entries[j].DocID
		switch {
		case ai == bj:
			out = append(out, index.PostingEntry{DocID: ai, TermFreq: a.Entries[i].TermFreq + b.Entries[j].TermFreq})
			i++
			j++
		case ai < bj:
			i++
		default:
			j++
		}
	}
	return index.PostingList{DocFreq: uint32(len(out)), Entries: out}
}

// Union returns all entries in a or b (OR), sorted by DocID.
func Union(a, b index.PostingList) index.PostingList {
	var out []index.PostingEntry
	i, j := 0, 0
	for i < len(a.Entries) && j < len(b.Entries) {
		ai, bj := a.Entries[i].DocID, b.Entries[j].DocID
		switch {
		case ai == bj:
			out = append(out, index.PostingEntry{DocID: ai, TermFreq: a.Entries[i].TermFreq + b.Entries[j].TermFreq})
			i++
			j++
		case ai < bj:
			out = append(out, a.Entries[i])
			i++
		default:
			out = append(out, b.Entries[j])
			j++
		}
	}
	out = append(out, a.Entries[i:]...)
	out = append(out, b.Entries[j:]...)
	return index.PostingList{DocFreq: uint32(len(out)), Entries: out}
}

// Difference returns entries in a but not in b (NOT b).
func Difference(a, b index.PostingList) index.PostingList {
	var out []index.PostingEntry
	i, j := 0, 0
	for i < len(a.Entries) {
		if j >= len(b.Entries) || a.Entries[i].DocID < b.Entries[j].DocID {
			out = append(out, a.Entries[i])
			i++
		} else if a.Entries[i].DocID == b.Entries[j].DocID {
			i++
			j++
		} else {
			j++
		}
	}
	return index.PostingList{DocFreq: uint32(len(out)), Entries: out}
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/postings/... -v
```

- [ ] **Step 5: Commit**

```bash
git add wikisearch/internal/postings/
git commit -m "feat: posting list ops (Intersect, Union, Difference) — two-pointer merge"
```

---

### Task 7: Build pipeline + search REPL

**Files:**
- Modify: `wikisearch/cmd/index/main.go`
- Create: `wikisearch/cmd/search/main.go`

**Interfaces:**
- Consumes: `corpus.Reader`, `analysis.Analyzer`, `index.MemoryIndex`, `postings.Intersect`
- Produces: working REPL that ANDs query terms and prints top 10 titles

- [ ] **Step 1: Update cmd/index to build and serialize index**

Replace `cmd/index/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

func main() {
	dump := flag.String("dump", "", "path to dump (.json.gz or .json.bz2)")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()

	start := time.Now()
	var count uint32
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		idx.Add(doc, a)
		count++
		if count%10000 == 0 {
			fmt.Printf("\r  indexed %d docs...", count)
		}
	}
	idx.Finalize()
	fmt.Printf("\nindexed %d docs in %s\n", count, time.Since(start))
}
```

- [ ] **Step 2: Write search REPL**

`cmd/search/main.go`:
```go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
	"wikisearch/internal/postings"
)

func main() {
	dump := flag.String("dump", "", "path to dump")
	flag.Parse()
	if *dump == "" {
		log.Fatal("-dump required")
	}

	// Build index
	fmt.Print("building index...")
	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		idx.Add(doc, a)
	}
	idx.Finalize()
	fmt.Printf(" done (%d docs)\n", idx.NumDocs())

	// REPL
	sc := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for sc.Scan() {
		query := strings.TrimSpace(sc.Text())
		if query == "" {
			fmt.Print("> ")
			continue
		}

		start := time.Now()
		tokens := a.Analyze(query)
		if len(tokens) == 0 {
			fmt.Println("(no terms after analysis)")
			fmt.Print("> ")
			continue
		}

		// AND all terms together
		pl, ok := idx.Lookup(tokens[0].Term)
		if !ok {
			fmt.Println("no results")
			fmt.Print("> ")
			continue
		}
		for _, tok := range tokens[1:] {
			other, ok := idx.Lookup(tok.Term)
			if !ok {
				pl.Entries = nil
				break
			}
			pl = postings.Intersect(pl, other)
		}

		elapsed := time.Since(start)
		fmt.Printf("found %d documents in %s\n", len(pl.Entries), elapsed)

		limit := 10
		if len(pl.Entries) < limit {
			limit = len(pl.Entries)
		}
		for i, entry := range pl.Entries[:limit] {
			doc, _ := idx.Doc(entry.DocID)
			fmt.Printf("%d. %s\n", i+1, doc.Title)
		}
		fmt.Print("> ")
	}
}
```

- [ ] **Step 3: Smoke test**

```bash
cd wikisearch
go run ./cmd/search -dump data/sample.json.gz
```
Then type: `photosynthesis plant`

Expected output:
```
found N documents in Xms
1. Photosynthesis
2. ...
```

- [ ] **Step 4: Commit**

```bash
git add wikisearch/cmd/
git commit -m "feat: index builder and boolean search REPL — Phase 1 complete"
```

---

## Sprint 4 — Ranking + Evaluation

**Learning goal:** Boolean retrieval finds matching documents; ranking decides which ones matter. TF-IDF and BM25 are the two dominant term-frequency rankers. You'll implement both, then *measure* the difference.

### Task 8: TF-IDF + BM25 scorers

**Files:**
- Create: `wikisearch/internal/rank/tfidf.go`
- Create: `wikisearch/internal/rank/bm25.go`
- Create: `wikisearch/internal/rank/scorer.go`
- Create: `wikisearch/internal/rank/rank_test.go`

**Interfaces:**
- Consumes: `index.PostingEntry`, `index.Index`
- Produces: `rank.Scorer` interface, `rank.TFIDFScorer`, `rank.BM25Scorer`

- [ ] **Step 1: Write failing tests**

`internal/rank/rank_test.go`:
```go
package rank_test

import (
	"math"
	"testing"

	"wikisearch/internal/index"
	"wikisearch/internal/rank"
)

// stubIndex satisfies index.Index for scoring tests.
type stubIndex struct {
	numDocs  uint32
	avgLen   float64
	docLens  map[uint32]uint32
}

func (s *stubIndex) NumDocs() uint32                               { return s.numDocs }
func (s *stubIndex) AvgDocLen() float64                            { return s.avgLen }
func (s *stubIndex) DocLen(id uint32) uint32                       { return s.docLens[id] }
func (s *stubIndex) Lookup(term string) (index.PostingList, bool)  { return index.PostingList{}, false }
func (s *stubIndex) Doc(id uint32) (interface{ Title() string }, error) { return nil, nil }

func TestBM25NonNegative(t *testing.T) {
	idx := &stubIndex{numDocs: 1000, avgLen: 100, docLens: map[uint32]uint32{0: 50}}
	scorer := rank.NewBM25(idx, 1.2, 0.75)
	entry := index.PostingEntry{DocID: 0, TermFreq: 3}
	score := scorer.Score(entry, 50 /* docFreq */, idx.DocLen(0))
	if score < 0 {
		t.Errorf("BM25 score negative: %f", score)
	}
	if math.IsNaN(score) {
		t.Error("BM25 score is NaN")
	}
}

func TestBM25HigherTFHigherScore(t *testing.T) {
	idx := &stubIndex{numDocs: 1000, avgLen: 100, docLens: map[uint32]uint32{0: 100, 1: 100}}
	scorer := rank.NewBM25(idx, 1.2, 0.75)
	low := scorer.Score(index.PostingEntry{DocID: 0, TermFreq: 1}, 10, 100)
	high := scorer.Score(index.PostingEntry{DocID: 1, TermFreq: 5}, 10, 100)
	if high <= low {
		t.Errorf("higher TF should give higher BM25 score: low=%f high=%f", low, high)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/rank/... -v
```

- [ ] **Step 3: Implement scorers**

`internal/rank/scorer.go`:
```go
package rank

import "wikisearch/internal/index"

// Scorer assigns a score to one (term, document) pair.
type Scorer interface {
	Score(entry index.PostingEntry, docFreq uint32, docLen uint32) float64
}

// Result is a ranked search result.
type Result struct {
	DocID uint32
	Score float64
}
```

`internal/rank/tfidf.go`:
```go
package rank

import (
	"math"

	"wikisearch/internal/index"
)

// TFIDFScorer implements classic TF-IDF.
// tf(t,d) = 1 + log(freq), idf(t) = log(N/df).
type TFIDFScorer struct{ idx index.Index }

func NewTFIDF(idx index.Index) *TFIDFScorer { return &TFIDFScorer{idx: idx} }

func (s *TFIDFScorer) Score(entry index.PostingEntry, docFreq uint32, _ uint32) float64 {
	if docFreq == 0 || entry.TermFreq == 0 {
		return 0
	}
	tf := 1 + math.Log(float64(entry.TermFreq))
	idf := math.Log(float64(s.idx.NumDocs()) / float64(docFreq))
	return tf * idf
}
```

`internal/rank/bm25.go`:
```go
package rank

import (
	"math"

	"wikisearch/internal/index"
)

// BM25Scorer implements Okapi BM25.
// k1 controls TF saturation; b controls length normalization.
// Typical defaults: k1=1.2, b=0.75.
type BM25Scorer struct {
	idx index.Index
	k1  float64
	b   float64
}

func NewBM25(idx index.Index, k1, b float64) *BM25Scorer {
	return &BM25Scorer{idx: idx, k1: k1, b: b}
}

// Score returns the BM25 contribution of one term in one document.
// Call for each query term, sum for the full document score.
func (s *BM25Scorer) Score(entry index.PostingEntry, docFreq uint32, docLen uint32) float64 {
	N := float64(s.idx.NumDocs())
	df := float64(docFreq)
	tf := float64(entry.TermFreq)
	dl := float64(docLen)
	avgdl := s.idx.AvgDocLen()

	// IDF with smoothing to avoid log(0)
	idf := math.Log(1 + (N-df+0.5)/(df+0.5))

	// TF with saturation and length normalization
	norm := tf * (s.k1 + 1) / (tf + s.k1*(1-s.b+s.b*dl/avgdl))

	return idf * norm
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/rank/... -v
```

- [ ] **Step 5: Commit**

```bash
git add wikisearch/internal/rank/
git commit -m "feat: TF-IDF and BM25 scorers"
```

---

### Task 9: Top-k heap + ranked REPL

**Files:**
- Create: `wikisearch/internal/rank/topk.go`
- Modify: `wikisearch/cmd/search/main.go`

**Interfaces:**
- Consumes: `rank.Result`, `rank.Scorer`
- Produces: `rank.TopK(results []Result, k int) []Result`

- [ ] **Step 1: Implement top-k using container/heap**

`internal/rank/topk.go`:
```go
package rank

import "container/heap"

// minHeap is a min-heap of Results by Score (lowest score at top).
type minHeap []Result

func (h minHeap) Len() int            { return len(h) }
func (h minHeap) Less(i, j int) bool  { return h[i].Score < h[j].Score }
func (h minHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(Result)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// TopK returns the k highest-scoring results, sorted descending.
// Runs in O(n log k) — much better than sorting all results when k << n.
func TopK(results []Result, k int) []Result {
	h := &minHeap{}
	heap.Init(h)
	for _, r := range results {
		heap.Push(h, r)
		if h.Len() > k {
			heap.Pop(h) // drop lowest
		}
	}
	out := make([]Result, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(Result)
	}
	return out
}
```

- [ ] **Step 2: Update REPL to score and rank results**

Update `cmd/search/main.go` — replace the REPL loop body with ranked output:
```go
// Score each result with BM25
scorer := rank.NewBM25(idx, 1.2, 0.75)

// ... inside the REPL loop, after building pl:
var results []rank.Result
for _, entry := range pl.Entries {
    var score float64
    for _, tok := range tokens {
        tpl, ok := idx.Lookup(tok.Term)
        if !ok {
            continue
        }
        score += scorer.Score(entry, tpl.DocFreq, idx.DocLen(entry.DocID))
    }
    results = append(results, rank.Result{DocID: entry.DocID, Score: score})
}
top := rank.TopK(results, 10)
for i, r := range top {
    doc, _ := idx.Doc(r.DocID)
    fmt.Printf("%d. %s (%.4f)\n", i+1, doc.Title, r.Score)
}
```

Add `"wikisearch/internal/rank"` to imports.

- [ ] **Step 3: Smoke test**

```bash
go run ./cmd/search -dump data/sample.json.gz
```
Query: `photosynthesis plant`

Results should now be ordered by BM25 score, not docID.

- [ ] **Step 4: Commit**

```bash
git add wikisearch/internal/rank/ wikisearch/cmd/search/
git commit -m "feat: top-k heap, BM25 ranking in REPL"
```

---

### Task 10: Evaluation harness + SQLite baseline

**Files:**
- Create: `wikisearch/testdata/queries.json`
- Create: `wikisearch/internal/eval/metrics.go`
- Create: `wikisearch/internal/eval/metrics_test.go`
- Create: `wikisearch/cmd/eval/main.go`
- Create: `wikisearch/cmd/baseline/main.go`

**Interfaces:**
- Produces: `eval.PrecisionAtK`, `eval.MRR`, `eval.NDCG`

- [ ] **Step 1: Write relevance judgments**

`testdata/queries.json`:
```json
[
  {"query": "photosynthesis", "relevant": ["Photosynthesis", "Plant", "Chlorophyll", "Leaf"]},
  {"query": "world war two", "relevant": ["World War II", "Adolf Hitler", "Nazi Germany", "Holocaust"]},
  {"query": "mercury", "relevant": ["Mercury (planet)", "Mercury (element)", "Mercury (mythology)"]},
  {"query": "python programming", "relevant": ["Python (programming language)", "Programming language"]},
  {"query": "solar system planets", "relevant": ["Solar System", "Planet", "Jupiter", "Saturn", "Earth"]},
  {"query": "evolution darwin", "relevant": ["Evolution", "Charles Darwin", "Natural selection"]},
  {"query": "water molecule", "relevant": ["Water", "Molecule", "Hydrogen", "Oxygen"]},
  {"query": "french revolution", "relevant": ["French Revolution", "Napoleon Bonaparte", "Marie Antoinette"]},
  {"query": "quantum mechanics", "relevant": ["Quantum mechanics", "Wave function", "Electron"]},
  {"query": "black hole", "relevant": ["Black hole", "Gravity", "Neutron star"]},
  {"query": "music jazz", "relevant": ["Jazz", "Jazz music", "Blues"]},
  {"query": "football soccer", "relevant": ["Association football", "FIFA World Cup", "Football"]},
  {"query": "amazon river rainforest", "relevant": ["Amazon River", "Amazon rainforest", "Brazil"]},
  {"query": "gravity newton", "relevant": ["Gravity", "Isaac Newton", "Classical mechanics"]},
  {"query": "democracy election voting", "relevant": ["Democracy", "Election", "Voting"]}
]
```

- [ ] **Step 2: Write failing metric tests**

`internal/eval/metrics_test.go`:
```go
package eval_test

import (
	"math"
	"testing"

	"wikisearch/internal/eval"
)

func TestPrecisionAtK(t *testing.T) {
	results := []string{"A", "B", "C", "D", "E"}
	relevant := map[string]bool{"A": true, "C": true, "E": true}
	p := eval.PrecisionAtK(results, relevant, 5)
	// 3 of 5 relevant
	if math.Abs(p-0.6) > 1e-9 {
		t.Errorf("P@5 = %f, want 0.6", p)
	}
}

func TestMRR(t *testing.T) {
	// first relevant result at position 3 (0-indexed: 2)
	results := []string{"X", "Y", "A", "B"}
	relevant := map[string]bool{"A": true}
	mrr := eval.MRR(results, relevant)
	// rank 3 → 1/3
	if math.Abs(mrr-1.0/3.0) > 1e-9 {
		t.Errorf("MRR = %f, want 0.333", mrr)
	}
}

func TestNDCG(t *testing.T) {
	// perfect order
	results := []string{"A", "B", "C"}
	relevant := map[string]bool{"A": true, "B": true, "C": true}
	ndcg := eval.NDCGAtK(results, relevant, 3)
	if math.Abs(ndcg-1.0) > 1e-9 {
		t.Errorf("NDCG@3 perfect order = %f, want 1.0", ndcg)
	}
}
```

- [ ] **Step 3: Run — expect FAIL**

```bash
go test ./internal/eval/... -v
```

- [ ] **Step 4: Implement metrics**

`internal/eval/metrics.go`:
```go
package eval

import "math"

// PrecisionAtK = (relevant results in top k) / k
func PrecisionAtK(results []string, relevant map[string]bool, k int) float64 {
	if k <= 0 {
		return 0
	}
	var hits float64
	for i := 0; i < k && i < len(results); i++ {
		if relevant[results[i]] {
			hits++
		}
	}
	return hits / float64(k)
}

// MRR = 1 / rank of first relevant result. 0 if none found.
func MRR(results []string, relevant map[string]bool) float64 {
	for i, r := range results {
		if relevant[r] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// NDCGAtK computes Normalized Discounted Cumulative Gain at k.
// Assumes binary relevance (0 or 1).
func NDCGAtK(results []string, relevant map[string]bool, k int) float64 {
	dcg := dcgAtK(results, relevant, k)
	// ideal: all relevant results at top
	ideal := make([]string, 0, len(relevant))
	for r := range relevant {
		ideal = append(ideal, r)
	}
	idcg := dcgAtK(ideal, relevant, k)
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func dcgAtK(results []string, relevant map[string]bool, k int) float64 {
	var dcg float64
	for i := 0; i < k && i < len(results); i++ {
		if relevant[results[i]] {
			dcg += 1.0 / math.Log2(float64(i+2)) // log2(rank+1), rank is 1-based
		}
	}
	return dcg
}
```

- [ ] **Step 5: Run — expect PASS**

```bash
go test ./internal/eval/... -v
```

- [ ] **Step 6: Add SQLite baseline**

```bash
cd wikisearch
go get github.com/mattn/go-sqlite3
go mod tidy
```

`cmd/baseline/main.go`:
```go
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"wikisearch/internal/corpus"
	"wikisearch/internal/eval"
)

type queryCase struct {
	Query    string   `json:"query"`
	Relevant []string `json:"relevant"`
}

func main() {
	dump := flag.String("dump", "", "path to dump")
	queries := flag.String("queries", "testdata/queries.json", "path to queries.json")
	flag.Parse()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE VIRTUAL TABLE docs USING fts5(title, text)`)

	// Load corpus
	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	stmt, _ := db.Prepare(`INSERT INTO docs(title, text) VALUES (?, ?)`)
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		stmt.Exec(doc.Title, doc.Text)
	}
	r.Close()

	// Run eval
	data, _ := os.ReadFile(*queries)
	var cases []queryCase
	json.Unmarshal(data, &cases)

	var totalP, totalMRR, totalNDCG float64
	for _, c := range cases {
		rows, _ := db.Query(`SELECT title FROM docs WHERE docs MATCH ? ORDER BY bm25(docs) LIMIT 10`, c.Query)
		var results []string
		for rows.Next() {
			var title string
			rows.Scan(&title)
			results = append(results, title)
		}
		rows.Close()

		rel := make(map[string]bool)
		for _, r := range c.Relevant {
			rel[r] = true
		}
		totalP += eval.PrecisionAtK(results, rel, 10)
		totalMRR += eval.MRR(results, rel)
		totalNDCG += eval.NDCGAtK(results, rel, 10)
	}
	n := float64(len(cases))
	fmt.Printf("SQLite FTS5 baseline (%d queries)\n", len(cases))
	fmt.Printf("  P@10:    %.3f\n", totalP/n)
	fmt.Printf("  MRR:     %.3f\n", totalMRR/n)
	fmt.Printf("  NDCG@10: %.3f\n", totalNDCG/n)
}
```

- [ ] **Step 7: Commit**

```bash
git add wikisearch/
git commit -m "feat: eval metrics (P@k, MRR, NDCG), SQLite FTS5 baseline"
```

---

## Sprint 5 — Persistence (The Hard One)

**Learning goal:** Real search engines don't rebuild from scratch on startup. You'll write the index to disk using a compact binary format, then read it back via memory-mapped I/O. This is the foundation of Lucene's segment architecture.

### Task 11: Varint codec

**Files:**
- Create: `wikisearch/internal/postings/varint.go`
- Create: `wikisearch/internal/postings/varint_test.go`

**Interfaces:**
- Produces: `postings.AppendUvarint(buf []byte, x uint64) []byte`, `postings.ReadUvarint(buf []byte, offset int) (uint64, int)`

- [ ] **Step 1: Write failing tests**

`internal/postings/varint_test.go`:
```go
package postings_test

import (
	"testing"
	"wikisearch/internal/postings"
)

func TestVarintRoundtrip(t *testing.T) {
	cases := []uint64{0, 1, 127, 128, 255, 16383, 16384, 1<<32 - 1}
	for _, v := range cases {
		buf := postings.AppendUvarint(nil, v)
		got, n := postings.ReadUvarint(buf, 0)
		if got != v {
			t.Errorf("roundtrip(%d): got %d", v, got)
		}
		if n <= 0 {
			t.Errorf("ReadUvarint returned n=%d", n)
		}
	}
}

func TestVarintSmallNumbers(t *testing.T) {
	// values < 128 should fit in 1 byte
	buf := postings.AppendUvarint(nil, 42)
	if len(buf) != 1 {
		t.Errorf("value 42 encoded in %d bytes, want 1", len(buf))
	}
}
```

- [ ] **Step 2: Implement varint**

`internal/postings/varint.go`:
```go
package postings

// AppendUvarint encodes x as a variable-length unsigned integer and appends to buf.
// Values < 128 use 1 byte. Each byte uses 7 bits of value; bit 8 = "more bytes follow".
func AppendUvarint(buf []byte, x uint64) []byte {
	for x >= 0x80 {
		buf = append(buf, byte(x)|0x80)
		x >>= 7
	}
	return append(buf, byte(x))
}

// ReadUvarint decodes a varint from buf starting at offset.
// Returns the value and number of bytes consumed.
func ReadUvarint(buf []byte, offset int) (uint64, int) {
	var x uint64
	var shift uint
	for i := offset; i < len(buf); i++ {
		b := buf[i]
		x |= uint64(b&0x7F) << shift
		shift += 7
		if b < 0x80 {
			return x, i - offset + 1
		}
	}
	return 0, -1 // truncated
}
```

- [ ] **Step 3: Run and commit**

```bash
go test ./internal/postings/... -v
git add wikisearch/internal/postings/
git commit -m "feat: varint codec (uvarint encode/decode)"
```

---

### Task 12: Segment writer + reader

**Files:**
- Create: `wikisearch/internal/index/writer.go`
- Create: `wikisearch/internal/index/segment.go`
- Create: `wikisearch/internal/index/segment_test.go`

**Interfaces:**
- Consumes: `index.MemoryIndex`, `postings.AppendUvarint`, `postings.ReadUvarint`
- Produces: `index.WriteSegment(m *MemoryIndex, dir string) error`, `index.OpenSegment(dir string) (Index, error)`

**Segment format:**
```
segment.dict   — sorted terms with offsets into .post
segment.post   — posting lists (gap-encoded docIDs + termFreq, both uvarint)
segment.docs   — docID→title,URL,length (JSON lines for simplicity)
```

- [ ] **Step 1: Write failing test**

`internal/index/segment_test.go`:
```go
package index_test

import (
	"testing"

	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

func TestSegmentRoundtrip(t *testing.T) {
	docs := []corpus.Document{
		{ID: 0, Title: "Alpha", Text: "alpha beta gamma"},
		{ID: 1, Title: "Beta",  Text: "beta gamma delta"},
		{ID: 2, Title: "Gamma", Text: "gamma delta epsilon"},
	}
	a := analysis.NewAnalyzer()
	mem := index.NewMemoryIndex()
	for _, d := range docs {
		mem.Add(d, a)
	}
	mem.Finalize()

	dir := t.TempDir()
	if err := index.WriteSegment(mem, dir); err != nil {
		t.Fatalf("WriteSegment: %v", err)
	}

	seg, err := index.OpenSegment(dir)
	if err != nil {
		t.Fatalf("OpenSegment: %v", err)
	}

	// NumDocs must match
	if seg.NumDocs() != mem.NumDocs() {
		t.Errorf("NumDocs: got %d, want %d", seg.NumDocs(), mem.NumDocs())
	}

	// Lookup must return same posting list
	for _, term := range []string{"gamma", "beta", "delta"} {
		memPL, memOK := mem.Lookup(term)
		segPL, segOK := seg.Lookup(term)
		if memOK != segOK {
			t.Errorf("term %q: mem ok=%v seg ok=%v", term, memOK, segOK)
			continue
		}
		if !memOK {
			continue
		}
		if memPL.DocFreq != segPL.DocFreq {
			t.Errorf("term %q: docFreq mem=%d seg=%d", term, memPL.DocFreq, segPL.DocFreq)
		}
		for i := range memPL.Entries {
			if memPL.Entries[i].DocID != segPL.Entries[i].DocID {
				t.Errorf("term %q entry %d: docID mem=%d seg=%d", term, i, memPL.Entries[i].DocID, segPL.Entries[i].DocID)
			}
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/index/... -run TestSegmentRoundtrip -v
```

- [ ] **Step 3: Implement writer**

`internal/index/writer.go`:
```go
package index

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"wikisearch/internal/corpus"
	"wikisearch/internal/postings"
)

type dictEntry struct {
	term   string
	offset uint64
	length uint64
}

// WriteSegment writes a MemoryIndex to three files in dir:
//   segment.post — posting lists (gap-encoded docIDs, varint)
//   segment.dict — term dictionary with offsets
//   segment.docs — docID metadata (JSON lines)
func WriteSegment(m *MemoryIndex, dir string) error {
	// 1. Sort terms
	terms := make([]string, 0, len(m.postings))
	for t := range m.postings {
		terms = append(terms, t)
	}
	sort.Strings(terms)

	// 2. Write postings file
	postFile, err := os.Create(filepath.Join(dir, "segment.post"))
	if err != nil {
		return err
	}
	defer postFile.Close()

	dictEntries := make([]dictEntry, 0, len(terms))
	var offset uint64
	for _, term := range terms {
		entries := m.postings[term]
		var buf []byte
		buf = postings.AppendUvarint(buf, uint64(len(entries))) // docFreq
		var prevID uint32
		for _, e := range entries {
			gap := e.DocID - prevID
			buf = postings.AppendUvarint(buf, uint64(gap))
			buf = postings.AppendUvarint(buf, uint64(e.TermFreq))
			prevID = e.DocID
		}
		postFile.Write(buf)
		dictEntries = append(dictEntries, dictEntry{term: term, offset: offset, length: uint64(len(buf))})
		offset += uint64(len(buf))
	}

	// 3. Write dictionary file
	dictFile, err := os.Create(filepath.Join(dir, "segment.dict"))
	if err != nil {
		return err
	}
	defer dictFile.Close()

	// Format: [uint32 numTerms] then for each: [uint16 termLen][termBytes][uint64 offset][uint64 len]
	binary.Write(dictFile, binary.LittleEndian, uint32(len(dictEntries)))
	for _, de := range dictEntries {
		tb := []byte(de.term)
		binary.Write(dictFile, binary.LittleEndian, uint16(len(tb)))
		dictFile.Write(tb)
		binary.Write(dictFile, binary.LittleEndian, de.offset)
		binary.Write(dictFile, binary.LittleEndian, de.length)
	}

	// 4. Write docs file (JSON lines: one doc per line)
	docsFile, err := os.Create(filepath.Join(dir, "segment.docs"))
	if err != nil {
		return err
	}
	defer docsFile.Close()

	type docRecord struct {
		Title  string `json:"title"`
		Length uint32 `json:"length"`
	}
	enc := json.NewEncoder(docsFile)
	for _, doc := range m.docs {
		enc.Encode(docRecord{Title: doc.Title, Length: doc.Length})
	}
	return nil
}

// writeUint32 is a helper used internally.
func writeUint32(f *os.File, v uint32) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, v)
	f.Write(buf)
}
```

- [ ] **Step 4: Implement segment reader**

`internal/index/segment.go`:
```go
package index

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"wikisearch/internal/corpus"
	"wikisearch/internal/postings"
)

type segmentIndex struct {
	// dictionary: term → (offset, length) in postings file
	dict map[string][2]uint64
	// postings data loaded in memory (for simplicity; Phase 3 uses mmap)
	postData []byte
	// doc metadata
	docs    []corpus.Document
	docLens []uint32
	totalLen uint64
}

// OpenSegment reads the three segment files and returns an Index.
func OpenSegment(dir string) (Index, error) {
	s := &segmentIndex{dict: make(map[string][2]uint64)}

	// Read dictionary
	dictData, err := os.ReadFile(filepath.Join(dir, "segment.dict"))
	if err != nil {
		return nil, fmt.Errorf("read dict: %w", err)
	}
	pos := 0
	numTerms := binary.LittleEndian.Uint32(dictData[pos:])
	pos += 4
	for i := uint32(0); i < numTerms; i++ {
		tLen := int(binary.LittleEndian.Uint16(dictData[pos:]))
		pos += 2
		term := string(dictData[pos : pos+tLen])
		pos += tLen
		offset := binary.LittleEndian.Uint64(dictData[pos:])
		pos += 8
		length := binary.LittleEndian.Uint64(dictData[pos:])
		pos += 8
		s.dict[term] = [2]uint64{offset, length}
	}

	// Read postings into memory
	s.postData, err = os.ReadFile(filepath.Join(dir, "segment.post"))
	if err != nil {
		return nil, fmt.Errorf("read post: %w", err)
	}

	// Read docs
	docsFile, err := os.Open(filepath.Join(dir, "segment.docs"))
	if err != nil {
		return nil, fmt.Errorf("read docs: %w", err)
	}
	defer docsFile.Close()

	type docRecord struct {
		Title  string `json:"title"`
		Length uint32 `json:"length"`
	}
	sc := bufio.NewScanner(docsFile)
	var id uint32
	for sc.Scan() {
		var rec docRecord
		json.Unmarshal(sc.Bytes(), &rec)
		s.docs = append(s.docs, corpus.Document{ID: id, Title: rec.Title, Length: rec.Length})
		s.docLens = append(s.docLens, rec.Length)
		s.totalLen += uint64(rec.Length)
		id++
	}
	return s, nil
}

func (s *segmentIndex) Lookup(term string) (PostingList, bool) {
	loc, ok := s.dict[term]
	if !ok {
		return PostingList{}, false
	}
	data := s.postData[loc[0] : loc[0]+loc[1]]
	docFreq, n := postings.ReadUvarint(data, 0)
	off := n
	entries := make([]PostingEntry, docFreq)
	var prevID uint32
	for i := range entries {
		gap, n1 := postings.ReadUvarint(data, off)
		off += n1
		tf, n2 := postings.ReadUvarint(data, off)
		off += n2
		prevID += uint32(gap)
		entries[i] = PostingEntry{DocID: prevID, TermFreq: uint32(tf)}
	}
	return PostingList{Term: term, DocFreq: uint32(docFreq), Entries: entries}, true
}

func (s *segmentIndex) NumDocs() uint32   { return uint32(len(s.docs)) }
func (s *segmentIndex) DocLen(id uint32) uint32 { return s.docLens[id] }
func (s *segmentIndex) AvgDocLen() float64 {
	if len(s.docs) == 0 {
		return 0
	}
	return float64(s.totalLen) / float64(len(s.docs))
}
func (s *segmentIndex) Doc(id uint32) (corpus.Document, error) {
	if int(id) >= len(s.docs) {
		return corpus.Document{}, fmt.Errorf("doc %d out of range", id)
	}
	return s.docs[id], nil
}
```

- [ ] **Step 5: Run — expect PASS**

```bash
go test ./internal/index/... -v
```

- [ ] **Step 6: Update cmd/index to write segment, cmd/search to load it**

Modify `cmd/index/main.go` to call `index.WriteSegment(idx, "data/index")` after `Finalize()`.

Modify `cmd/search/main.go` to accept `-index` flag that loads `index.OpenSegment("data/index")` instead of rebuilding.

- [ ] **Step 7: Verify eval scores unchanged**

Run eval against both in-memory and on-disk index. NDCG must be identical. Any difference = codec bug.

- [ ] **Step 8: Commit**

```bash
git add wikisearch/internal/index/ wikisearch/cmd/
git commit -m "feat: segment writer/reader, gap-encoded postings, persistent index"
```

---

## Sprint 6 — Richer Retrieval (Phrase + Parser + Snippets)

### Task 13: Positional index + phrase queries

**Files:**
- Modify: `wikisearch/internal/index/memory.go` — store positions in PostingEntry
- Modify: `wikisearch/internal/index/writer.go` / `segment.go` — encode/decode positions
- Create: `wikisearch/internal/postings/phrase.go`
- Create: `wikisearch/internal/postings/phrase_test.go`

**Learning goal:** Position lists let you distinguish `"climate change"` (adjacent) from `climate AND change` (anywhere in the doc). The index roughly doubles in size because you store one entry per occurrence, not per document.

- [ ] **Step 1: Write failing phrase test**

`internal/postings/phrase_test.go`:
```go
package postings_test

import (
	"testing"
	"wikisearch/internal/index"
	"wikisearch/internal/postings"
)

func plWithPos(docID uint32, positions ...uint32) index.PostingEntry {
	return index.PostingEntry{DocID: docID, TermFreq: uint32(len(positions)), Positions: positions}
}

func TestPhraseMatch(t *testing.T) {
	// "climate" at positions 2,5 in doc0; "change" at positions 3,9 in doc0
	climate := index.PostingList{Entries: []index.PostingEntry{
		plWithPos(0, 2, 5),
		plWithPos(1, 0),
	}}
	change := index.PostingList{Entries: []index.PostingEntry{
		plWithPos(0, 3, 9), // position 3 = climate(2)+1 ✓
		plWithPos(2, 1),
	}}

	result := postings.PhraseIntersect(climate, change, 1) // gap=1 for adjacent
	if len(result.Entries) != 1 {
		t.Fatalf("got %d docs, want 1", len(result.Entries))
	}
	if result.Entries[0].DocID != 0 {
		t.Errorf("got docID %d, want 0", result.Entries[0].DocID)
	}
}
```

- [ ] **Step 2: Implement PhraseIntersect**

`internal/postings/phrase.go`:
```go
package postings

import "wikisearch/internal/index"

// PhraseIntersect returns documents where term b appears exactly (gap) positions
// after term a. gap=1 means adjacent (phrase query).
func PhraseIntersect(a, b index.PostingList, gap uint32) index.PostingList {
	var out []index.PostingEntry
	i, j := 0, 0
	for i < len(a.Entries) && j < len(b.Entries) {
		ai, bj := a.Entries[i].DocID, b.Entries[j].DocID
		if ai < bj {
			i++
		} else if ai > bj {
			j++
		} else {
			// same doc — check if any position pair satisfies the gap
			if hasPositionGap(a.Entries[i].Positions, b.Entries[j].Positions, gap) {
				out = append(out, index.PostingEntry{DocID: ai, TermFreq: 1})
			}
			i++
			j++
		}
	}
	return index.PostingList{DocFreq: uint32(len(out)), Entries: out}
}

// hasPositionGap returns true if any posA + gap == posB.
func hasPositionGap(posA, posB []uint32, gap uint32) bool {
	p, q := 0, 0
	for p < len(posA) && q < len(posB) {
		target := posA[p] + gap
		if posB[q] == target {
			return true
		} else if posB[q] < target {
			q++
		} else {
			p++
		}
	}
	return false
}
```

- [ ] **Step 3: Update MemoryIndex.Add to store positions**

In `internal/index/memory.go`, change the build loop to record positions per term per document. Update `PostingEntry.Positions` field to be populated during `Add`.

- [ ] **Step 4: Run and commit**

```bash
go test ./internal/postings/... -v
git add wikisearch/
git commit -m "feat: positional index, phrase intersection"
```

---

### Task 14: Query parser (lexer + recursive descent)

**Files:**
- Create: `wikisearch/internal/query/lexer.go`
- Create: `wikisearch/internal/query/parser.go`
- Create: `wikisearch/internal/query/ast.go`
- Create: `wikisearch/internal/query/parser_test.go`

**Learning goal:** A recursive-descent parser is the standard approach for small grammars. Each grammar rule becomes one function. The grammar here is small but realistic.

Grammar:
```
expr   := term (OR term)*
term   := factor (AND? factor)*       // AND is optional (implicit)
factor := NOT? atom
atom   := WORD | PHRASE | FIELD:atom | '(' expr ')'
```

- [ ] **Step 1: Write AST types**

`internal/query/ast.go`:
```go
package query

// Node is a query AST node.
type Node interface{ node() }

type AndNode  struct{ Children []Node }
type OrNode   struct{ Children []Node }
type NotNode  struct{ Child Node }
type TermNode struct{ Term string }
type PhraseNode struct{ Terms []string }
type FieldNode struct{ Field string; Child Node }

func (*AndNode) node()    {}
func (*OrNode) node()     {}
func (*NotNode) node()    {}
func (*TermNode) node()   {}
func (*PhraseNode) node() {}
func (*FieldNode) node()  {}
```

- [ ] **Step 2: Write failing parser test**

`internal/query/parser_test.go`:
```go
package query_test

import (
	"testing"
	"wikisearch/internal/query"
)

func TestParseAnd(t *testing.T) {
	node, err := query.Parse("foo bar")
	if err != nil {
		t.Fatal(err)
	}
	and, ok := node.(*query.AndNode)
	if !ok {
		t.Fatalf("got %T, want *AndNode", node)
	}
	if len(and.Children) != 2 {
		t.Fatalf("got %d children, want 2", len(and.Children))
	}
}

func TestParsePhrase(t *testing.T) {
	node, err := query.Parse(`"climate change"`)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := node.(*query.PhraseNode)
	if !ok {
		t.Fatalf("got %T, want *PhraseNode", node)
	}
}

func TestParseOr(t *testing.T) {
	node, err := query.Parse("foo OR bar")
	if err != nil {
		t.Fatal(err)
	}
	_, ok := node.(*query.OrNode)
	if !ok {
		t.Fatalf("got %T, want *OrNode", node)
	}
}

func TestParseNot(t *testing.T) {
	node, err := query.Parse("foo NOT bar")
	if err != nil {
		t.Fatal(err)
	}
	and, ok := node.(*query.AndNode)
	if !ok {
		t.Fatalf("got %T, want *AndNode", node)
	}
	_, isNot := and.Children[1].(*query.NotNode)
	if !isNot {
		t.Fatalf("second child: got %T, want *NotNode", and.Children[1])
	}
}
```

- [ ] **Step 3: Run — expect FAIL**

```bash
go test ./internal/query/... -v
```

- [ ] **Step 4: Implement lexer**

`internal/query/lexer.go`:
```go
package query

import "strings"

type tokenType int

const (
	tokWord tokenType = iota
	tokPhrase
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
	tokColon
	tokEOF
)

type lexToken struct {
	typ tokenType
	val string
}

func lex(input string) []lexToken {
	var tokens []lexToken
	i := 0
	for i < len(input) {
		switch {
		case input[i] == ' ' || input[i] == '\t':
			i++
		case input[i] == '(':
			tokens = append(tokens, lexToken{tokLParen, "("})
			i++
		case input[i] == ')':
			tokens = append(tokens, lexToken{tokRParen, ")"})
			i++
		case input[i] == ':':
			tokens = append(tokens, lexToken{tokColon, ":"})
			i++
		case input[i] == '"':
			end := strings.Index(input[i+1:], `"`)
			if end == -1 {
				end = len(input)
			} else {
				end = i + 1 + end
			}
			tokens = append(tokens, lexToken{tokPhrase, input[i+1 : end]})
			i = end + 1
		default:
			j := i
			for j < len(input) && input[j] != ' ' && input[j] != ')' && input[j] != '(' && input[j] != ':' && input[j] != '"' {
				j++
			}
			word := input[i:j]
			switch strings.ToUpper(word) {
			case "AND":
				tokens = append(tokens, lexToken{tokAnd, word})
			case "OR":
				tokens = append(tokens, lexToken{tokOr, word})
			case "NOT":
				tokens = append(tokens, lexToken{tokNot, word})
			default:
				tokens = append(tokens, lexToken{tokWord, word})
			}
			i = j
		}
	}
	tokens = append(tokens, lexToken{tokEOF, ""})
	return tokens
}
```

- [ ] **Step 5: Implement recursive-descent parser**

`internal/query/parser.go`:
```go
package query

import "fmt"

type parser struct {
	tokens []lexToken
	pos    int
}

func (p *parser) peek() lexToken  { return p.tokens[p.pos] }
func (p *parser) next() lexToken  { t := p.tokens[p.pos]; p.pos++; return t }
func (p *parser) done() bool      { return p.peek().typ == tokEOF }

// Parse parses a query string into an AST node.
func Parse(input string) (Node, error) {
	p := &parser{tokens: lex(input)}
	node := p.parseExpr()
	if !p.done() {
		return nil, fmt.Errorf("unexpected token: %q", p.peek().val)
	}
	return node, nil
}

func (p *parser) parseExpr() Node {
	left := p.parseTerm()
	var children []Node
	children = append(children, left)
	for p.peek().typ == tokOr {
		p.next()
		children = append(children, p.parseTerm())
	}
	if len(children) == 1 {
		return children[0]
	}
	return &OrNode{Children: children}
}

func (p *parser) parseTerm() Node {
	var children []Node
	for {
		// consume optional AND
		if p.peek().typ == tokAnd {
			p.next()
		}
		tok := p.peek()
		if tok.typ == tokEOF || tok.typ == tokRParen || tok.typ == tokOr {
			break
		}
		children = append(children, p.parseFactor())
		if len(children) == 0 {
			break
		}
	}
	if len(children) == 1 {
		return children[0]
	}
	return &AndNode{Children: children}
}

func (p *parser) parseFactor() Node {
	if p.peek().typ == tokNot {
		p.next()
		return &NotNode{Child: p.parseAtom()}
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() Node {
	tok := p.next()
	switch tok.typ {
	case tokWord:
		// check for field:
		if p.peek().typ == tokColon {
			p.next()
			child := p.parseAtom()
			return &FieldNode{Field: tok.val, Child: child}
		}
		return &TermNode{Term: tok.val}
	case tokPhrase:
		return &PhraseNode{Terms: splitPhrase(tok.val)}
	case tokLParen:
		node := p.parseExpr()
		if p.peek().typ == tokRParen {
			p.next()
		}
		return node
	default:
		return &TermNode{Term: tok.val}
	}
}

func splitPhrase(s string) []string {
	var out []string
	for _, w := range splitWords(s) {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

func splitWords(s string) []string {
	var words []string
	var cur []byte
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			if len(cur) > 0 {
				words = append(words, string(cur))
				cur = cur[:0]
			}
		} else {
			cur = append(cur, s[i])
		}
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}
```

- [ ] **Step 6: Run — expect PASS**

```bash
go test ./internal/query/... -v
```

- [ ] **Step 7: Commit**

```bash
git add wikisearch/internal/query/
git commit -m "feat: query lexer and recursive-descent parser (AND/OR/NOT/phrase/field)"
```

---

### Task 15: Snippet generation

**Files:**
- Create: `wikisearch/internal/rank/snippet.go`
- Create: `wikisearch/internal/rank/snippet_test.go`

**Interfaces:**
- Produces: `rank.Snippet(text string, queryTerms []string, windowSize int) string`

- [ ] **Step 1: Write failing test**

`internal/rank/snippet_test.go`:
```go
package rank_test

import (
	"strings"
	"testing"
	"wikisearch/internal/rank"
)

func TestSnippetContainsQueryTerm(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog. " +
		"Photosynthesis is a process used by plants to convert light. " +
		"The dog barked loudly."
	snip := rank.Snippet(text, []string{"photosynthesi"}, 100)
	if !strings.Contains(strings.ToLower(snip), "photosynthes") {
		t.Errorf("snippet missing query term: %q", snip)
	}
}
```

- [ ] **Step 2: Implement snippet**

`internal/rank/snippet.go`:
```go
package rank

import (
	"strings"
	"unicode"
)

// Snippet finds the text window with highest query term density
// and returns ~windowSize characters around it, with terms highlighted.
func Snippet(text string, queryTerms []string, windowSize int) string {
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) == 0 {
		return ""
	}

	termSet := make(map[string]bool)
	for _, t := range queryTerms {
		termSet[strings.ToLower(t)] = true
	}

	// find word index with highest hit density in a window
	wSize := windowSize / 5 // rough word count
	bestStart, bestHits := 0, -1
	for i := range words {
		end := i + wSize
		if end > len(words) {
			end = len(words)
		}
		hits := 0
		for _, w := range words[i:end] {
			if termSet[strings.ToLower(w)] {
				hits++
			}
		}
		if hits > bestHits {
			bestHits = hits
			bestStart = i
		}
	}

	end := bestStart + wSize
	if end > len(words) {
		end = len(words)
	}
	snippet := strings.Join(words[bestStart:end], " ")

	// highlight query terms with ANSI bold
	for _, term := range queryTerms {
		// case-insensitive highlight — simple approach
		lower := strings.ToLower(snippet)
		lowerTerm := strings.ToLower(term)
		idx := strings.Index(lower, lowerTerm)
		if idx >= 0 {
			orig := snippet[idx : idx+len(term)]
			snippet = strings.ReplaceAll(snippet, orig, "\033[1m"+orig+"\033[0m")
		}
	}
	return snippet
}
```

- [ ] **Step 3: Run and commit**

```bash
go test ./internal/rank/... -v -run TestSnippet
git add wikisearch/internal/rank/
git commit -m "feat: snippet generation with ANSI term highlighting"
```

---

## Sprint 7 — PageRank

### Task 16: Link graph + PageRank

**Files:**
- Create: `wikisearch/internal/link/pagerank.go`
- Create: `wikisearch/internal/link/pagerank_test.go`

**Learning goal:** PageRank is a query-independent signal — articles cited by many other articles are probably more important. Wikipedia's editorial link graph is dense and curated, making it an unusually good corpus for link analysis.

- [ ] **Step 1: Write failing test**

`internal/link/pagerank_test.go`:
```go
package link_test

import (
	"testing"
	"wikisearch/internal/link"
)

func TestPageRankSums(t *testing.T) {
	// Simple 3-node graph: 0→1, 0→2, 1→2
	graph := link.Graph{
		0: []uint32{1, 2},
		1: []uint32{2},
		2: []uint32{},
	}
	scores := link.PageRank(graph, 3, 0.85, 30)
	var total float64
	for _, s := range scores {
		total += s
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("PageRank scores sum to %f, want ~1.0", total)
	}
	// node 2 receives most links — should have highest rank
	if !(scores[2] > scores[1] && scores[2] > scores[0]) {
		t.Errorf("node 2 should have highest rank: %v", scores)
	}
}
```

- [ ] **Step 2: Implement PageRank**

`internal/link/pagerank.go`:
```go
package link

// Graph maps docID → outgoing docIDs.
type Graph map[uint32][]uint32

// PageRank runs power iteration and returns per-doc scores that sum to 1.
// d=0.85 is the standard damping factor; iters=30 is typically enough to converge.
func PageRank(graph Graph, numDocs int, d float64, iters int) []float64 {
	N := float64(numDocs)
	scores := make([]float64, numDocs)
	next := make([]float64, numDocs)

	// Initialize uniformly
	for i := range scores {
		scores[i] = 1.0 / N
	}

	// Build in-link lists for efficiency
	inLinks := make([][]uint32, numDocs)
	for src, dests := range graph {
		for _, dest := range dests {
			if int(dest) < numDocs {
				inLinks[dest] = append(inLinks[dest], src)
			}
		}
	}

	for iter := 0; iter < iters; iter++ {
		// Collect dangling rank (nodes with no outlinks)
		var dangling float64
		for id := 0; id < numDocs; id++ {
			if len(graph[uint32(id)]) == 0 {
				dangling += scores[id]
			}
		}

		for id := 0; id < numDocs; id++ {
			// Teleport + dangling redistribution
			next[id] = (1-d)/N + d*dangling/N
			// Add rank from in-links
			for _, src := range inLinks[uint32(id)] {
				outdeg := float64(len(graph[src]))
				if outdeg > 0 {
					next[id] += d * scores[src] / outdeg
				}
			}
		}
		copy(scores, next)
	}
	return scores
}
```

- [ ] **Step 3: Build graph from corpus + blend into BM25**

Add `link.BuildGraph(idx index.Index) Graph` that iterates docs and maps title→docID from outgoing_link. Then in `cmd/search`, after building the index:

```go
graph := link.BuildGraph(idx)
prScores := link.PageRank(graph, int(idx.NumDocs()), 0.85, 30)

// Blend: final = bm25 + w * log(1 + pagerank)
// Start w=1.0, tune against eval
import "math"
for i := range results {
    pr := prScores[results[i].DocID]
    results[i].Score += 1.0 * math.Log1p(pr)
}
```

- [ ] **Step 4: Run and measure**

```bash
go test ./internal/link/... -v
go run ./cmd/eval -dump data/sample.json.gz   # measure NDCG before/after PageRank
```

- [ ] **Step 5: Commit**

```bash
git add wikisearch/internal/link/ wikisearch/cmd/
git commit -m "feat: PageRank power iteration, blend into BM25 score"
```

---

## Sprint 8 — Performance

### Task 17: Benchmarks + profiling baseline

**Files:**
- Create: `wikisearch/internal/index/bench_test.go`
- Create: `wikisearch/internal/postings/bench_test.go`

- [ ] **Step 1: Write benchmarks**

`internal/index/bench_test.go`:
```go
package index_test

import (
	"testing"
	"wikisearch/internal/analysis"
	"wikisearch/internal/corpus"
	"wikisearch/internal/index"
)

var benchIdx index.Index

func BenchmarkBuild(b *testing.B) {
	a := analysis.NewAnalyzer()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := index.NewMemoryIndex()
		for j := 0; j < 1000; j++ {
			idx.Add(corpus.Document{ID: uint32(j), Title: "Doc", Text: "photosynthesis plant process light energy"}, a)
		}
		idx.Finalize()
		benchIdx = idx
	}
}

func BenchmarkLookup(b *testing.B) {
	a := analysis.NewAnalyzer()
	idx := index.NewMemoryIndex()
	for j := 0; j < 10000; j++ {
		idx.Add(corpus.Document{ID: uint32(j), Title: "Doc", Text: "photosynthesis plant process light energy"}, a)
	}
	idx.Finalize()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Lookup("photosynthesi")
	}
}
```

- [ ] **Step 2: Record baseline numbers**

```bash
cd wikisearch
go test ./internal/... -bench=. -benchmem -count=3 > bench_baseline.txt
cat bench_baseline.txt
```

These numbers are your before. Every optimization must beat them.

- [ ] **Step 3: Profile**

```bash
go test ./internal/index/... -bench=BenchmarkBuild -cpuprofile=cpu.out -memprofile=mem.out
go tool pprof -http=:8080 cpu.out   # open browser, look at flame graph
```

Note the top 3 hot functions. Usually: JSON decode, map operations, or slice allocations in the posting loop.

- [ ] **Step 4: Commit**

```bash
git add wikisearch/
git commit -m "perf: add benchmarks, record baseline for optimization work"
```

---

### Task 18: Skip pointers + WAND

**Files:**
- Modify: `wikisearch/internal/postings/ops.go` — add skip-pointer-aware intersection
- Create: `wikisearch/internal/rank/wand.go`
- Create: `wikisearch/internal/rank/wand_test.go`

**Learning goal:** Skip pointers let intersection jump over large gaps instead of scanning entry by entry. WAND skips entire documents that can't beat the current top-k threshold.

- [ ] **Step 1: Add galloping search to Intersect**

Update `Intersect` in `ops.go` to use exponential search when advancing the pointer:

```go
// gallopTo advances entries slice past docID using exponential then binary search.
// More efficient than linear scan when gaps are large.
func gallopTo(entries []PostingEntry, docID uint32, start int) int {
	i := start
	step := 1
	for i+step < len(entries) && entries[i+step].DocID < docID {
		i += step
		step *= 2
	}
	// binary search in [i, min(i+step, len)]
	lo, hi := i, len(entries)
	if i+step < hi {
		hi = i + step
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if entries[mid].DocID < docID {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
```

- [ ] **Step 2: Implement WAND**

`internal/rank/wand.go`:
```go
package rank

import (
	"sort"
	"wikisearch/internal/index"
)

// TermState holds the current position in a posting list during WAND traversal.
type TermState struct {
	PL      index.PostingList
	Pos     int
	MaxScore float64 // upper bound on contribution of this term
}

// WAND runs Weak AND top-k retrieval.
// It skips documents whose upper-bound score can't beat the k-th best seen so far.
// scorer must implement Score(entry, docFreq, docLen) float64.
func WAND(terms []TermState, k int, idx index.Index, scorer Scorer) []Result {
	// ponytail: simplified WAND — full block-max WAND adds per-block maxima
	heap := &minHeap{}

	// threshold: the k-th best score seen so far (0 until heap is full)
	threshold := 0.0

	for {
		// Sort terms by current docID (ascending)
		sort.Slice(terms, func(i, j int) bool {
			if terms[i].Pos >= len(terms[i].PL.Entries) {
				return false
			}
			if terms[j].Pos >= len(terms[j].PL.Entries) {
				return true
			}
			return terms[i].PL.Entries[terms[i].Pos].DocID < terms[j].PL.Entries[terms[j].Pos].DocID
		})

		// Find pivot: first term where cumulative max score > threshold
		var cumScore float64
		pivot := -1
		pivotDocID := uint32(0)
		for i, ts := range terms {
			if ts.Pos >= len(ts.PL.Entries) {
				continue
			}
			cumScore += ts.MaxScore
			if cumScore > threshold {
				pivot = i
				pivotDocID = ts.PL.Entries[ts.Pos].DocID
				break
			}
		}
		if pivot == -1 {
			break // no more candidates can beat threshold
		}

		// Check if all terms before pivot point past pivotDocID
		allAtPivot := true
		for i := 0; i < pivot; i++ {
			if terms[i].Pos < len(terms[i].PL.Entries) &&
				terms[i].PL.Entries[terms[i].Pos].DocID < pivotDocID {
				allAtPivot = false
				// Advance this term to pivotDocID
				for terms[i].Pos < len(terms[i].PL.Entries) &&
					terms[i].PL.Entries[terms[i].Pos].DocID < pivotDocID {
					terms[i].Pos++
				}
			}
		}

		if allAtPivot {
			// Score this document fully
			var score float64
			for i := range terms {
				if terms[i].Pos < len(terms[i].PL.Entries) &&
					terms[i].PL.Entries[terms[i].Pos].DocID == pivotDocID {
					entry := terms[i].PL.Entries[terms[i].Pos]
					score += scorer.Score(entry, terms[i].PL.DocFreq, idx.DocLen(pivotDocID))
					terms[i].Pos++
				}
			}
			pushResult(heap, Result{DocID: pivotDocID, Score: score}, k, &threshold)
		}
	}

	out := make([]Result, heap.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = (*heap)[0]
		*heap = (*heap)[1:]
	}
	return out
}

func pushResult(h *minHeap, r Result, k int, threshold *float64) {
	import_heap_push(h, r)
	if h.Len() > k {
		import_heap_pop(h)
	}
	if h.Len() == k {
		*threshold = (*h)[0].Score
	}
}
```

Note: WAND is complex — implement, test against TopK output to verify identical results on small inputs, then benchmark the speedup.

- [ ] **Step 3: Commit**

```bash
git add wikisearch/
git commit -m "perf: skip pointers (galloping search), WAND early termination"
```

---

## Sprint 9 — Elasticsearch Comparison

### Task 19: Load corpus into Elasticsearch

**Files:**
- Create: `wikisearch/cmd/esload/main.go`

- [ ] **Step 1: Start Elasticsearch**

```bash
docker run -d --name es \
  -p 9200:9200 \
  -e "discovery.type=single-node" \
  -e "xpack.security.enabled=false" \
  docker.elastic.co/elasticsearch/elasticsearch:8.14.0
```

- [ ] **Step 2: Create index with analyzer config**

```bash
curl -X PUT "localhost:9200/wiki" -H 'Content-Type: application/json' -d '{
  "settings": {
    "refresh_interval": "-1",
    "number_of_replicas": 0,
    "analysis": {
      "analyzer": {
        "wiki_analyzer": {
          "type": "english"
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "title": { "type": "text", "analyzer": "wiki_analyzer", "boost": 2 },
      "text":  { "type": "text", "analyzer": "wiki_analyzer" }
    }
  }
}'
```

- [ ] **Step 3: Bulk load**

`cmd/esload/main.go` — reads dump, converts to NDJSON bulk format, POSTs in batches of 500:

```go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"wikisearch/internal/corpus"
)

func main() {
	dump := flag.String("dump", "", "path to dump")
	flag.Parse()

	r, err := corpus.NewReader(*dump)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	var buf bytes.Buffer
	batch := 0
	flush := func() {
		resp, err := http.Post("http://localhost:9200/wiki/_bulk",
			"application/x-ndjson", &buf)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
		buf.Reset()
		batch++
		fmt.Printf("\rflushed batch %d", batch)
	}

	count := 0
	for {
		doc, ok, err := r.Next()
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			break
		}
		// action line
		fmt.Fprintf(&buf, `{"index":{"_id":"%d"}}`+"\n", doc.ID)
		// doc line
		body, _ := json.Marshal(map[string]string{"title": doc.Title, "text": doc.Text})
		buf.Write(body)
		buf.WriteByte('\n')
		count++
		if count%500 == 0 {
			flush()
		}
	}
	if buf.Len() > 0 {
		flush()
	}

	// Restore refresh
	http.Post("http://localhost:9200/wiki/_settings",
		"application/json",
		bytes.NewBufferString(`{"index":{"refresh_interval":"1s"}}`))
	fmt.Printf("\ndone: %d docs\n", count)
}
```

- [ ] **Step 4: Verify analyzer matches yours**

```bash
curl -X POST "localhost:9200/wiki/_analyze" \
  -H 'Content-Type: application/json' \
  -d '{"analyzer":"wiki_analyzer","text":"The running dogs quickly jumped"}'
```

Compare tokens with your analyzer on the same input. Note any differences (possessives, irregular stems, etc).

- [ ] **Step 5: Compare BM25 scores via _explain**

```bash
# Pick a docID from your index, e.g. 0
curl "localhost:9200/wiki/_explain/0" \
  -H 'Content-Type: application/json' \
  -d '{"query":{"match":{"text":"photosynthesis"}}}'
```

The response breaks down idf, tf, avgFieldLength. Compare values against your BM25 for the same doc+term.

- [ ] **Step 6: Point eval at Elasticsearch**

Write `cmd/eseval/main.go` that queries `localhost:9200/wiki/_search` for each query case and computes the same P@10/MRR/NDCG. Print a comparison table:

```
                P@10    MRR   NDCG@10
mine (BM25+PR)  0.xx   0.xx   0.xx
sqlite fts5     0.xx   0.xx   0.xx
elasticsearch   0.xx   0.xx   0.xx
```

- [ ] **Step 7: Commit**

```bash
git add wikisearch/cmd/esload/ wikisearch/cmd/eseval/
git commit -m "feat: Elasticsearch loader, eval comparison — Phase 6 complete"
```

---

## Progress Tracking

- [ ] Sprint 1 — Project setup + dump reader
- [ ] Sprint 2 — Analyzer pipeline
- [ ] Sprint 3 — In-memory index + boolean search + REPL
- [ ] Sprint 4 — Ranking + evaluation harness
- [ ] Sprint 5 — Persistence (segment format, SPIMI)
- [ ] Sprint 6 — Phrase queries + query parser + snippets
- [ ] Sprint 7 — PageRank
- [ ] Sprint 8 — Performance (benchmarks, skip pointers, WAND)
- [ ] Sprint 9 — Elasticsearch comparison

## Key Invariants (Never Break These)

1. **Analyzer symmetry** — index-time and query-time analysis must be identical
2. **Posting list sort order** — entries always sorted by DocID ascending
3. **Position gap preservation** — dropping a stopword leaves a positional gap; do NOT renumber
4. **Eval score stability** — after adding persistence (Sprint 5), NDCG must be identical to in-memory
5. **Even-line rule** — when slicing the dump, always use even line counts
