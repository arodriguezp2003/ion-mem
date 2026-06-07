# Archive Report — mcp-mvp

**Change**: mcp-mvp
**Started**: 2026-06-07
**Archived**: 2026-06-07 (same-session delivery)
**Verdict**: PASS WITH WARNINGS (0 CRITICAL, 2 WARNING, 1 SUGGESTION)

## What shipped

**Capability**: `ion-mcp` (now Active in `openspec/specs/ion-mcp.md`)

**Tools** (14 registered under "agent" profile, all with `ion_` prefix):
- ion_current_project, ion_save, ion_search (slice 1)
- ion_context, ion_get_observation, ion_session_start, ion_session_end, ion_session_summary, ion_save_prompt, ion_suggest_topic_key (slice 2)
- ion_update, ion_delete, ion_timeline, ion_stats (slice 3)

**Code**: 21 source files + 13 test files in `internal/mcp/` and `internal/mcp/handlers/`
- `internal/mcp/server.go`, `envelope.go`, `project.go`, `session.go`, `doc.go`, `errors.go` (if any) + 14 `tool_*.go`
- `internal/mcp/handlers/` — stubs + black-box test files (avoids circular import; tests drive mcp from outside)
- Total: ~5,013 changed lines across 3 commits

**Tests**: 69 functions, 0 failures, 78.6% cross-package coverage (target ≥75% met)

**Commits**:
- `3cdea8d` feat(mcp): slice 1 — stdio server scaffold + 3 tools
- `6f10d34` feat(mcp): slice 2 — daily-driver tools (context, get, session_*, save_prompt, suggest)
- `6b35314` feat(mcp): slice 3 — utility tools + agentTools reconciliation + e2e

**New deps**: `github.com/mark3labs/mcp-go v0.44.0` (direct require, NOT indirect — go mod tidy ran AFTER imports per `discovery/go-mod-tidy-import-order`)

## Critical contracts verified

- ✅ `ion_session_summary` calls `store.EndSession` when `session_id` is provided (`tool_session.go:157-159`, asserted by `TestSessionSummary_with_session_id_also_calls_store_EndSession`)
- ✅ `ion_current_project` NEVER returns Go error from MCP boundary; ambiguity surfaces structurally (R-TOOL-CURRENT-02)
- ✅ `envelope.Build` is SOLE entry for envelope-wrapped JSON; `json.Marshal` appears ONLY in `envelope.go` + `tool_current_project.go` (documented exception per design §6)
- ✅ All 14 tools start with `ion_` prefix (zero `mem_*` names)
- ✅ Single `os.Getenv` call (in `project.go` for `ION_MEM_PROJECT`)
- ✅ Empty store cases return empty arrays/strings, NOT errors (R-TOOL-SEARCH-02, ion_context, etc.)
- ✅ Idempotent ion_session_start on duplicate id (returns existing, not error)

## Carry-forward items

- **W-01 (test noise)**: tests emit `ERROR: Error reading from stdout: io: read/write on closed pipe` on teardown. Benign (all tests pass) but noisy. Future cleanup: explicit context cancellation in test helpers before pipe close.
- **W-02 (coverage metric ambiguity)**: three coverage numbers exist (60.3% mcp-only / 78.6% cross-package / 81.9% func-total). Canonical is 78.6% but a Makefile target would prevent CI misconfiguration.
- **SUGGESTION**: add `envelope.Raw(body any) []byte` helper so `tool_current_project.go` doesn't hand-roll json.Marshal. Cosmetic — current behavior is correct.
- **`Serve()` not directly tested**: requires real stdio (out of unit-test scope). Tested indirectly via in-process mcptest transport. Acceptable for v1.
- **Coverage uplift opportunities**: `Serve()` orchestration path, error branches in tool_save (FK rejection, dedup hash collision).
- **Deviations from design** (all justified):
  1. Handler implementations live in flat `internal/mcp/tool_*.go` instead of `internal/mcp/handlers/*.go` (avoids circular imports; `handlers/` keeps stubs + tests).
  2. `revision_count` NOT auto-incremented by `store.UpdateObservation` on patch — only `topicKeyUpsert` does. Spec said "preserves unchanged fields", silent on increment.
  3. E2E test placed in `server_test.go` instead of `handlers/lifecycle_test.go`. Functionally equivalent.

## Process notes captured

- **`discovery/apply-agent-spec-violation-detection`** (id 57) — reinforced this round: spec tasks risk note about `ion_session_summary` side-effect was explicitly called out in apply prompt; agent honored it correctly.
- **`discovery/go-mod-tidy-import-order`** (id 35) — slice 1 prompt explicitly told apply to write the `mark3labs/mcp-go` import BEFORE running `go mod tidy`. Result: dep is direct, NOT indirect. Lesson applied successfully.
- **`discovery/sdd-archive-agent-copy-vs-move`** (id 39) — bypassed entirely this round by orchestrator handling archive mechanics inline (no sub-agent). Cleanest archive of the 3 done so far.

## Next change recommended

`claude-code-plugin` — wraps ion-mem as a Claude Code plugin so devs can install it and have it run as their memory backend. Needs:
- `plugin/claude-code/.claude-plugin/plugin.json` — manifest
- `plugin/claude-code/.mcp.json` — registers `ion-mem mcp` as MCP server (requires `cli-mvp` first OR a separate command entry point in `cmd/ion-mem/main.go`)
- `plugin/claude-code/hooks/hooks.json` — SessionStart, UserPromptSubmit, SubagentStop, PostCompaction, SessionStop hooks
- `plugin/claude-code/scripts/*.sh` — hook scripts (session creation, compaction recovery, prompt capture wiring)
- `plugin/claude-code/skills/memory/SKILL.md` — Memory Protocol that tells the agent WHEN to call `ion_save`, `ion_search`, etc.

Likely 500-800 LOC. Single PR, low risk. But depends on `cli-mvp` (a thin wrapper around `mcp.Server` exposed as `ion-mem mcp` subcommand). Could split:
- `cli-mvp` first (~150 LOC): wire `ion-mem mcp [--profile=agent]` in `cmd/ion-mem/main.go`
- `claude-code-plugin` after (~500 LOC): plugin manifest + hooks + scripts + skill

Or alternatively, start `cloud-data-model` (the Ionix team-grade differentiator: users + projects + members + invites + audit_log schema) in parallel — independent of MCP path.
