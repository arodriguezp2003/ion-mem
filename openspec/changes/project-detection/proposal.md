# Proposal: Project Detection (5-Case Algorithm)

## Intent

Every ion-mem memory operation must be scoped to a project. Today nothing in the codebase knows how to resolve "which project am I in?" from a working directory. Upstream engram solved this with a deterministic 5-case algorithm (config → git_remote → git_root → git_child → dir_basename). We mirror that algorithm in a pure-function `internal/project` package so the upcoming MCP server, CLI, and HTTP layers all share one source of truth, with no surprises for users migrating from engram.

## Why Now

The next planned change is `mcp-mvp`. Every MCP tool call (`mem_save`, `mem_search`, `mem_current_project`, …) needs a resolved project name before it can hit `internal/store`. Without detection, MCP cannot ship. Detection is also a hard prerequisite for cloud sync (scoping uploads to the right project) and for the CLI/TUI. It is the smallest unblocking dependency and has zero coupling to `store`, so it is safe to land first and in isolation.

## Scope

### In Scope

- `internal/project/detect.go` — public `Detect(cwd) (string, error)` and `DetectFull(cwd) (DetectionResult, error)`.
- `DetectionResult` struct: `Project`, `Source`, `Path`, `Warning`, `Error`, `AvailableProjects`.
- 5 detection cases, in priority order:
  1. `config` — nearest `.ion-mem/config.json` inside the enclosing repo, walking up from cwd.
  2. `git_remote` — cwd is a git root with a `remote.origin.url`; parse last path segment, strip `.git`.
  3. `git_root` — cwd inside a git repo (any depth); use repo-root basename.
  4. `git_child` — cwd is the parent of EXACTLY ONE child git repo (after noise filtering); auto-promote with `Warning`.
  5. `dir_basename` — fallback, always succeeds.
- Sentinel `ErrAmbiguousProject` (returned only when `git_child` finds 2+ candidates).
- Noise set for `git_child` scan: `node_modules`, `vendor`, `.venv`, `venv`, `target`, `dist`, `build`, `.idea`, `.vscode`, `.git`, `bin`, `out`, `cache`, `tmp`.
- Git access via shelling out to `git` (`rev-parse --show-toplevel`, `remote get-url origin`).
- Black-box tests in `package project_test` mirroring `local-store-mvp` patterns: table-driven, `t.TempDir`, fixture builders in `*_helpers_test.go`.
- Coverage target ≥ 80%.

### Out of Scope

- Name similarity / consolidation (`similar.go` in upstream) — deferred to a `project-consolidate` change.
- Project rename / migration tooling.
- `.ion-mem/config.json` schema versioning beyond a single `project` field.
- Caching detection results (recompute per call; revisit if MCP perf demands it).
- File-watching `.ion-mem/config.json` changes.
- Wiring detection into MCP / CLI consumers (handled by `mcp-mvp` and later changes).

## Capabilities

### New Capabilities

- `project-detection`: deterministic resolution of a project name from a working directory, exposing both a convenience function (`Detect`) and a full diagnostic result (`DetectFull`) so callers can render source, path, warnings, and ambiguity candidates.

### Modified Capabilities

- None.

## Approach

- Mirror engram's algorithm exactly (5 cases, same priority, same noise set, same warning semantics) so users moving between engram and ion-mem get identical results.
- Pure-function package: no state, no globals, no `store` dependency. Single entry point per use case.
- Shell out to `git` (less code, same surface as upstream, no `.git/config` parser to maintain).
- Strict TDD (now active): every behavior lands red → green → refactor. Tests-first per case and per edge case.
- File layout splits algorithm (`detect.go`), git helpers (`git.go`), config reader (`config.go`), noise filter (`noise.go`), errors (`errors.go`).

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/project/` | New | Source files: `detect.go`, `git.go`, `config.go`, `noise.go`, `errors.go` + tests. |
| `internal/project/doc.go` | Modified | Replace scaffold placeholder with real package doc. |
| `go.mod` | None expected | No new dependencies (stdlib + `os/exec`). |

## Open Questions (Surface Before Spec)

1. **Config dir name** — `.ion-mem/config.json` (clear identity) vs `.engram/config.json` (engram cross-compat). **Recommendation**: `.ion-mem/`; users migrating run a rename one-liner.
2. **Git access** — `os/exec` shelling to `git` (engram parity, less code) vs parsing `.git/config` directly (no `git` binary required). **Recommendation**: shell out.
3. **Public surface** — export `DetectionResult` + `DetectFull` in v1, or keep internal until MCP needs them. **Recommendation**: export both now; MCP `mem_current_project` will need `DetectFull` immediately and the contract is cheap to commit.
4. **Naming** — `Detect`/`DetectFull` vs `Resolve`/`Identify`/`Find`. **Recommendation**: `Detect`/`DetectFull` matches engram's mental model.
5. **Warning channel** — `DetectionResult.Warning string` vs typed sentinel. **Recommendation**: string for v1 (only one warning case); upgrade to sentinels when more arrive.
6. **Config vs git_root conflict** — if `.ion-mem/config.json` says `foo` but the git repo basename is `bar`, who wins? **Recommendation**: config wins (engram does the same — explicit overrides inference).

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Behavior diverges silently from engram on edge cases (URL shapes, symlinks) | Medium | Table-driven tests per URL shape; mirror engram test fixtures; document any intentional divergence in spec. |
| `git` binary missing on user machine | Low | `git_remote` / `git_root` / `git_child` gracefully fall through to `dir_basename`; surface clear error only when shelling out errors mid-flight. |
| Auto-promote in `git_child` surprises users (silent project switch) | Medium | `Warning` field is populated and callers (MCP, CLI) must render it; document the rule in spec acceptance. |
| Hallucinating "drift" instead of verifying against engram source | Low | Process lesson logged in `discovery/verify-before-claiming-drift`; spec and tests cite engram file paths for traceability. |

## Rollback Plan

`internal/project` is a new isolated package with no callers yet. Rollback = delete `internal/project/*.go` (except `doc.go`), revert the change branch. No data migration, no schema change, no consumer impact.

## Dependencies

- Archived `scaffold-project` (Go module, package layout, CI wiring).
- Archived `local-store-mvp` (test patterns and Strict TDD activation reused here).
- No dependency on `internal/store`; safe to land before `mcp-mvp`.

## Success Criteria

- [ ] All 5 detection cases implemented and covered by at least one happy-path test each.
- [ ] All edge cases enumerated in the spec covered by tests (malformed config, git URL shapes, deeply nested cwd, ambiguous git_child, all-noise filtering, nonexistent cwd).
- [ ] `go test ./internal/project/...` passes with coverage ≥ 80%.
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...` clean.
- [ ] Public API: `Detect`, `DetectFull`, `DetectionResult`, `ErrAmbiguousProject` exported from `internal/project`.
- [ ] All 6 open questions resolved (in spec or design) before `sdd-apply` starts.
- [ ] Behavior matches engram upstream on shared test fixtures; any divergence is documented in the spec.
