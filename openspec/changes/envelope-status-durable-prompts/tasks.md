---
ion-sdd-version: "1.0"
phase: ion-sdd-tasks
generated: "2026-07-04T00:00:00Z"
mode: openspec
change: "envelope-status-durable-prompts"
strict_tdd: true
delivery_strategy: "ask-on-risk"
chain_strategy: "pending"
---

# Tasks: envelope-status-durable-prompts

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | ~530 total (Slice 1 ~240, Slice 2 ~290) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → Slice 1 (envelope) / PR 2 → Slice 2 (durable prompts) |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|---|---|---|---|
| 1 | Envelope structured errors (Slice 1) | PR 1 → main | Self-contained; no Slice 2 deps. All ~17 call-site migrations included. |
| 2 | Durable prompt capture (Slice 2) | PR 2 → PR 1 | Depends on PR 1 landing; removes in-memory buffer entirely. |

---

## Phase 1: Slice 1 — Envelope Foundation (TDD)

- [x] 1.1 RED — Add `TestBuild_StatusOk` and `TestBuildError_*` table test in `internal/mcp/envelope_test.go`; assert `status:"ok"` on `Build` and `status:"error"` + `error_code` per closed-vocabulary value on `BuildError`; run `go test ./internal/mcp/...` — expect compile failure.
- [x] 1.2 GREEN — In `internal/mcp/envelope.go`: add five `const` error-code strings; add `status:"ok"` to `Build` map; implement `BuildError(det, code, msg string) []byte` setting `status:"error"`, `error_code`, `result`; add unexported `errorCode(err error) string` mapping store sentinels via `errors.Is`. Tests must pass.
- [x] 1.3 RED — In `internal/mcp/handlers/current_project_test.go`: add `TestCurrentProject_AmbiguousErrorValue` asserting `error:"project_ambiguous"` (not `"ambiguous_project"`); expect failure.
- [x] 1.4 GREEN — In `internal/mcp/tool_current_project.go`: rename literal `"ambiguous_project"` → `"project_ambiguous"`. Test must pass.
- [x] 1.5 RED — Add per-path handler tests across `handlers/save_test.go`, `handlers/save_prompt_test.go`, `handlers/search_test.go`, `handlers/context_test.go`, `handlers/session_test.go`, `handlers/get_observation_test.go`, `handlers/timeline_test.go`, `handlers/update_test.go`, `handlers/delete_test.go`, `handlers/stats_test.go`: assert `status:"ok"` on success and `status:"error"` + correct `error_code` on each error path. Expect failure.
- [x] 1.6 GREEN — Migrate all ~17 `Build(det, "error ...")` call sites across the tool files above to `BuildError(det, errorCode(err), ...)` (or the appropriate closed-vocabulary literal). All Slice 1 tests must pass.
- [x] 1.7 VERIFY — `go test ./internal/mcp/...`, `go vet ./internal/mcp/...`, `gofmt -l ./internal/mcp/` all clean.

---

## Phase 2: Slice 2 — Store Foundation (TDD)

- [x] 2.1 RED — In `internal/store/prompts_test.go`: add `TestConsumeLatestPrompt_Found`, `TestConsumeLatestPrompt_NotFound`, `TestConsumeLatestPrompt_AlreadyConsumed`, `TestConsumeLatestPrompt_LatestSelected`, `TestMigration0004_Applies`, `TestMigration0004_ExistingRowsNullConsumedAt`. Expect compile/schema failure.
- [x] 2.2 GREEN — Create `internal/store/schema_0004_prompt_consumed.go` with `ALTER TABLE user_prompts ADD COLUMN consumed_at TEXT DEFAULT NULL` and `registerMigration(4, ...)`. Update `Prompt` struct in `internal/store/prompts.go` to add `ConsumedAt sql.NullString`; update all `Scan` calls that read prompt rows. Tests must pass.
- [x] 2.3 GREEN — Implement `ConsumeLatestPrompt(ctx, sessionID string) (Prompt, bool, error)` in `internal/store/prompts.go`: single tx `SELECT ... WHERE consumed_at IS NULL ORDER BY created_at DESC LIMIT 1`, then `UPDATE consumed_at = datetime('now')`. All store tests must pass.
- [x] 2.4 VERIFY — `go test ./internal/store/...` clean.

---

## Phase 3: Slice 2 — Integration & Buffer Removal (TDD)

- [ ] 3.1 RED — Rewrite `internal/mcp/prompt_test.go`: replace buffer tests with `TestConsumeLatestPrompt_*` integration tests using `ConsumeLatestPrompt` via a real store (restart-durability, double-consume false, dedup+consumed interaction). Remove all references to `recordPrompt`/`lastPromptForSession`/`RecordPromptForTest`. Expect compile failure.
- [ ] 3.2 GREEN — Remove from `internal/mcp/server.go`: `promptsBySession`, `promptMu` fields and `make(map[string]string)` init, `RecordPromptForTest` method. Remove from `internal/mcp/session.go`: `recordPrompt` and `lastPromptForSession` methods. Tests must compile.
- [ ] 3.3 GREEN — In `internal/mcp/tool_save_prompt.go`: remove `s.recordPrompt(sessionID, content)` call. In `internal/mcp/tool_save.go`: replace `s.lastPromptForSession(sessionID)` block with `_, promptAttached, _ = s.store.ConsumeLatestPrompt(ctx, sessionID)`. All Slice 2 prompt tests must pass.
- [ ] 3.4 RED — Update `internal/mcp/handlers/save_test.go`: replace `ionSrv.RecordPromptForTest(...)` with `ion_save_prompt` tool call seeding the DB; assert `prompt_attached: true`. Update `internal/mcp/handlers/save_prompt_test.go` similarly. Expect failure.
- [ ] 3.5 GREEN — Adjust handler test helpers to wire `ion_save_prompt` as the seeding path. All handler tests must pass.
- [ ] 3.6 VERIFY — `go test ./...`, `go vet ./...`, `gofmt -l ./...` all clean. No references to `RecordPromptForTest` or `promptsBySession` remain.
