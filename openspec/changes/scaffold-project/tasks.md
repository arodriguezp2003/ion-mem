# Tasks: project-scaffold

**Change**: scaffold-project
**Delivery strategy**: ask-on-risk
**Strict TDD**: disabled (no behavioral tests; scaffold has empty test suite)

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~310–390 (17 new files, mostly boilerplate) |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: pending
400-line budget risk: Low

### Rationale

All 17 files are brand-new boilerplate with no existing code to modify. Go skeletons (`doc.go`, `main.go`) are 3–8 lines each; `go.mod` is 5 lines; the Makefile ~30 lines; `ci.yml` ~50 lines; `LICENSE` ~22 lines; `README.md` ~40 lines; `.gitignore` ~20 lines. Total estimated additions land around 310–390 lines — under the 400-line budget. A single PR is appropriate.

---

## Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Foundation + skeleton + tooling + CI | PR 1 | All 17 files; repo stays buildable at every commit boundary |

---

## Phase 1: Foundation (root files, no Go code)

- [x] 1.1 Create `go.mod` — declare module `github.com/ionix/ion-mem`, set `go 1.25`, empty `require` block. (R-MOD-01, R-MOD-02, R-MOD-03)
- [x] 1.2 Create `LICENSE` — MIT license text, copyright `Ionix`, year `2026`. (R-LIC-01, R-LIC-02, R-LIC-03, R-LIC-04)
- [x] 1.3 Create `.gitignore` — patterns: `/bin/`, `dist/`, `ion-mem` (binary), `*.db`, `*.db-journal`, `.engram/`, `.env`, `.env.local`. (R-GIT-01, R-GIT-02, R-GIT-03, R-GIT-04)
- [x] 1.4 Create `README.md` — sections: what ion-mem is, fork relationship to upstream engram, status ("scaffold / work in progress"), build and test commands (`go build ./...`, `go test ./...`). (R-README-01 through R-README-05)

## Phase 2: Empty Go Skeleton (must compile)

- [x] 2.1 Create `cmd/ion-mem/main.go` — `package main`, empty `func main() {}`. (R-PKG-01)
- [x] 2.2 Create `internal/store/doc.go` — `// Package store` comment stating SQLite + FTS5 storage layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)
- [x] 2.3 Create `internal/mcp/doc.go` — `// Package mcp` comment stating MCP server protocol layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)
- [x] 2.4 Create `internal/server/doc.go` — `// Package server` comment stating HTTP/API server layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)
- [x] 2.5 Create `internal/cloud/doc.go` — `// Package cloud` comment stating cloud feature layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)
- [x] 2.6 Create `internal/cloud/dashboard/doc.go` — `// Package dashboard` comment stating dashboard sub-package placeholder intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04, R-PKG-05)
- [x] 2.7 Create `internal/tui/doc.go` — `// Package tui` comment stating terminal UI layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)
- [x] 2.8 Create `internal/setup/doc.go` — `// Package setup` comment stating setup and initialization layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)
- [x] 2.9 Create `internal/sync/doc.go` — `// Package sync` comment stating cloud sync layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)
- [x] 2.10 Create `internal/project/doc.go` — `// Package project` comment stating project management layer intent; no implementation code. (R-PKG-02, R-PKG-03, R-PKG-04)

## Phase 3: Build Tooling

- [x] 3.1 Create `Makefile` with the following targets (R-MAKE-01 through R-MAKE-05):
  - `build`: `go build ./...`
  - `test`: `go test ./...`
  - `lint`: `go vet ./...`
  - `fmt`: `@out="$$(gofmt -l .)" && test -z "$$out" || (echo "$$out" && exit 1)`
  - `help` (default target): print available targets with descriptions

## Phase 4: CI Workflow

- [x] 4.1 Create `.github/workflows/ci.yml` with (R-CI-01 through R-CI-07):
  - Triggers: `push` (all branches) and `pull_request`
  - Go setup step: `go-version: '1.25'`
  - Step: `go build ./...` (fails CI on non-zero exit)
  - Step: `go test ./...` (fails CI on non-zero exit)
  - Step: `go vet ./...` (fails CI on non-zero exit)
  - Step: gofmt check — `test -z "$(gofmt -l .)"` (fails CI when output is non-empty)
  - Step: LICENSE presence check — `test -f LICENSE` (LIC-02 explicit CI guard, fail-fast)

## Phase 5: Verification

- [x] 5.1 Run `go build ./...` at repo root — must exit 0 with no output. (Scenario B-01, R-MOD-01)
- [x] 5.2 Run `go test ./...` at repo root — must exit 0. (Scenario T-01)
- [x] 5.3 Run `gofmt -l .` — must produce no output. (Scenario L-01)
- [x] 5.4 Run `go vet ./...` — must exit 0 with no diagnostics. (Scenario L-03)

---

## Verification Strategy

Maps spec §1.4 acceptance criteria to verification method and task order:

| Acceptance Criterion | Verification Method | Verified After |
|---|---|---|
| `go build ./...` exits 0 | Manual: task 5.1; CI: step in task 4.1 | Phase 2 complete (task 2.10) |
| `go test ./...` exits 0 | Manual: task 5.2; CI: step in task 4.1 | Phase 2 complete |
| `gofmt -l .` produces no output | Manual: task 5.3; CI: gofmt step in task 4.1 | Phase 3 complete (Makefile fmt target) |
| `go vet ./...` exits 0 | Manual: task 5.4; CI: step in task 4.1 | Phase 2 complete |
| CI triggers on push and PR; all four checks are gates | Review ci.yml triggers + branch protection config | Phase 4 complete (task 4.1) |
| Module path is `github.com/ionix/ion-mem` | `grep module go.mod` | Phase 1 complete (task 1.1) |
| Go version in `go.mod` is `1.25` | `grep ^go go.mod` | Phase 1 complete (task 1.1) |
| `go.mod` has empty `require` block | `cat go.mod` — no `require` entries | Phase 1 complete (task 1.1) |
| `LICENSE` present | CI: `test -f LICENSE` step (task 4.1); manual inspection | Phase 1 complete (task 1.2) |
