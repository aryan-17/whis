# AI Practices Gap Analysis: n8n → wikisearch

Audited n8n project at `/Users/ctme/Documents/projects/n8n`.  
Compared against wikisearch setup at `/Users/ctme/Documents/projects/whis`.

---

## What n8n Has → What We Added

| Practice | n8n | wikisearch (added) |
|----------|-----|--------------------|
| Coding standards for AI sessions | `AGENTS.md` (342 lines) | `wikisearch/CLAUDE.md` ✅ |
| Sprint/task tracker | `agent:setup` scripts | `wikisearch/SPRINTS.md` ✅ |
| PostToolUse auto-format hook | Biome hook on write | `.claude/settings.json` (gofmt) ✅ |
| Session continuity instructions | AGENTS.md "fresh setup" section | CLAUDE.md "Session Continuity" ✅ |
| Implementation plan | `.claude/plans/` | `docs/superpowers/plans/` ✅ |

---

## What n8n Has → Still Missing Here

### 1. Git Pre-Commit Hook (HIGH PRIORITY)
n8n uses **lefthook** to run `pnpm lint` + `pnpm typecheck` before every commit.  
wikisearch has nothing blocking bad commits.

**Needed:** `.git/hooks/pre-commit` that runs:
```bash
go build ./...
go vet ./...
gofmt -l . | grep . && exit 1 || true
go test ./... -count=1 -short
```

### 2. Granular Permissions in settings.json (MEDIUM)
n8n `.claude/settings.json` has explicit allow rules for every command:
```json
"Bash(git log:*)", "Bash(pnpm lint:*)", "Bash(pnpm test:*)"
```
wikisearch `.claude/settings.json` only has the gofmt hook — no permission rules defined.

**Needed:** Add explicit allow rules for safe commands:
```json
"Bash(go build:*)", "Bash(go test:*)", "Bash(go vet:*)",
"Bash(gofmt:*)", "Bash(git log:*)", "Bash(git show:*)"
```

### 3. PR Template (MEDIUM)
n8n has `.github/pull_request_template.md` with checklist:
- Responsibility statement
- Tests included
- Docs updated

wikisearch has no PR template.

**Needed:** `.github/pull_request_template.md`

### 4. PR Title Conventions (LOW)
n8n has `.github/pull_request_title_conventions.md` — Angular format with changelog impact table.

wikisearch CLAUDE.md has commit conventions but no PR title doc.

**Needed:** Either add to CLAUDE.md or create `.github/pull_request_title_conventions.md`

### 5. CONTRIBUTING.md (LOW — solo project for now)
n8n has 342-line CONTRIBUTING.md with:
- What requires an issue first
- PR size limits (1000 lines max)
- Auto-reject patterns (whitespace PRs, mass renames)
- AI disclosure requirements

**Needed:** Not urgent for a solo learning project. Add when open-sourcing.

### 6. Skill Usage Tracking Hook (LOW)
n8n tracks which skills are used via `PostToolUse` on `Skill` events → `track-skill-usage.mjs`.

**Needed:** Not needed for a solo project. Add if extending to team use.

### 7. Linting Config (golangci-lint)
n8n uses Biome (formatter) + ESLint (rules) + lefthook (git hooks).  
wikisearch has `gofmt` (formatter) but no linter config.

**Needed:** `.golangci.yml` with rules:
```yaml
linters:
  enable:
    - errcheck      # all errors must be handled
    - govet         # go vet rules
    - staticcheck   # advanced static analysis
    - unused        # unused code
    - gofmt         # formatting
    - goimports     # import grouping
    - revive        # idiomatic Go
    - godot         # comment punctuation
```

### 8. Security Fix Hygiene Doc (LOW)
n8n has explicit rules: don't expose vulnerability type in branch names, commit messages, test names, or PR titles.

**Needed:** Probably overkill for a personal learning project. Add if open-sourcing.

---

## n8n Practices Adapted to Go (Already in CLAUDE.md)

| n8n Practice | Go Equivalent (in CLAUDE.md) |
|--------------|------------------------------|
| No `any` TypeScript type | No `interface{}`, use typed interfaces |
| No `as` casts in TypeScript | No type assertions without guard |
| Wrap errors with context | `fmt.Errorf("context: %w", err)` |
| Interface at point of use | Define interface in consuming package |
| Small focused interfaces | `Index` has 5 methods, not 20 |
| Constructor functions | `New<Type>(deps) *Type` |
| Table-driven tests | Always for multi-case functions |
| Commit per task | Per sprint task, not batched |
| Comment the "why" | 1-2 lines, not "what" |
| Reuse existing helpers | Check package before writing new |

---

## n8n Practices Not Applicable Here

| n8n Practice | Why Not Applicable |
|--------------|-------------------|
| pnpm workspaces | Single Go module, no monorepo |
| TypeORM boundary lint rule | No ORM (raw binary files) |
| PostHog feature flags | No user-facing product |
| Customer confidentiality rules | No customers |
| v3 branch model | No versioned releases |
| Playwright E2E tests | CLI tool, no browser |
| Vue/i18n rules | Not a web app |
| Linear issue tracking | Solo project |
| Biome formatter | Go uses gofmt |

---

## Priority Order: What to Do Next

1. **[x] Git pre-commit hook** — `.git/hooks/pre-commit` (build + vet + fmt + test -short)
2. **[x] `.golangci.yml`** — errcheck, govet, staticcheck, revive, godot, misspell
3. **[x] Granular permissions in `.claude/settings.json`** — go/git/gofmt/golangci-lint allow rules
4. **[x] `.github/pull_request_template.md`** — sprint task ref + invariant checklist
