# Archive Report — project-detection

**Change**: project-detection
**Started**: 2026-06-07
**Archived**: 2026-06-07 (same-session delivery)
**Verdict**: PASS WITH WARNINGS — 0 CRITICAL / 2 WARNING / 1 SUGGESTION
**Archive mode**: hybrid (openspec + engram)

---

## What Shipped

### Files delivered

| File | Type | Lines (est.) |
|------|------|-------------|
| `internal/project/errors.go` | source | ~20 |
| `internal/project/noise.go` | source | ~25 |
| `internal/project/git.go` | source | ~110 |
| `internal/project/config.go` | source | ~80 |
| `internal/project/detect.go` | source | ~235 |
| `internal/project/doc.go` | source (modified) | ~5 |
| `internal/project/git_test.go` | test | ~200 |
| `internal/project/config_test.go` | test | ~150 |
| `internal/project/detect_test.go` | test | ~750 |
| `internal/project/helpers_test.go` | test | ~157 |

**Totals**: ~470 production lines, ~1257 test lines, ~2695 total commit lines (including SDD docs).

### Metrics

| Metric | Result | Threshold |
|--------|--------|-----------|
| Tests | 43 pass, 0 fail | all pass |
| Coverage | 88.3% | ≥ 80% |
| Spec scenarios covered | 32/32 | all |
| `go build ./...` | exit 0 | exit 0 |
| `go vet ./...` | exit 0 | exit 0 |
| `gofmt -l .` | no output | no output |

### New capability

- `project-detection` — specced at `openspec/specs/project-detection.md`

---

## Commits

| Hash | Message |
|------|---------|
| `c09dd90` | `feat(project): pure-function project detection with 5-case algorithm` |

T-32 (PR) deferred: remote not yet configured; follows scaffold-project and local-store-mvp precedent. PR description requirements noted in tasks.md T-32.

---

## Carry-Forward Items

### WARNING-01: Detect() fail-open on hard errors (design-sanctioned)
`detect.go:177-183` returns `(basename, nil)` for any error that is not `ErrAmbiguousProject`. Design §7 explicitly sanctions this. Non-blocking. Future iteration should add a distinct error path for hard failures (git binary missing).

### WARNING-02: gitRoot swallows non-128 git exit codes
`git.go:70-72` returns `("", false, nil)` for any non-zero git exit, including transient permission errors or corrupt repos. Consistent with engram parity. Non-blocking. Log/surface exit code in a future iteration.

### SUGGESTION-01: DIR-BASENAME-02 subtest discards Project field
`detect_test.go:59` uses `_ = result.Project` — weaker assertion than other subtests. Add `if result.Project == "" { t.Error(...) }` in a future pass.

---

## Process Discoveries Captured This Change

| Title | Engram ID | Summary |
|-------|-----------|---------|
| `discovery/apply-agent-spec-violation-detection` | 57 | Apply shipped R-ALGO-03 violation with misleading test name; orchestrator fixed inline before commit. Future apply runs must spot-check spec-impact areas. |
| `discovery/verify-before-claiming-drift` | 46 | Verify-before-claiming-drift lesson reinforced this round when validating against engram upstream. |

---

## Engram Artifact IDs

| Artifact | Topic Key | Engram ID |
|----------|-----------|-----------|
| Proposal | `sdd/project-detection/proposal` | 49 |
| Design (patched) | `sdd/project-detection/design` | 51 |
| Spec | `sdd/project-detection/spec` | (file-only — openspec) |
| Tasks | `sdd/project-detection/tasks` | (file-only — openspec) |
| Verify report | `sdd/project-detection/verify-report` | 58 |
| Archive report | `sdd/project-detection/archive-report` | (this document + engram save) |

---

## Source Folder Cleanup Note

The archive agent does not have access to a shell execution tool. The source folder `openspec/changes/project-detection/` was NOT removed by this agent. The orchestrator must run:

```bash
git rm -r openspec/changes/project-detection
git add openspec/archive/2026-06-07-project-detection/
git add openspec/specs/project-detection.md
git commit -m "chore(sdd): archive project-detection change"
```

This follows the same pattern as scaffold-project and local-store-mvp archives.

---

## Next Change Recommended

**`mcp-mvp`** — now that `local-store` + `project-detection` both ship, the MCP server can wire all 19 `mem_*` tools. Estimated ~2000-2500 LOC, likely 3-4 slices.

Dependencies satisfied:
- `local-store`: Active (`internal/store`) — all 19 tool backing operations available
- `project-detection`: Active (`internal/project`) — `mem_current_project` can resolve cwd

---

## Archive Integrity

- [x] Main spec created: `openspec/specs/project-detection.md` (Status: Active)
- [x] Archive report written: `openspec/archive/2026-06-07-project-detection/archive-report.md`
- [x] All artifact engram IDs recorded above
- [ ] Source folder removed: PENDING — orchestrator must run `git rm -r openspec/changes/project-detection`
- [ ] Git commit for archive: PENDING — orchestrator must commit
