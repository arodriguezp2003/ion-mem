# Specification: project-scaffold

**Capability**: project-scaffold
**Status**: Active
**Go version**: 1.25
**Module path**: `github.com/ionix/ion-mem` (placeholder; update via `go mod edit -module` before first tagged release)

---

## 1. Capability: project-scaffold

### 1.1 Summary

`project-scaffold` creates the minimum set of files that make the `ion-mem` repository compile, pass `go test`, and gate every future PR through CI. It establishes the module identity, package skeleton, build tooling, CI workflow, license, and project documentation. No business logic is included. Every subsequent SDD change (`store-mvp`, `mcp-mvp`, `cloud-mvp`) assumes these files exist.

---

### 1.2 Requirements

#### Module declaration

| ID | Verb | Requirement |
|----|------|-------------|
| R-MOD-01 | MUST | `go.mod` declares module `github.com/ionix/ion-mem` |
| R-MOD-02 | MUST | `go.mod` sets `go 1.25` |
| R-MOD-03 | MUST | `go.mod` has an empty `require` block (no third-party deps) |

#### Package skeleton

| ID | Verb | Requirement |
|----|------|-------------|
| R-PKG-01 | MUST | `cmd/ion-mem/main.go` exists with `package main` and an empty `main()` function |
| R-PKG-02 | MUST | Each of the 9 `doc.go` files listed in §1.4 exists at its exact path |
| R-PKG-03 | MUST | Every `doc.go` begins with a `// Package <name>` comment stating the package's intent |
| R-PKG-04 | MUST NOT | Any `doc.go` contains implementation code (vars, funcs, types) |
| R-PKG-05 | MUST | `internal/cloud/dashboard/doc.go` exists as a nested placeholder under `internal/cloud/` |

#### Build tooling

| ID | Verb | Requirement |
|----|------|-------------|
| R-MAKE-01 | MUST | `Makefile` provides a `build` target that runs `go build ./...` |
| R-MAKE-02 | MUST | `Makefile` provides a `test` target that runs `go test ./...` |
| R-MAKE-03 | MUST | `Makefile` provides a `lint` target that runs `go vet ./...` |
| R-MAKE-04 | MUST | `Makefile` provides a `fmt` target that runs `gofmt -l .` and exits non-zero if output is non-empty |
| R-MAKE-05 | SHOULD | `Makefile` provides a `help` or default target listing available targets |

#### CI workflow

| ID | Verb | Requirement |
|----|------|-------------|
| R-CI-01 | MUST | `.github/workflows/ci.yml` triggers on `push` to any branch and on `pull_request` |
| R-CI-02 | MUST | CI runs `go build ./...` and fails the workflow if exit code is non-zero |
| R-CI-03 | MUST | CI runs `go test ./...` and fails the workflow if exit code is non-zero |
| R-CI-04 | MUST | CI runs `gofmt -l .` and fails the workflow if output is non-empty |
| R-CI-05 | MUST | CI runs `go vet ./...` and fails the workflow if exit code is non-zero |
| R-CI-06 | MUST | All four checks are required to pass before a PR can be merged (branch protection gate) |
| R-CI-07 | MUST | CI uses the same Go version declared in `go.mod` (Go 1.25) |

#### License

| ID | Verb | Requirement |
|----|------|-------------|
| R-LIC-01 | MUST | `LICENSE` file exists at the repo root |
| R-LIC-02 | MUST | `LICENSE` contains MIT license text |
| R-LIC-03 | MUST | MIT copyright line names Ionix as the copyright holder |
| R-LIC-04 | MUST | Copyright year in `LICENSE` is the year this scaffold is committed |

#### README

| ID | Verb | Requirement |
|----|------|-------------|
| R-README-01 | MUST | `README.md` exists at the repo root |
| R-README-02 | MUST | `README.md` includes a section describing what ion-mem is |
| R-README-03 | MUST | `README.md` states the fork relationship to upstream engram |
| R-README-04 | MUST | `README.md` states current status as "scaffold / work in progress" |
| R-README-05 | MUST | `README.md` includes build and test commands (`go build ./...`, `go test ./...`) |

#### .gitignore

| ID | Verb | Requirement |
|----|------|-------------|
| R-GIT-01 | MUST | `.gitignore` ignores Go build output (`/bin/`, `dist/`, binary named `ion-mem`) |
| R-GIT-02 | MUST | `.gitignore` ignores SQLite files (`*.db`, `*.db-journal`) |
| R-GIT-03 | MUST | `.gitignore` ignores `.engram/` directory |
| R-GIT-04 | SHOULD | `.gitignore` ignores local env files (`.env`, `.env.local`) |

---

### 1.3 Scenarios

#### Build scenarios

**Scenario B-01: clean build**

- GIVEN a fresh clone of the repo with no prior build artifacts
- WHEN `go build ./...` is run at the repo root
- THEN the command exits 0 with no output

**Scenario B-02: make build produces binary**

- GIVEN the repo is cloned and `make` is available
- WHEN `make build` is run
- THEN `go build ./...` runs, exits 0, and the `ion-mem` binary is produced under the expected output path (e.g., `./bin/ion-mem` or repo root per Makefile convention)

**Scenario B-03: missing go.mod causes toolchain failure**

- GIVEN the repo root has no `go.mod`
- WHEN `go build ./...` is run
- THEN the command exits non-zero with an error referencing the missing module file

#### Test scenarios

**Scenario T-01: empty suite passes**

- GIVEN no `_test.go` files exist in any package
- WHEN `go test ./...` is run
- THEN the command exits 0 (proves toolchain is operational, no panics)

**Scenario T-02: make test delegates correctly**

- GIVEN the Makefile exists with a `test` target
- WHEN `make test` is run
- THEN `go test ./...` is invoked and exits 0

#### Lint and format scenarios

**Scenario L-01: no gofmt drift on scaffold**

- GIVEN all Go source files are committed as written by the scaffold
- WHEN `gofmt -l .` is run
- THEN the command produces no output (all files are already formatted)

**Scenario L-02: gofmt detects drift**

- GIVEN a Go source file has been edited and contains formatting drift
- WHEN `gofmt -l .` is run
- THEN the command outputs the name of the drifted file

**Scenario L-03: go vet passes on empty packages**

- GIVEN the package skeleton contains only `doc.go` and `main.go`
- WHEN `go vet ./...` is run
- THEN the command exits 0 with no diagnostics

#### CI scenarios

**Scenario CI-01: push triggers CI**

- GIVEN the CI workflow file exists at `.github/workflows/ci.yml`
- WHEN a commit is pushed to any branch
- THEN GitHub Actions triggers the CI workflow

**Scenario CI-02: PR triggers CI**

- GIVEN the CI workflow is configured with `pull_request` trigger
- WHEN a pull request is opened or updated
- THEN GitHub Actions triggers the CI workflow

**Scenario CI-03: CI passes when all four checks are green**

- GIVEN `go build ./...`, `go test ./...`, `gofmt -l .`, and `go vet ./...` all exit 0 / produce no output
- WHEN the CI workflow runs
- THEN all steps report success and the workflow exits green

**Scenario CI-04: CI fails if build breaks**

- GIVEN a syntax error is introduced in any `.go` file
- WHEN the CI workflow runs
- THEN the `go build` step exits non-zero and the workflow is marked failed

**Scenario CI-05: CI fails if gofmt finds drift**

- GIVEN a `.go` file has formatting drift
- WHEN the CI workflow runs
- THEN the `gofmt -l .` step outputs the filename and the CI step fails

#### Package structure scenarios

**Scenario P-01: doc.go parses as valid Go**

- GIVEN each `doc.go` file listed in §1.4
- WHEN `go build ./...` is run
- THEN all packages compile without error (validates that `doc.go` files contain valid Go syntax)

**Scenario P-02: doc.go states intent**

- GIVEN `internal/store/doc.go` is opened
- WHEN the file is read
- THEN the first line is a `// Package store` comment describing the package's purpose (SQLite + FTS5 storage)

**Scenario P-03: cloud/dashboard placeholder is nested correctly**

- GIVEN `internal/cloud/dashboard/doc.go` exists
- WHEN `go build ./...` is run
- THEN the `dashboard` package compiles as a sub-package of `cloud` without error

#### License scenarios

**Scenario LIC-01: LICENSE contains MIT text**

- GIVEN the `LICENSE` file exists at the repo root
- WHEN the file is read
- THEN it contains the standard MIT license text with "Ionix" as copyright holder and the scaffold commit year

**Scenario LIC-02: missing LICENSE is detectable**

- GIVEN CI includes an "expected files" check step
- WHEN `LICENSE` is absent from the repo
- THEN the CI check step fails
- NOTE: if no explicit file-presence CI check is added, this scenario is enforced by code review policy; the spec records both options and leaves the implementation decision to `sdd-tasks`

#### README scenarios

**Scenario README-01: required sections present**

- GIVEN `README.md` exists at the repo root
- WHEN the file is parsed
- THEN it contains sections covering: what ion-mem is, the fork relationship to upstream engram, current status, and build/test commands

**Scenario README-02: build commands are accurate**

- GIVEN the README shows `go build ./...` and `go test ./...`
- WHEN those commands are run on the repo
- THEN both exit 0

---

### 1.4 Acceptance Criteria

- [x] `go build ./...` exits 0
- [x] `go test ./...` exits 0
- [x] `gofmt -l .` produces no output
- [x] `go vet ./...` exits 0
- [x] CI workflow triggers on push and PR; all four checks are required gates
- [x] Module path in `go.mod` is `github.com/ionix/ion-mem`
- [x] Go version in `go.mod` is `1.25`
- [x] `go.mod` has an empty `require` block

**Required files — exact list (17 total):**

| File | Description |
|------|-------------|
| `go.mod` | Module declaration |
| `LICENSE` | MIT license, Ionix copyright |
| `README.md` | Project overview and commands |
| `.gitignore` | Go + SQLite + engram data patterns |
| `Makefile` | `build`, `test`, `lint`, `fmt` targets |
| `.github/workflows/ci.yml` | CI: build + test + fmt + vet |
| `cmd/ion-mem/main.go` | Empty `main()`, `package main` |
| `internal/store/doc.go` | SQLite + FTS5 storage layer intent |
| `internal/mcp/doc.go` | MCP server protocol layer intent |
| `internal/server/doc.go` | HTTP/API server layer intent |
| `internal/cloud/doc.go` | Cloud feature layer intent |
| `internal/tui/doc.go` | Terminal UI layer intent |
| `internal/setup/doc.go` | Setup and initialization layer intent |
| `internal/sync/doc.go` | Cloud sync layer intent |
| `internal/project/doc.go` | Project management layer intent |
| `internal/cloud/dashboard/doc.go` | Dashboard sub-package placeholder |

Total: 6 root/tooling files + 1 CI workflow + 1 `main.go` + 9 `doc.go` files = **17 files**.

---

### 1.5 Out of Scope

| Item | Deferred to |
|------|------------|
| Third-party Go dependencies | `store-mvp`, `mcp-mvp`, `cloud-mvp` |
| Business logic in `internal/*` | Future capability changes |
| Cloud schema, RBAC, invites, sync | `cloud-mvp` |
| TUI implementation | Future TUI change |
| Plugin system | Future change |
| Goreleaser config | Separate release change |
| Final repo host resolution | Pre-release `go mod edit` |
