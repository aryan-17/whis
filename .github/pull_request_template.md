## Summary

<!-- What changed and why. Be specific — one sentence per bullet. -->

-

## Sprint / Task

<!-- Which sprint task does this close? e.g. "Sprint 3, Task 6 — Posting list ops" -->

Sprint ___, Task ___ —

## How to test

<!-- Steps to verify this works. -->

1.
2.

## Checklist

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `gofmt -l .` outputs nothing (all files formatted)
- [ ] `go test ./...` passes
- [ ] Posting lists sorted by DocID (if index code changed)
- [ ] Analyzer used identically at index-time and query-time (if analysis code changed)
- [ ] Eval NDCG unchanged vs in-memory (if persistence code changed — Sprint 5+)
- [ ] SPRINTS.md checkboxes updated
