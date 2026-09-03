# Learning Go Alongside the Search Engine

A companion to `search-engine-project-plan.md`.

This is not a general Go tutorial. It's the subset of Go this specific project
needs, in the order the project needs it, plus the traps that will actually bite
you while writing an inverted index.

---

## The honest framing

Learning a language while building a hard project works, with one caveat worth
naming up front: **you will sometimes not know whether you're stuck on Go or
stuck on search.** Those are very different problems and they get debugged
differently. When you hit a wall, ask which one it is before you start flailing.

The mitigation is a short warm-up so the syntax is automatic before the ideas
get hard. About a week. Skipping it is the main way this goes wrong.

Go is a genuinely good first systems language for this. Small spec — you can
hold the whole language in your head, unlike C++ or Rust. Excellent stdlib for
exactly what you need (`bufio`, `encoding/binary`, `container/heap`). Built-in
testing, benchmarking, and profiling, which the project leans on heavily. Fast
compile times, so iteration feels like a scripting language. And the compiler
catches the errors that matter without a borrow checker arguing with you while
you're also learning what a posting list is.

---

## Week 0 — Warm-up before Phase 1

**~5–7 evenings. Do this first.**

### Day 1–2: A Tour of Go

`go.dev/tour` — the official interactive tour. Do the whole thing, including
the exercises. It's a few hours and covers roughly 70% of the syntax you need.

Pay particular attention to:
- Slices — how they differ from arrays, what `append` actually does
- Maps — declaration, the comma-ok idiom, iteration
- Structs and methods
- Interfaces and implicit satisfaction
- `defer`
- Errors as values

Skip lightly over goroutines and channels for now. See the note below.

### Day 3: Effective Go + errors

Read `go.dev/doc/effective_go`. It's the idiom guide, and reading it early
saves you from writing Java-in-Go for three months.

Then internalize the error pattern, because you'll type it a thousand times:

```go
f, err := os.Open(path)
if err != nil {
    return nil, fmt.Errorf("open dump %s: %w", path, err)
}
defer f.Close()
```

`%w` wraps, preserving the chain for `errors.Is` and `errors.As`. Wrap with
context at each layer; don't just pass `err` up naked.

### Day 4: Tooling

```bash
go mod init <name>      # create a module
go build ./...          # compile everything
go test ./...           # run all tests
go test -run TestFoo    # run one test
go vet ./...            # catch likely bugs
gofmt -w .              # format (non-negotiable in Go)
go doc bufio.Scanner    # read docs in the terminal
```

Set up your editor with `gopls`. Format on save. Go has one formatting style
and arguing with it is wasted energy.

### Day 5–7: Write a small thing

Not part of the search engine — something throwaway that exercises the same
muscles. A word frequency counter is ideal:

- Read a text file line by line with `bufio.Scanner`
- Split into words, lowercase them
- Count in a `map[string]int`
- Sort by count and print the top 20
- Write a table-driven test for the splitter

That's a miniature of Phase 1: streaming I/O, tokenization, maps, sorting,
tests. If you can write it without looking things up constantly, you're ready.

---

## What you need, when

### Phase 1 — Ingestion, analysis, in-memory index

**Packages and I/O**
- Module layout, `internal/` (importable only within your module)
- Exported vs unexported: capital letter = public. There is no `private` keyword.
- `io.Reader` and `io.Writer` — the two most important interfaces in Go.
  Everything composes through them. `os.File` → `bzip2.Reader` → `bufio.Scanner`
  is three `io.Reader`s stacked.

**Strings, bytes, and runes** — *the one that will actually bite you*

Go strings are UTF-8 byte sequences. Indexing gives you a **byte**, not a
character:

```go
s := "café"
len(s)      // 5, not 4 — é is two bytes
s[3]        // 195, a byte, not 'é'

for i, r := range s {   // ranges over runes, i jumps by rune width
    // r is a rune (int32), a Unicode code point
}

[]rune(s)   // 4 elements — allocates, but correct
```

Your tokenizer must be rune-aware or it will silently corrupt every non-ASCII
title in Wikipedia. Use `unicode.IsLetter(r)`, never `r >= 'a' && r <= 'z'`.

Also learn `strings.Builder` for concatenation in loops — `s += x` in a loop is
O(n²).

**Slices** — the other one that will bite you

```go
var s []int          // nil slice — valid, len 0, appendable
s = append(s, 1)     // may or may not reallocate

a := []int{1,2,3,4,5}
b := a[1:3]          // b SHARES a's backing array
b[0] = 99            // a is now [1,99,3,4,5]
b = append(b, 100)   // OVERWRITES a[3] — no reallocation, cap allows it
```

This matters directly: if you slice a posting list and append to the slice,
you may corrupt the original. Use `copy` when you need independence, or
three-index slices `a[1:3:3]` to cap capacity and force reallocation on append.

Preallocate when you know the size — `make([]Posting, 0, docFreq)`.

**Maps**
- `m[k]` returns the zero value for missing keys, never an error
- `v, ok := m[k]` to distinguish missing from zero
- **Iteration order is deliberately randomized.** Never depend on it. When you
  write the term dictionary in Phase 3, you must extract keys and `sort` them.
- Sets are `map[string]struct{}` — `struct{}` occupies zero bytes

**Structs and methods**
- Value vs pointer receivers. Rule of thumb: pointer receivers if the method
  mutates, or if the struct is large. Be consistent within a type.
- Embedding for composition. Go has no inheritance and you won't miss it.

**Interfaces**

The most important Go concept for this project. Satisfaction is implicit —
no `implements` keyword:

```go
type Analyzer interface {
    Analyze(text string) []Token
}

type StandardAnalyzer struct{ filters []Filter }

func (a *StandardAnalyzer) Analyze(text string) []Token { ... }
// StandardAnalyzer now satisfies Analyzer. Nothing was declared.
```

**Keep interfaces small.** One to three methods. Go's convention is that the
*consumer* defines the interface, not the producer. Your `Index` interface
exists so `cmd/search` doesn't care whether it's talking to the in-memory or
mmap'd version.

**JSON**

```go
type Document struct {
    Title string   `json:"title"`
    Text  string   `json:"text"`
    Links []string `json:"outgoing_link"`
}

var d Document
if err := json.Unmarshal(line, &d); err != nil { ... }
```

Struct tags map field names. Unknown JSON fields are silently ignored, which is
what you want with the dump's dozens of fields. Field names must be **exported**
(capitalized) or `encoding/json` can't see them — a classic first-week bug.

For a large stream, `json.Decoder` over the reader avoids buffering everything;
but since your dump is line-delimited, `Scanner` + `Unmarshal` per line is
simpler and fine.

**Testing**

Table-driven is the Go idiom and it fits tokenizer tests perfectly:

```go
func TestTokenize(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  []string
    }{
        {"simple", "hello world", []string{"hello", "world"}},
        {"unicode", "café naïve", []string{"café", "naïve"}},
        {"punctuation", "don't stop", []string{"don't", "stop"}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := Tokenize(tt.input)
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

### Phase 2 — Ranking and evaluation

**`sort`** — `sort.Slice` with a closure is the easy path. `sort.Sort` with a
custom `Interface` when you need it.

**`container/heap`** — worth its own paragraph, because it's Go's most awkward
stdlib API and you need it for top-k. You implement five methods (`Len`, `Less`,
`Swap`, `Push`, `Pop`), and `Push`/`Pop` operate on `*T` not `T`, and you call
the package functions `heap.Push(h, x)` rather than `h.Push(x)`. It's confusing
the first time. Read the package example carefully; don't guess.

**Floats** — `math.Log`, `math.Inf`. Never compare with `==`. Watch for `NaN`
when a document frequency is zero.

**Generics** (Go 1.18+) — useful for your metrics code, but optional. Don't
reach for them early; Go code stays readable largely because people don't.

---

### Phase 3 — Binary formats and mmap

This phase is where Go gets closest to systems programming.

**`encoding/binary`**

```go
buf := make([]byte, binary.MaxVarintLen64)
n := binary.PutUvarint(buf, value)
w.Write(buf[:n])

value, n := binary.Uvarint(data)   // n = bytes consumed, or <= 0 on error
```

Uvarint is your posting compression primitive. Understand that it encodes 7 bits
per byte with a continuation flag — small numbers cost one byte, which is
exactly why delta encoding pays off.

**`[]byte` vs `string`** — conversion copies. In a hot decode loop that
allocation shows up in your profile. `unsafe` string/byte conversion exists but
don't reach for it until pprof says to.

**`bufio.Writer`** — always wrap file writes. Unbuffered `Write` per posting
will be catastrophically slow. And `defer w.Flush()` — forgetting to flush
produces a truncated index and a very confusing debugging session.

**mmap** — `golang.org/x/exp/mmap` gives you a safe `ReaderAt`. Direct
`syscall.Mmap` gives you a `[]byte` view of the file, which is faster to decode
from but requires care around unmapping. Start with the safe one.

**`defer`** — note it runs at *function* exit, not block exit. `defer` inside a
loop accumulates until the function returns, which leaks file handles. Wrap the
body in a closure or call explicitly.

---

### Phase 4 — Parsers and trees

**Recursion and interfaces together.** Your query AST is the textbook case:

```go
type Node interface{ Eval(idx Index) PostingList }

type TermNode struct{ Term string }
type AndNode struct{ Left, Right Node }
type PhraseNode struct{ Terms []string }
```

Each satisfies `Node`; `Eval` recurses. This is where Go's interfaces click.

**Type switches** for when you need the concrete type back:

```go
switch n := node.(type) {
case *TermNode:  // n is *TermNode here
case *AndNode:
}
```

---

### Phase 5 — Concurrency, benchmarks, profiling

**A note on goroutines.** Concurrency is Go's headline feature and every tutorial
front-loads it — but **this project barely needs it until Phase 5.** Indexing is
I/O-then-CPU in a single pass, and a correct sequential indexer beats a buggy
concurrent one. Resist the urge to parallelize early. When you do:

- Parallel analysis: a worker pool of goroutines analyzing documents, results
  merged by one goroutine that owns the index. **One owner** avoids locking.
- `sync.WaitGroup` to wait for workers
- Channels for the pipeline; buffered channels to smooth throughput
- `sync.Mutex` when shared state is genuinely unavoidable
- Background segment merging with `context.Context` for cancellation

**Run `go test -race` once you have any concurrency.** The race detector is
excellent and will find bugs you'd otherwise chase for days.

**Benchmarks**

```go
func BenchmarkSearch(b *testing.B) {
    idx := loadIndex(b)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Search(idx, "photosynthesis")
    }
}
```

```bash
go test -bench=. -benchmem
```

`-benchmem` shows allocations per operation, which is usually the real story.

**Profiling**

```bash
go test -bench=. -cpuprofile=cpu.out -memprofile=mem.out
go tool pprof -http=:8080 cpu.out
```

The flame graph in the browser is genuinely good. Learn to read it — it's the
difference between optimizing and guessing.

**Escape analysis** — `go build -gcflags='-m'` tells you what escapes to the
heap. Understanding why a slice escapes is how you cut allocations in the
posting decoder.

---

### Phase 6 — HTTP

`net/http` client, `json.Marshal` for bulk request bodies, `context` for
timeouts. Straightforward after everything above.

---

## Traps, ranked by how likely they are to bite *this* project

1. **Slice aliasing after `append`.** Posting lists are slices you'll slice and
   append constantly. Learn `copy` and three-index slices now.
2. **Bytes vs runes.** Your tokenizer will mangle Unicode if you index into
   strings. Wikipedia is full of Unicode.
3. **Map iteration order is random.** Your term dictionary must be sorted
   explicitly. If you rely on map order the index will be subtly wrong and
   the bug will look nondeterministic.
4. **Unexported fields don't unmarshal.** Lowercase `title` in your struct
   means `encoding/json` silently leaves it empty.
5. **Forgetting `bufio.Writer.Flush()`.** Truncated index file, no error.
6. **`Scanner`'s 64KB line limit.** Long Wikipedia articles exceed it, and
   `Scanner` just stops early — `sc.Err()` returns `bufio.ErrTooLong`, but if
   you don't check it you'll think the corpus is smaller than it is. **Always
   check `sc.Err()` after the loop.**
7. **`defer` in a loop.** File handles accumulate.
8. **The nil interface trap.** A nil `*T` stored in an interface is not `== nil`.
   Bites when returning concrete error types.
9. **Ignoring `err`.** Go makes it easy to `_` an error. Don't, especially
   around `binary.Uvarint`, where `n <= 0` means corrupt data.

---

## Style

- `gofmt` is not negotiable. There is one style.
- Short variable names in short scopes: `i`, `n`, `buf`, `sc`. Long names for
  package-level things. `idx` not `theSearchIndexInstance`.
- Accept interfaces, return structs.
- No getters/setters unless there's logic. Exported fields are fine.
- Package names are lowercase, single word, no underscores. The package name is
  part of every call site: `analysis.New()` not `analysis.NewAnalyzer()`.
- Handle errors where they occur; wrap with context; don't log-and-return.
- Comments on exported identifiers start with the identifier name.

---

## Resources

Recalling these from memory — verify before relying on them:

- **`go.dev/tour`** — start here, do all of it
- **`go.dev/doc/effective_go`** — the idiom guide, read it in week 0
- **Go by Example** (`gobyexample.com`) — quick syntax lookups
- **`pkg.go.dev`** — stdlib docs; the examples are often the best documentation
- ***The Go Programming Language*** by Donovan & Kernighan — the standard book;
  somewhat pre-generics but the fundamentals hold
- ***Learning Go*** by Jon Bodner — more recent, covers modules and generics
- **Go Wiki: CodeReviewComments** — the checklist Go reviewers actually use
- **`100go.co`** (Common Go Mistakes) — I believe this exists and is good;
  worth a look for the trap list

---

## Suggested rhythm

**Week 0** — Tour, Effective Go, word frequency counter. Don't touch the
search engine.

**Weeks 1–2 (Phase 1)** — You'll be looking things up constantly. Normal. The
project is deliberately easy here so the language can be the hard part.

**Week 3+ (Phase 2 onward)** — Syntax should be fading into the background.
If it isn't, pause and do more Tour exercises rather than pushing on.

**Phase 3** — The language and the problem are both hard here. If you're
stuck, isolate: write a tiny standalone program that varint-encodes and decodes
a slice of integers, prove it works, then integrate.

**Phase 5** — Come back and learn concurrency properly. By then you'll have a
real workload to parallelize and a benchmark to prove whether it helped, which
is a far better way to learn goroutines than a toy example.

---

## One test for readiness

Before starting Phase 1, you should be able to write this without looking
anything up:

```go
// Read a gzipped, line-delimited JSON file. For each even-numbered line,
// decode it into a struct, count the words in one field, and print the
// ten most common across the whole file.
```

If that feels achievable, go. If not, another two or three evenings on the
Tour will pay for themselves several times over.
