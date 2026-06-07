# Verification Report: mcp-mvp

**Date**: 2026-06-06  
**Verifier**: sdd-verify (independent — not the apply agent)  
**Commits verified**: 3cdea8d, 6f10d34, 6b35314 (all 3 slices)

---

## Summary

**Verdict: PASS WITH WARNINGS**  
CRITICAL: 0 | WARNING: 2 | SUGGESTION: 1

All 69 tests pass independently. Build, vet, and gofmt are all clean. Coverage is 78.6% (handlers driving mcp package), meeting the ≥75% spec target. All 14 tools are registered with the `ion_` prefix. Zero `mem_*` names exist anywhere. All 8 locked design decisions are respected. All 10 CC checks pass. Critical side-effects verified by both source inspection and runtime test evidence.

---

## Verification Commands (independently executed)

| Command | Result |
|---|---|
| `go build ./...` | EXIT:0 — clean |
| `go test ./internal/mcp/... -count=1` | EXIT:0 — 69 tests, 0 failed |
| `go test -coverpkg=./internal/mcp/... ./internal/mcp/...` | 78.6% (handlers → mcp); 81.9% total via `go tool cover -func` |
| `gofmt -l .` | EXIT:0 — no files flagged |
| `go vet ./...` | EXIT:0 — clean |
| `rg -c "json\.Marshal" internal/mcp/` | Only `envelope.go` (2) and `tool_current_project.go` (3) — matches documented exception |

---

## Cross-Cutting Requirements (CC.1–CC.10)

| Check | Spec Req | Result | Status |
|---|---|---|---|
| context.Context first param on public funcs | R-CC-01 | All handlers receive ctx | PASS |
| Godoc on all exported types/funcs | R-CC-02 | Verified across server.go and all tool files | PASS |
| imports only store + project from internal/ | R-CC-03 | No other internal imports found via rg | PASS |
| os.Getenv only in configuredDefaultProject | R-CC-04 | Single callsite in project.go — confirmed | PASS |
| All tool names ion_ prefix, no mem_* | R-CC-05 | 14 ion_ tools; "mem_" appears only in doc.go comment | PASS |
| envelope.Build sole JSON entry point | R-CC-06 | json.Marshal in envelope.go + tool_current_project.go only (documented exception) | PASS |
| Real *store.Store in tests, t.TempDir() | R-CC-07 | mustStore() uses t.TempDir(); no mocks | PASS |
| Test helpers in helpers_test files | R-CC-08 | mcp/helpers_test.go + handlers/helpers_test.go present | PASS |
| No testify | R-CC-09 | rg testify: 0 matches | PASS |
| Table-driven tests | R-CC-10 | TestServer_AgentAndAllProfileExactlyFourteenTools uses t.Run subtests | PASS |

---

## Locked Design Decisions (from design §4 + §6)

| # | Decision | Evidence |
|---|---|---|
| 1 | mcp-go v0.44.0 exact pin | go.mod: `github.com/mark3labs/mcp-go v0.44.0` |
| 2 | envelope.Build sole entry point | json.Marshal only in envelope.go + tool_current_project.go (exception documented in spec §4 + design §6) |
| 3 | ion_current_project returns direct JSON, not standard envelope | Source inspection + TestCurrentProject_returns_detection_result_directly PASS |
| 4 | ion_session_summary calls store.EndSession when session_id supplied | tool_session.go lines 157-159 + TestSessionSummary_with_session_id_also_calls_store_EndSession PASS |
| 5 | Real store in tests | t.TempDir() in mustStore() |
| 6 | No testify | rg: no matches |
| 7 | No mem_* names | rg "mem_" in mcp/: only comment in doc.go |
| 8 | 14 tools, all ion_ prefix | agentTools map: 14 entries; TestServer_AgentAndAllProfileExactlyFourteenTools PASS |
| 9 | os.Getenv isolated | project.go:configuredDefaultProject is sole callsite |

---

## Tool Registration

`agentTools` in `internal/mcp/server.go` contains exactly **14** entries:

```
ion_current_project    ion_save              ion_search
ion_context            ion_get_observation   ion_session_start
ion_session_end        ion_session_summary   ion_save_prompt
ion_suggest_topic_key  ion_update            ion_delete
ion_timeline           ion_stats
```

`TestServer_AgentAndAllProfileExactlyFourteenTools` covers both `profile=agent` and `profile=all`. Both subtests PASS.

---

## Spec Requirement Compliance

### Slice 1 (18 requirements, 17 scenarios)

All R-S1-* requirements satisfied. All S1-T-* scenarios have passing covering tests.

Key verifications:
- R-S1-SVR-01: Server struct has `store`, `detect`, `defaultProj`, `profile`, `sessionMu`, `sessionsByProj`, `promptsBySession` — CONFIRMED
- R-S1-PROJ-01: Precedence order (per-call arg → defaultProj → cwd arg → DetectFull) — covered by TestResolveProject_* battery (8 tests)
- R-TOOL-CURRENT-02/03: Returns DetectionResult directly; NEVER returns Go error — source + TestCurrentProject_ambiguous_cwd_returns_error_in_body_not_go_error PASS
- R-S1-ENV-01/02/03: Envelope shape — TestBuild_* battery (4 tests) PASS

### Slice 2 (16 requirements, 16 scenarios)

All R-S2-* requirements satisfied. All S2-T-* scenarios covered.

Critical: R-S2-SSUM-01 (ion_session_summary also calls store.EndSession when session_id provided) — VERIFIED by both source (`if sessionIDArg != "" { _ = s.store.EndSession(...) }`) and TestSessionSummary_with_session_id_also_calls_store_EndSession which checks `sess.Status == "ended"` directly against the store.

### Slice 3 (11 requirements, 8 scenarios)

All R-S3-* requirements satisfied. All S3-T-* scenarios covered.

- S3-T-STATS-01: TestIonStats_ReflectsCurrentState seeds 2 sessions / 5 obs / 1 prompt and asserts exact counts — PASS
- S3-T-INT-01: TestIonFullLifecycle_E2E exercises full lifecycle in server_test.go — PASS
- R-S3-PROFILE-01: Both agent and all profiles return exactly 14 tools — PASS

---

## Handler Test Spot-Check (5 randomly selected)

All tested handlers (a) call the tool via callTool(), (b) assert envelope shape or direct response body, and (c) assert behavioral side-effects or specific field values.

| Test | Calls tool | Asserts shape | Asserts side-effect or value |
|---|---|---|---|
| TestSessionSummary_with_session_id_also_calls_store_EndSession | YES | YES | `sess.Status == "ended"` via st.GetSession — real store query |
| TestCurrentProject_ambiguous_cwd_returns_error_in_body_not_go_error | YES | YES | `error == "ambiguous_project"`, `available_projects` present, no nil result |
| TestIonStats_ReflectsCurrentState | YES | YES | Exact counts: total_sessions:2, total_obs:5, total_prompts:1 |
| TestIonUpdate_PatchPreservesUnchangedFields | YES | YES | title changed, content + type unchanged, revision_count ≥ 1 |
| TestIonDelete_SoftDeleteHidesFromSearch | YES | YES | count:0 in subsequent ion_search after soft delete |

---

## Critical Path Verification

### ion_session_summary → store.EndSession (CRITICAL side-effect)

Source (`internal/mcp/tool_session.go` lines 157-159):
```go
if sessionIDArg != "" {
    _ = s.store.EndSession(ctx, sessionID, summary)
}
```

Runtime: `TestSessionSummary_with_session_id_also_calls_store_EndSession` asserts `sess.Status == "ended"` by querying the store after calling the tool. **VERIFIED: YES.**

### ion_current_project never returns Go error (R-TOOL-CURRENT-03)

Handler always returns `(textResult(raw), nil)` in all branches — happy path, ambiguous, and other errors. **VERIFIED: YES.** Two tests confirm this explicitly.

---

## Per-Slice Acceptance Criteria

| Slice | Build | Tests | Coverage | Profile tools | vet | gofmt |
|---|---|---|---|---|---|---|
| 1 | PASS | 27 pass | 71.5% mcp / 84.6% w/handlers (≥70%) | 3 | PASS | PASS |
| 2 | PASS | 64 pass | 78.9% (≥72%) | 10 | PASS | PASS |
| 3 | PASS | 69 pass | 78.6% (≥75%) | 14 | PASS | PASS |

---

## Issues

### WARNINGS (non-blocking)

**W-01 — Noisy test stderr (benign)**  
Multiple tests emit `ERROR: Error reading from stdout: io: read/write on closed pipe` to stderr. This is a teardown race in the mcp-go stdio server when the in-process test server is stopped. All tests still PASS. No spec requirement is violated, but the noise could be mistaken for failures in CI logs.  
*Recommendation: wrap the in-process server shutdown to drain the pipe before closing, or suppress the log in test mode.*

**W-02 — Coverage metric ambiguity**  
Three different coverage numbers exist: 60.3% (mcp white-box only), 78.6% (handlers driving mcp — canonical), 81.9% (`go tool cover -func` total). The spec requires ≥75% and the canonical cross-package measurement satisfies it, but the multi-number situation creates CI configuration risk.  
*Recommendation: document the canonical command (`go test -coverpkg=./internal/mcp/... ./internal/mcp/...`) in a Makefile target to avoid confusion.*

### SUGGESTION

**S-01 — TestIonUpdate_MissingIdEnvelopeError assertion strength**  
The test asserts `result != ""` and `result != "observation updated"` — both correct, but weak negative assertions. A positive assertion like `strings.Contains(result, "not found")` would be more robust against future messages that accidentally satisfy the negations.

---

## Final Verdict

**PASS WITH WARNINGS**

0 CRITICAL issues. 2 WARNINGs (both non-blocking: test noise and coverage metric confusion). 1 SUGGESTION. All spec requirements traced to passing tests. All locked decisions confirmed by source + runtime evidence. Ready for `sdd-archive`.
