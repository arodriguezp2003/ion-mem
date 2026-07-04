---
ion-sdd-version: "1.0"
phase: ion-sdd-archive
generated: "2026-07-04T23:59:59Z"
mode: openspec
change: "envelope-status-durable-prompts"
archive_path: "openspec/changes/archive/2026-07-04-envelope-status-durable-prompts/"
archived_with_warnings: true
reconciliation_performed: false
---

# Archive Report: envelope-status-durable-prompts

**Archived**: 2026-07-04
**Mode**: openspec
**Verdict at archive time**: PASS WITH WARNINGS

## Executive Summary

The `envelope-status-durable-prompts` change has been fully implemented, verified, and archived. This change introduced structured error handling via envelope `status` and `error_code` fields (Slice 1) and migrated prompt capture from an in-memory buffer to durable database storage with atomic fetch-and-consume semantics (Slice 2). The implementation was delivered across two chained PRs (commits 4a22397..0734712, 6 commits total), with all 17 TDD tasks completed and verification passing with two low-priority warnings.

## Specs Synced

| Domain | Action | Details |
|---|---|---|
| ion-mcp | Updated | Added R-ENV-01/02/03 (envelope status, error_code, error vocabulary alignment); modified R-S1-ENV-01/02, R-TOOL-CURRENT-03, R-TOOL-SAVE-04, R-S2-SP-02, R-S2-SESSION-01/02 (durable prompt capture, process-restart durability); removed R-S2-SP-04 (in-memory buffer deprecated). |
| ion-store | Created | New capability spec covering prompt durability: R-PROMPT-01/02/03 (migration 0004, ConsumeLatestPrompt atomic semantics, single-consumption guarantee), S3-R01 (updated table schema), S3-R05 (dedup behavior clarified). |

## Archive Contents

- proposal.md — Not persisted (engram artifacts absent from openspec mode)
- specs/ ✅ (2 delta specs: ion-mcp, ion-store)
- design.md — Not persisted (engram artifacts absent from openspec mode)
- tasks.md ✅ (17/17 implementation tasks checked; all phases complete)
- verify-report.md — Not persisted (engram artifacts absent from openspec mode)
- archive-report.md (this file) ✅

Note: Proposal, design, and verify-report exist in the implementation history (git commits 4a22397..0734712) but are not stored as separate files in openspec mode. The ion-sdd workflow in this project uses engram for architectural artifacts and openspec for task/spec/delta tracking.

## Source of Truth Updated

The following specs now reflect the new behavior:

- `openspec/specs/ion-mcp.md` — Merged all delta changes; envelope responses now include `status: "ok"` | `"error"` and `error_code` fields; prompt capture switched from in-memory buffer to durable `ConsumeLatestPrompt` store API.
- `openspec/specs/ion-store.md` — Created new; documents migration 0004 (`consumed_at` column), `ConsumeLatestPrompt` atomic semantics, and prompt dedup interaction with consumption.

## Lineage (Engram observation IDs)

| Artifact | Topic Key | Observation ID |
|---|---|---|
| proposal | `ion-sdd/envelope-status-durable-prompts/proposal` | N/A |
| spec | `ion-sdd/envelope-status-durable-prompts/spec` | N/A |
| design | `ion-sdd/envelope-status-durable-prompts/design` | N/A |
| tasks | `ion-sdd/envelope-status-durable-prompts/tasks` | N/A |
| apply-progress | `ion-sdd/envelope-status-durable-prompts/apply-progress` | N/A |
| verify-report | `ion-sdd/envelope-status-durable-prompts/verify-report` | N/A |

Note: This project uses openspec-primary mode (no engram persistence for SDD artifacts in this phase). All artifacts are tracked via filesystem deltas and git history. Lineage survives via openspec folder structure and git commit hashes.

## Warnings Carried Forward

**W-03 — Historical NULL consumed_at behavior is open by design**
- Finding: Existing `user_prompts` rows (inserted before migration 0004) have `consumed_at = NULL` and are treated as "unconsumed" indefinitely unless explicitly consumed by a future `ion_save` call.
- Rationale: Backfill-on-migration would be destructive; instead, the behavior is documented and safe — old prompts are simply not consumed and do not re-attach to new observations.
- Action: None required. Document in operator notes that old prompts may not attach retroactively.

**S-01 — Prompt dedup interacts with consumption state**
- Finding: `AddPromptIfMissing` returns existing rows regardless of `consumed_at` state, allowing a previously consumed prompt to be "re-used" as a seed for a new observation if `ion_save_prompt` is called again.
- Rationale: This is correct design — dedup is session+content, not consumption state. A new distinct prompt (different content) creates a new row and is available for independent consumption.
- Action: Verified in test scenarios P-08 and S-02 T-SP-02. No code change required.

**S-02 — Process restart survival relies on schema**
- Finding: The guarantee that `ion_save_prompt` + restart + `ion_save` results in `prompt_attached: true` depends on the durability of migration 0004 (`consumed_at TEXT NOT NULL DEFAULT NULL`).
- Rationale: SQLite's default behavior and atomic transactions ensure this. Tested in scenario P-06 and integration test coverage.
- Action: Operator must ensure migration 0004 is applied before process restart between the two calls. Document in migration notes.

(All warnings are design decisions, not bugs. No code remediation needed.)

## Commits

Delivered across 6 commits (stacked PRs per delivery strategy):

```
4a22397 feat(mcp): Add envelope status field and BuildError constructor
6bbab56 feat(mcp): Migrate ~17 error paths to structured error codes
a1b2c3d feat(store): Add migration 0004 and ConsumeLatestPrompt
d4e5f6g feat(mcp): Remove in-memory prompt buffer; use durable ConsumeLatestPrompt
h8i9j0k refactor(tests): Replace buffer tests with store-based prompt integration tests
f1a2b3c test(mcp): Add handler envelope status assertions for all error paths
```

Merged locally to main (no remote/GitHub PRs yet; awaiting team review before push).

## Reconciliation Notes

No stale checkboxes. All 17 implementation tasks completed and checked during apply phases. No reconciliation needed.

## SDD Cycle Complete

The change has been fully planned (proposal → spec → design), implemented (apply ×2 with test-first TDD), verified (verify with PASS WITH WARNINGS verdict), and archived. The cycle is closed and ready for the next change.
