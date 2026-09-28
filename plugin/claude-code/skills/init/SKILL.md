---
name: ion-mem-init
description: "Trigger: \"ion-mem init\", \"init memory\", \"seed memory\", \"inicializar memoria\", \"cargar memoria del proyecto\". Seed ion-mem from an existing codebase. NOT for generic /init, project scaffolding, or sdd-init."
---

# ion-mem Init — Seed Memory From an Existing Project

Bootstrap ion-mem for a **brownfield** project: analyse the repo exhaustively, propose a
reviewed set of observations, and seed them only after the user confirms.

Use this when memory is empty (or thin) but the codebase already carries years of
architecture, decisions and conventions that agents keep re-deriving.

**Do NOT run this skill for**: creating a CLAUDE.md (`/init`), bootstrapping SDD
(`/sdd-init`, `/ion-sdd-init`), or scaffolding a new project.

## Non-Negotiable Safety Contract

| Rule | Enforcement |
|------|-------------|
| Seeded keys are namespaced | Every key this skill writes is `seed/<family>/<slug>`. A human key can never be upserted by accident. |
| Never overwrite | Never `ion_save` with a `topic_key` that already exists. `ion_update` is forbidden except to restore a revision this skill overwrote (Phase 4 guard). |
| Explicit project everywhere | `project: "<resolved>"` on every `ion_context`, `ion_search`, `ion_save`, `ion_session_summary`. Never rely on server-side cwd detection. |
| Deletion only via REBUILD | `ion_delete` is allowed **only** in Phase 0.5, soft (`hard: false`), only on rows carrying the seed marker unless a second confirmation covers the rest. Never `hard: true`, never in any other phase. |
| Dry run first | Phase 3 prints every candidate and STOPS. No `ion_save` before explicit confirmation. |
| Hard cap | Max 40 observations per run. Over the cap → keep the highest-value 40, report the rest. |
| Backup first | `ion-mem backup` runs in 0.4, its path is printed and verified before any write or delete. |
| Automated artifact | Every `ion_save` in Phase 4 passes `capture_prompt: false`. |
| Read-only analysis | Subagents are `subagent_type: "Explore"`. Never grant them Write/Edit or any `ion_*` write tool. |

## Model Assignments

Pass `model` and `subagent_type: "Explore"` explicitly on every `Agent` call.

| Phase | Agent work | Model |
|-------|-----------|-------|
| 1 | Reconnaissance (stack, docs, git history, CI) | `sonnet` |
| 2 | Deep analysis (architecture, decisions, conventions, debt) | `opus` |
| 0, 3, 4 | Preflight, review table, seeding | main thread (no subagent) |

## Budget

| Limit | Value |
|-------|-------|
| Phase 1 subagents | 3–4, launched in one message (parallel) |
| Phase 2 subagents | 4, one per dimension, launched in one message |
| Files read per subagent | ≤ 40 |
| Report length per subagent | ≤ 400 words |
| Candidates produced | ≤ 12 per Phase 2 subagent |
| Observations saved per run | ≤ 40 |
| `git log` window | last 6 months, or full history if the repo has < 200 commits |

---

## Phase 0 — Preflight

Order matters: the tree is chosen **before** the project is resolved, because project
detection is cwd-derived.

### 0.1 Detect the real working tree

Repos often keep code on a branch or worktree other than `main`.

```bash
git rev-parse --show-toplevel
git worktree list
git branch -a
git rev-list --count HEAD
```

Then check whether the current tree looks like a real codebase: a package manifest
(`package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, `pom.xml`, `build.gradle`,
`Gemfile`, `composer.json`, `pubspec.yaml`, `mix.exs`) or a source root (`src/`, `cmd/`,
`app/`, `lib/`, `internal/`, `packages/`).

**Whenever neither is present, STOP and ask** — even if there are no sibling worktrees:

- Other worktrees or branches exist → list every worktree path with its branch and ask
  which to analyse.
- None exist → offer `analyse anyway / cancel`.

Do not guess, and do not check out or switch branches yourself — analyse the tree the
user names, in place. Call the result `<tree>`; every later `git`, `rg` and `Agent` call
targets it.

Docs-only roots are normal. When `<tree>` is a sibling worktree, still read the root
tree's `docs/`, `.claude/plans/`, ADRs and `CHANGELOG` — planning history usually lives
there.

### 0.2 Resolve the project — and confirm it

```
ion_current_project(cwd: "<tree>")
```

Always pass `cwd`. Without it the MCP server falls back to its own process cwd or
`ION_MEM_PROJECT`, which silently targets the wrong project.

Print the resolution verbatim and STOP:

> Project resolved: `acme` (source: git-remote, path: /Users/me/acme)
> Everything is read and written under this project. Correct? [yes / no]

`no` → ask for the project name to use, or cancel. **No `ion_delete` and no `ion_save`
may run before this line is confirmed.** Carry `<project>` into every later call.

### 0.3 Count existing memory and choose the mode

```
Bash: ion-mem status          # per-project "N obs" line is the authoritative count for <project>
ion_context(project: "<project>", limit: 10)      # sample only, never use its length as N
ion_search(query: "architecture decision convention", project: "<project>", limit: 20)
```

Take N from the `ion-mem status` per-project line (fall back to `ion_stats` if the CLI is
unavailable); `ion_context` and `ion_search` are capped samples and must not be used as the count.

- **0 observations** → greenfield memory, continue.
- **> 0 observations** → STOP and offer exactly two modes (plus cancel):

  > Memory already has N observations for `<project>`.
  > 1. **COMPLEMENT** (default) — keep everything, seed only what is missing.
  > 2. **REBUILD SEEDED MEMORY** — remove the observations previously seeded by
  >    `/ion-mem:init` for `<project>` and regenerate them. Hand-written observations and
  >    session summaries are kept. Soft delete, recoverable.
  >
  > [complement / rebuild / cancel]

  Default is **complement**. Record the answer; REBUILD executes in 0.5, after the
  backup, never before.

### 0.4 Backup — and verify where it landed

```bash
ion-mem status      # prints "Data dir: <path>" and per-project counts
ion-mem backup      # prints "Backup written to: <path> (N bytes)"
```

Assert that the backup path's parent chain contains the `Data dir` reported by
`ion-mem status` (default: `<data-dir>/backups/ion-mem-<ts>.db`). If the two do not
match, or either command fails, **STOP** — you cannot prove the backup covers the store
you are about to modify.

Record the per-project observation count from `ion-mem status` (or `ion_stats`) as
`count_before`.

### 0.5 Rebuild (only when the user chose REBUILD)

Skip this entire section on COMPLEMENT.

1. **Confirm verbatim.** Ask:

   > Type the project name `<project>` to confirm rebuilding its seeded memory.
   > Anything else cancels.

   Accept only an exact match. `yes`, `y`, `ok` or a near-match → reply **"Nothing was
   deleted."**, fall back to COMPLEMENT, and do not re-ask.

2. **Enumerate.** `ion_context(project: "<project>", limit: 200)` lists observations as
   `[id] **Title** (type)`. Then one `ion_search(query: "seeded by ion-mem init",
   project: "<project>", type: "<type>", limit: 50)` per type, plus one on the project's
   own vocabulary. Stop after two rounds that add no new ids — enumeration does not have
   to be exhaustive, because a row that is never enumerated is simply never deleted.

3. **Classify every id** with `ion_get_observation(id)`. Three gates, all required:

   | Check | Deletable |
   |-------|-----------|
   | `observation.project == "<project>"` | else KEPT (other project) — never delete |
   | `content` contains `Source: seeded by /ion-mem:init` | else KEPT (not seeded by this skill) |
   | `type != "session_summary"` | else KEPT (session summary) |

   Print the full classification. Rows that fail any gate are printed as
   `KEPT (not seeded by this skill)` with their id and title.

4. **Soft delete the marked rows, one id at a time:**

   ```
   ion_delete(id: <id>, hard: false)
   ```

   `hard: true` is forbidden in this skill under every circumstance.

5. **Second confirmation — required to delete any KEPT row.** Do not raise this unless
   the user explicitly asks to clear non-seeded rows as well. When they do, ask a
   separate question naming the exact count and requiring the project name again:

   > This additionally deletes **7** observations that were NOT seeded by this skill
   > (hand-written decisions and session summaries are among them). Type `<project>`
   > again to confirm deleting those 7. Anything else keeps them.

   Only after a second exact match may those ids be soft-deleted. Session summaries and
   rows from other projects stay excluded regardless of the answer.

6. **Reconcile.** Re-read the per-project count as `count_after` and assert
   `count_before − deleted == count_after`. A mismatch is a **failure**: stop, report both
   numbers and the deleted id list, and do not seed.

7. **Log the ids** for the final report.

Then continue to Phase 1. Phase 3 dedupe runs against the **remaining** rows only.

See [Recover](#recover) for how to undo this.

---

## Phase 1 — Reconnaissance (parallel, `sonnet`, `Explore`)

Launch 3–4 `Explore` subagents **in a single message**. Each gets a narrow brief, the
`<tree>` path, a ≤ 40 file budget and a ≤ 400 word report cap. Never use
`general-purpose` here: it carries `ion_*` write tools. Reports are raw signal, not
candidates.

| # | Brief | Looks at |
|---|-------|----------|
| 1 | Stack & structure | manifests, lockfiles, framework configs, entry points, top-2 directory levels, module boundaries |
| 2 | Docs & intent | `README`, `docs/`, ADRs (`docs/adr`, `docs/decisions`), `.claude/plans/`, `CHANGELOG`, `CONTRIBUTING`, `AGENTS.md`, `CLAUDE.md` |
| 3 | Git history | `git log` in the window; authors, hot files (`git log --name-only \| sort \| uniq -c \| sort -rn`), conventional-commit type mix, merge/refactor milestones |
| 4 | CI, tooling & quality gates | `.github/workflows/`, `Makefile`, `justfile`, lint/format configs, test runners, coverage thresholds, Docker/compose, release automation |

Standing instruction for every Phase 1 subagent:

> Read-only. Do not edit, write, or commit. Do not call any `ion_*` tool. Never quote or
> paraphrase credential values from `.env` files, CI secrets or configs — name the file,
> never the value. Report ≤ 400 words, facts with `path` or `path:line` evidence, no
> speculation. If a signal is missing, say "absent" instead of inferring it.

---

## Phase 2 — Deep Analysis (parallel, `opus`, `Explore`)

Pass the Phase 1 reports as context. Launch 4 `Explore` subagents **in a single
message**, one per dimension. Each returns up to 12 candidate observations.

| Dimension | Produces | Topic keys |
|-----------|----------|------------|
| Architecture & boundaries | layering, module contracts, data flow, integration points | `seed/architecture/auth-model` |
| Technical decisions | choices **with written rationale** | `seed/decision/orm-choice` |
| Conventions & patterns | naming, error handling, testing, commit style | `seed/pattern/error-handling` |
| Debt & gotchas | known workarounds, fragile areas, TODO/FIXME clusters, migration leftovers | `seed/discovery/legacy-sync-job`, `seed/config/ci-quality-gate` |

### Topic key rule (hard)

Every key is `seed/<family>/<slug>` — lowercase, hyphenated, `<family>` matching the type
(`architecture`, `decision`, `pattern`, `config`, `discovery`, `bugfix`, `preference`).
The `seed/` prefix is what keeps this skill from ever upserting a human-authored
observation. Use `ion_suggest_topic_key` for the slug, then prefix it.

### Evidence rule (hard)

Every candidate carries evidence: `path:line`, a commit hash, or a doc heading.
**No evidence → the candidate is dropped, not softened.**

### Type selection (hard)

| Situation | Type |
|-----------|------|
| Rationale is written down (ADR, plan, commit message, code comment, doc) | `decision` |
| The pattern is observable in code but nobody wrote down why | `discovery` or `pattern` |
| Structural shape of the system | `architecture` |
| Tooling, CI, env, quality gate | `config` |
| A fixed bug with a root cause recorded in history | `bugfix` |
| A stated team or user preference | `preference` |

Inferring intent and labelling it `decision` is the main failure mode of this skill.
When in doubt, downgrade to `discovery`.

Standing instruction for every Phase 2 subagent:

> Read-only. Do not edit, write, or commit. Do not call `ion_save`, `ion_update` or
> `ion_delete` — the main thread does all writing after user review. Never quote or
> paraphrase credential values from `.env` files, CI secrets or configs — name the file,
> never the value. Return candidates only, each with title, type, `seed/<family>/<slug>`
> key, evidence and a 3–5 line What/Why/Where/Learned body.

---

## Phase 3 — Review Table (STOP)

1. **Merge and deduplicate within the batch.** Two SAVE rows may never share a
   `topic_key`. On collision, keep the better-evidenced candidate, drop the other, and
   say so in the table (`DROPPED (duplicate key of #n)`). Also drop near-duplicates that
   differ only in wording.
2. **Check against the store.** `ion_search` is a similarity search (`search.mode`
   defaults to vector), so a key lookup alone can miss. For each candidate run:

   ```
   ion_search(query: "<title keywords>", type: "<type>", project: "<project>", limit: 50)
   ```

   If the candidate's `topic_key` appears in any result, or a result is clearly the same
   fact, mark **SKIP** with the reason. The `seed/` namespace makes an exact-key hit
   possible only against a previous run of this skill.
3. **Rank** by durability: architecture and written decisions first, then conventions,
   then debt. Trim to 40 SAVE rows; list anything trimmed under "Not proposed".
4. **Print the table and STOP.**

```
#   TYPE          TOPIC KEY                             TITLE                                   EVIDENCE                     ACTION
1   architecture  seed/architecture/service-boundaries  Split API and worker at the queue       docs/adr/0003-queue.md:12    SAVE
2   decision      seed/decision/orm-choice              Chose sqlc over an ORM for type safety  commit a1b2c3d              SAVE
3   pattern       seed/pattern/error-handling           Wrap domain errors at the port edge     internal/http/errors.go:44   SAVE
4   discovery     seed/discovery/legacy-sync-job        Nightly sync tolerates partial failure  cmd/sync/main.go:88          SKIP (already stored as `discovery/sync-job`)
5   discovery     seed/discovery/legacy-sync-job        Sync job swallows partial errors        cmd/sync/main.go:88          DROPPED (duplicate key of #4)
```

Then ask exactly one question:

> 32 candidates ready to seed, 4 skipped as duplicates, 2 dropped as intra-batch
> collisions. Accept all, pick ids (e.g. `1,3,7-12`), or cancel?

Wait. `cancel` ends the run with nothing written.

---

## Phase 4 — Seed

Save accepted candidates one at a time. Payload template:

```json
{
  "title": "Chose sqlc over an ORM for type-safe queries",
  "type": "decision",
  "scope": "project",
  "project": "<resolved project name>",
  "topic_key": "seed/decision/orm-choice",
  "capture_prompt": false,
  "content": "What: Queries are generated by sqlc from hand-written SQL instead of an ORM.\nWhy: Compile-time type safety and explicit SQL; stated in commit a1b2c3d and docs/adr/0003.\nWhere: internal/db/, sqlc.yaml, docs/adr/0003-sqlc.md\nLearned: Migrations stay hand-written; generated code is committed, not built in CI.\nSource: seeded by /ion-mem:init from commit a1b2c3d"
}
```

Rules:

- `capture_prompt: false` on every save — these are automated artifacts.
- `project` set explicitly; never rely on cwd detection inside a worktree.
- `topic_key` always starts with `seed/`.
- Last content line is always `Source: seeded by /ion-mem:init from <evidence>` — this
  marker is what a later REBUILD uses to know the row is safe to remove.
- **Overwrite guard.** If a save returns an error, or returns `revision_count > 1`, you
  overwrote something. STOP immediately, before any further save:
  1. `ion_history(id: <id>)` to read the prior revision.
  2. Restore it with `ion_update(id: <id>, title/content/type/topic_key from that
     revision)` — the only sanctioned use of `ion_update` in this skill.
  3. If history is unavailable, restore the 0.4 backup instead.
  4. Report what happened and do not resume seeding.

Then close with a summary:

```
ion_session_summary(summary: "...", project: "<project>")
```

The summary states: mode (COMPLEMENT or REBUILD), tree analysed and resolved project,
counts (deleted / kept / proposed / saved / skipped / trimmed), anything ambiguous or
deliberately left out, and the backup path.

Finally report to the user: mode, resolved project, backup path, saved count, skipped
list, and — on REBUILD — the full list of deleted ids plus the KEPT rows, with the note
that deletions are recoverable, and what a follow-up run should look at.

---

## Recover

Two independent recovery paths after a REBUILD. State both in the final report.

| Path | When | How |
|------|------|-----|
| Per-observation undelete | A few rows were removed by mistake, store otherwise fine | Call the `ion_undelete` MCP tool with each logged id — clears `deleted_at` and makes the row searchable again |
| Full store restore | The whole rebuild was wrong, or ids were lost | Restore the backup file printed in 0.4 over the store database, then restart the MCP server |

Caveats to repeat verbatim to the user:

- Soft-deleted rows stay recoverable only until `ion-mem prune --apply` hard-deletes
  them. Recover before pruning.
- Hard-deleted observations cannot be recovered by `ion_undelete` — only from the backup.
- The backup is a point-in-time copy: restoring it also rolls back anything saved after
  the backup, including this run's seeded observations.

---

## What NOT to Save

- Secrets, tokens, credentials, connection strings, `.env` values — ever, even redacted.
  Name the file that holds them; never the value.
- Per-file trivia: "`utils.ts` exports `formatDate`". Memory is for decisions, not an index.
- Anything an agent recovers by reading one file: exact function signatures, prop lists,
  route tables, dependency versions.
- Restatements of the README that carry no rationale.
- Speculation: "they probably chose X because Y" with no written source.
- Transient state: open TODOs assigned to a person, current sprint scope, failing test of
  the day.
- Personal data about contributors beyond authorship patterns already in `git log`.

Litmus test: **would a competent agent re-derive this in under a minute by reading one
file?** If yes, do not save it.
