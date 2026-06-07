# Tasks: local-store-mvp

**Change**: `local-store-mvp`
**Package**: `internal/store`
**Strict TDD**: ACTIVE — red → green → refactor on every behavior task
**Delivery**: 3 stacked-to-main PRs (Slice 1 → 2 → 3)

---

## Part 1: Review Workload Forecast

| Field | Value |
|---|---|
| Total estimated lines | ~1525 (475 + 625 + 425) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Chain strategy | stacked-to-main |
| Delivery strategy | auto-chain |
| Decision needed before apply | No (cached: 3 PRs stacked-to-main) |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

---

## Part 2: Suggested Work Units

| Unit | PR Title | Slice | Scope | Approx. Lines |
|------|----------|-------|-------|---------------|
| 1 | `feat(store): slice 1 — schema + sessions` | Slice 1 | store.go, migrations.go, errors.go, helpers.go, schema_0001_sessions.go, sessions.go + 3 test files | ~475 |
| 2 | `feat(store): slice 2 — observations + FTS5 + search` | Slice 2 | schema_0002_observations.go, observations.go, observations_test.go, search_test.go | ~625 |
| 3 | `feat(store): slice 3 — prompts + timeline + stats` | Slice 3 | schema_0003_prompts.go, prompts.go, timeline.go, prompts_test.go, timeline_test.go | ~425 |

---

## Part 3: Slice 1 — Schema + Sessions

PR: `feat(store): slice 1 — schema + sessions` → base: `main`

- [x] Task 1.1 through 1.28 all complete (see detailed list in source openspec/changes/local-store-mvp/tasks.md)

---

## Part 4: Slice 2 — Observations + FTS5 + Search

PR: `feat(store): slice 2 — observations + FTS5 + search` → base: `main` (rebase after PR 1 merges)

- [x] Task 2.1 through 2.30 all complete (see detailed list in source openspec/changes/local-store-mvp/tasks.md)

---

## Part 5: Slice 3 — Prompts + Timeline + Stats

PR: `feat(store): slice 3 — prompts + timeline + stats` → base: `main` (rebase after PR 2 merges)

- [x] Task 3.1 through 3.26 all complete (see detailed list in source openspec/changes/local-store-mvp/tasks.md)

---

## Part 6: Cross-cutting Verification (post-Slice 3)

- [x] CC.1 Confirm all 56 requirements have a corresponding test or implementation evidence link
- [x] CC.2 Confirm all 53 scenarios have a named test function
- [x] CC.3 Run `go test -cover ./internal/store/...` — inspect coverage report (informational, not a gate for MVP)
- [x] CC.4 Final clean pass: `go vet ./...`, `gofmt -l .`, `go build ./...`, `go test ./internal/store/...` all exit 0

---

## Summary

All 86+ tasks across 3 slices and cross-cutting verification are marked [x] complete.

Full task details are archived in the original openspec/changes/local-store-mvp/tasks.md file.
