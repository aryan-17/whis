# wikisearch — Claude & AI Coding Standards

> Loaded every session. All rules apply to every sprint, every file, every edit.

---

## Project Context

Search engine in Go over Simple English Wikipedia (~250k articles).  
Corpus: `simplewiki_content-20260830-00000.json.bz2`  
Plan: `docs/superpowers/plans/2026-09-03-search-engine.md`  
Sprint tracker: `wikisearch/SPRINTS.md`

---

## Non-Negotiable Rules

### Build
- **Always use:** `go build ./...` to verify no compile errors after changes
- **Run tests:** `go test ./...` before any commit
- **Vet:** `go vet ./...` — fix all warnings, never suppress
- **Format:** `gofmt -w <file>` on every `.go` file you touch

### Code Quality
- **Never use `interface{}`** — use typed interfaces or `any` (Go 1.18+) only when truly needed
- **No panic in library code** — only in `main()` or test helpers
- **All errors handled** — never `_ = err` in non-test code
- **Error wrapping:** `fmt.Errorf("context: %w", err)` — always add context, always wrap with `%w`
- **No magic numbers** — extract to named constants

### Comments
- Concise, explain **why**, not what — 1–2 lines max
- Every exported type, function, method must have a doc comment
- No task-specific comments (e.g., "// TODO: fix this later" without a sprint reference)

### Secrets
- Never hardcode paths, credentials, or file names — use flags or constants
- Never pass secrets as CLI arguments

---

## SOLID Principles (Go Translation)

### S — Single Responsibility
- One package = one concern. `analysis/` does text analysis only. `rank/` does scoring only.
- If a function needs a comment to explain its two jobs → split it.
- Keep files under ~200 lines. If larger, split by responsibility.

### O — Open/Closed
- Extend via interfaces, not by modifying existing types.
- `Index` interface: never change it — add new implementations instead.
- `Scorer` interface: add new scorers (TF-IDF, BM25, PageRank blend) without touching existing ones.
- `Filter` interface: add new filters to the pipeline without touching existing filters.

### L — Liskov Substitution
- `MemoryIndex` and `SegmentIndex` must be interchangeable via `Index` interface.
- `cmd/search` must not import concrete types — only the `Index` interface.
- If a substitute requires a special code path in the caller → interface design is wrong.

### I — Interface Segregation
- Small interfaces. `Index` has 5 methods, not 20.
- If callers only need `Lookup`, accept `interface{ Lookup(string) (PostingList, bool) }`, not `Index`.
- Don't bundle unrelated methods on one interface.

### D — Dependency Inversion
- `cmd/search` depends on `Index` (interface), not `MemoryIndex` (concrete).
- `rank` package depends on `Index` interface for scoring, never imports `index` package directly.
- Inject dependencies via constructor parameters, not `init()` or globals.

---

## Go-Specific Patterns

### Error Handling
```go
// GOOD — wrap with context
func (r *Reader) Next() (Document, bool, error) {
    if err := json.Unmarshal(data, &raw); err != nil {
        return Document{}, false, fmt.Errorf("line %d: unmarshal: %w", r.line, err)
    }
}

// BAD — swallow or ignore
if err != nil { return }
_ = err
```

### Interface Design
```go
// GOOD — define interface at point of use (in the consuming package)
// rank/bm25.go
type indexStats interface {
    NumDocs() uint32
    AvgDocLen() float64
    DocLen(id uint32) uint32
}

// BAD — import concrete type across packages
import "wikisearch/internal/index"
func NewBM25(idx *index.MemoryIndex) *BM25Scorer { ... }
```

### Constructor Pattern
```go
// GOOD
func NewAnalyzer() *Analyzer { return &Analyzer{} }
func NewBM25(idx indexStats, k1, b float64) *BM25Scorer { ... }

// BAD — package-level init, globals, or init()
var globalAnalyzer = &Analyzer{}
```

### Table-Driven Tests
```go
// ALWAYS use table-driven tests for functions with multiple cases
func TestTokenize(t *testing.T) {
    cases := []struct {
        input string
        want  []string
    }{
        {"Hello, world!", []string{"Hello", "world"}},
        {"", nil},
    }
    for _, c := range cases {
        t.Run(c.input, func(t *testing.T) {
            got := Tokenize(c.input)
            // assert
        })
    }
}
```

### Naming Conventions
| Thing | Convention | Example |
|-------|-----------|---------|
| Packages | lowercase, one word | `analysis`, `postings` |
| Exported types | PascalCase | `PostingList`, `MemoryIndex` |
| Unexported | camelCase | `rawDoc`, `nextID` |
| Interfaces | noun or adjective | `Index`, `Scorer`, `Analyzer` |
| Constructor | `New<Type>` | `NewReader`, `NewBM25` |
| Test helpers | `t.Helper()` + descriptive | `buildTestIndex(t)` |
| Constants | PascalCase (exported) / ALL_CAPS (avoid) | `DefaultK1 = 1.2` |

---

## Architecture Invariants

These must never be violated. Any change that breaks these is a bug.

1. **Analyzer symmetry** — `Analyzer` used identically at index-time and query-time. Same instance or same config. Never diverge.
2. **Posting list sort order** — `PostingList.Entries` always sorted by `DocID` ascending. Assert in tests.
3. **Position gap preservation** — Dropping a stopword leaves a positional gap. Never renumber positions after filter.
4. **Index interface stability** — `Index` interface never changes after Sprint 3. New capabilities = new interface or new implementation.
5. **Even-line rule** — Dump slices always use even line counts. Odd = split pair = corrupt data.
6. **Eval score stability** — After Sprint 5 (persistence), NDCG must be bit-for-bit identical to in-memory. Any diff = codec bug.

---

## Package Boundaries

```
corpus/    → no imports from this project (leaf package)
analysis/  → imports: corpus (for Document type only)
postings/  → imports: index (for types only)
index/     → imports: corpus, analysis, postings
rank/      → imports: index (interface only), postings
query/     → imports: analysis (for Analyzer)
link/      → imports: corpus, index (interface only)
eval/      → imports: nothing from this project
cmd/*      → imports everything, wires it together
```

**Forbidden:**
- `analysis` importing `index`
- `rank` importing concrete `index.MemoryIndex` or `index.SegmentIndex`
- `postings` importing `rank`
- Any package importing `cmd/*`

---

## Testing Standards

- **TDD:** Write failing test first, then implementation.
- **Table-driven:** Always for functions with 2+ cases.
- **Test helpers:** Always call `t.Helper()` in helper functions.
- **No mocking internal packages** — use real implementations. Only mock external I/O (files, HTTP).
- **Benchmark first** before any performance optimization. Record baseline in `bench_baseline.txt`.
- **One assert per test is fine** — multiple are fine too. Be explicit about what you're checking.
- **Test file naming:** `<file>_test.go` in same package for white-box, `<package>_test` for black-box.

---

## Commit Standards

- `feat: <what>` — new capability
- `fix: <what>` — bug fix  
- `perf: <what>` — performance improvement
- `test: <what>` — tests only
- `refactor: <what>` — no behavior change

Subject line: ≤50 chars, imperative ("add" not "added"), no period.

**Per sprint:** At minimum one commit per task. Never batch multiple tasks into one commit.

---

## What to Check Before Every Commit

```bash
cd wikisearch
go build ./...          # must pass
go vet ./...            # must pass  
go test ./...           # must pass
gofmt -l .              # must print nothing (no unformatted files)
```

---

## Session Continuity

When starting a new session on this project:
1. Read `SPRINTS.md` — check current sprint and unchecked tasks
2. Read this file
3. Read the plan: `docs/superpowers/plans/2026-09-03-search-engine.md`
4. Run `go build ./...` to confirm current state
5. Continue from where SPRINTS.md left off
