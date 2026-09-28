---
name: ion-mem-curate
description: "Trigger: \"ion-mem curate\", \"curate memory\", \"clean up memory\", \"sanear memoria\", \"limpiar memoria\", \"depurar memoria\", \"revisar decisiones obsoletas\". Propose observation status changes. NOT repo cleanup, prune, or delete."
---

# ion-mem Curate — Memory Hygiene Without Deletion

Memory drifts. Decisions get replaced, conventions change, the files an observation
cites stop existing. An agent that reads all of it as equally true gets misled.

This skill finds that drift and proposes **status changes** — `superseded` or
`obsolete` — for your review. It never deletes anything, and it never touches an
observation's content.

**Bugs are never lost.** `bugfix` and `discovery` observations are the project's
permanent learning record: when something related happens, the agent already knows
where to look. They can only ever be marked `superseded` by a newer permanent
observation, never `obsolete`, never deleted.

**Do NOT run this skill for**: generic repo cleanup, dead-code pruning,
`ion-mem prune`, deleting observations, or seeding memory (`/ion-mem:init`).

## Non-Negotiable Safety Contract

| Rule | Enforcement |
|------|-------------|
| Never delete | `ion_delete` is forbidden in every phase, at every `hard` value. `ion-mem prune` is never invoked. This skill has no delete path. |
| Never rewrite content | `ion_update` and `ion_save` are forbidden. Exactly two write tools are allowed: `ion_set_status`, and one `ion_session_summary` call closing Phase 4. Neither changes an existing observation's title, type or content. |
| Write only after the STOP | No `ion_set_status` before the Phase 3 table is printed and the user names what to accept. A user who says nothing has accepted nothing. |
| Permanent types are permanent | `bugfix` and `discovery` may only receive `status: "superseded"`, and only when `superseded_by` points at another `bugfix` or `discovery`. `obsolete` on a permanent row is forbidden — the store refuses it, and so do you. |
| Backup first, verified | `ion-mem backup` runs in 0.4 and its path is asserted against the `Data dir` from `ion-mem status` before any status change. |
| Explicit project | `project: "<resolved>"` on every `ion_context`, `ion_search` and `ion_session_summary`. `ion_set_status` and `ion_get_observation` take **no** `project` argument — they act on any id in the store, so the project gate is enforced by you (Phase 4 gate 1), not by the tool. |
| Evidence or no proposal | Every proposal carries evidence: a missing path, a quoted phrase, an id pair, a commit hash. No evidence → the row is `REVIEW`, not a change. |
| Hard cap | Max 40 **actionable** proposals per run (`superseded` + `obsolete`); `REVIEW` rows change nothing and do not count. Over the cap → keep the best-evidenced 40 and list the rest as not proposed. |
| Reversible by construction | Every change is undone by `ion_set_status(id, status: "active")`. The final report lists every changed id with its previous status. |
| Read-only analysis | Detector subagents are `subagent_type: "Explore"`. `Explore` still exposes the `ion_*` write tools — the standing instruction "never call any `ion_*` tool, never Write/Edit" is the only guard, and it is mandatory verbatim in every brief. |

## Model Assignments

Pass `model` and `subagent_type: "Explore"` explicitly on every `Agent` call.

| Phase | Agent work | Model |
|-------|-----------|-------|
| 2a | Dead evidence (path/commit existence in the tree) | `sonnet` |
| 2b | Contradictions (judging replacement between rows) | `opus` |
| 2c | Duplicates (near-identical rows) | `sonnet` |
| 2d | Permanent lineage (fix/discovery chains) | `opus` |
| 0, 1, 3, 4 | Preflight, inventory, review table, apply | main thread (no subagent) |

## Budget

| Limit | Value |
|-------|-------|
| Rows enumerated per run | ≤ 200. More than that → curate one type per round. |
| Rows handed to one detector | ≤ 60. A larger slice is split by type across sequential rounds of that same detector, never widened. |
| Detector subagents | 4, launched in one message (parallel) |
| Files read per subagent | ≤ 40 (only 2a reads the tree; 2b–2d read no files) |
| Report length per subagent | ≤ 400 words |
| Proposals per detector | ≤ 15 |
| Enumeration rounds | ≤ 3 per run (24 `ion_search` calls) |
| Actionable proposals applied per run | ≤ 40 (`REVIEW` rows do not count) |

## Observation Types

| Group | Types | May become |
|-------|-------|-----------|
| Supersedable | `decision`, `architecture`, `pattern`, `config`, `preference`, `manual` | `superseded` or `obsolete` |
| Permanent | `bugfix`, `discovery` | `superseded` only, and only by another permanent row |
| Excluded | `session_summary` | never proposed — session history is not curated |

---

## Phase 0 — Preflight

Order matters: the tree is chosen **before** the project is resolved, because project
detection is cwd-derived.

### 0.1 Detect the real working tree

```bash
git rev-parse --show-toplevel
git worktree list
git branch -a
```

Dead-evidence detection checks paths against this tree, so picking the wrong one
produces false "file is gone" results. Check that the tree looks like a real codebase:
a package manifest (`package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`,
`pom.xml`, `build.gradle`, `Gemfile`, `composer.json`, `pubspec.yaml`, `mix.exs`) or a
source root (`src/`, `cmd/`, `app/`, `lib/`, `internal/`, `packages/`).

**Whenever neither is present, STOP and ask** — even if there are no sibling worktrees:

- Other worktrees or branches exist → list every worktree path with its branch and ask
  which to curate against.
- None exist → offer `curate anyway / cancel`. On `curate anyway`, detector 2a is
  **skipped entirely**: without a real tree, every path looks missing.

Do not check out or switch branches yourself. Call the result `<tree>`; every later
`git`, `fd`, `rg` and `Agent` call targets it.

### 0.2 Resolve the project — and confirm it

```
ion_current_project(cwd: "<tree>")
```

Always pass `cwd`. Without it the MCP server falls back to its own process cwd or
`ION_MEM_PROJECT`, which silently targets the wrong project.

Print the resolution verbatim and STOP:

> Project resolved: `acme` (source: git-remote, path: /Users/me/acme)
> Only observations belonging to this project will be read or changed. Correct? [yes / no]

`no` → ask for the project name to use, or cancel. **No `ion_set_status` may run before
this line is confirmed.** Carry `<project>` into every later call.

### 0.3 Count what is there

```bash
ion-mem status          # "Data dir: <path>  (12.4 MB)", store-wide "status:" line, per-project "N obs"
```

The `Data dir:` line ends with the database size in parentheses. Strip that suffix and
keep only the path — comparing against the raw line makes the 0.4 assertion fail on a
store that is otherwise fine.

```
ion_context(project: "<project>", limit: 10)      # sample only, never a count
```

Record two numbers as `before`:

- **`status_before`** — the `status: N active · N superseded · N obsolete` line.
  This line is **store-wide**, not per project; say so when you report the delta.
- **`obs_before`** — the per-project `N obs` figure from the `by project` block (fall
  back to `ion_stats` if the CLI is unavailable). It must not change at all: curation
  never adds or removes rows.

If `obs_before` exceeds 200, the project will likely overflow the enumeration cap.
**STOP and ask** whether to sweep all types anyway (and accept that enumeration will be
partial) or to curate one type this round, offering this order, highest value first:
`decision → architecture → pattern → config → preference → bugfix/discovery`. The cap
itself is enforced in Phase 1, on the curation set — not on this number.

### 0.4 Backup — and verify where it landed

```bash
ion-mem backup      # prints "Backup written to: <path> (N bytes)"
```

Assert that the backup path's parent chain contains the `Data dir` **path** already
read from `ion-mem status` in 0.3 — the path with its `(size)` suffix stripped
(default: `<data-dir>/backups/ion-mem-<ts>.db`). If the two do not match, or
`ion-mem backup` fails, **STOP** — you cannot prove the backup covers the store you are
about to modify. Print the backup path; it goes in the final report.

---

## Phase 1 — Inventory

Build the row set the detectors will judge. Everything happens in the main thread over
MCP; do not shell out to `ion-mem export`.

1. **List recent rows.** `ion_context(project: "<project>", limit: 100)` prints
   `[id] **Title** (type)` with a content preview. It does **not** report status.

2. **Enumerate per type.** `search.mode` defaults to `vector`, so search is a
   similarity ranking, not a table scan — one query never returns everything. Run one
   call per supersedable type, then one per permanent type:

   ```
   ion_search(query: "<project vocabulary: domain nouns, subsystem names>",
              type: "decision", project: "<project>",
              include_superseded: true, limit: 50)
   ```

   Repeat for `architecture`, `pattern`, `config`, `preference`, `manual`, then
   `bugfix` and `discovery`. Vary the query across rounds (domain terms, then
   `"decision architecture convention"`, then terms harvested from titles already
   found). Stop after two rounds that add no new ids, and **never run more than 3
   rounds — 24 `ion_search` calls — per run**, whatever they return. Enumeration does
   not have to be exhaustive, because a row that is never enumerated is simply never
   changed. When round 3 was still finding new ids, say so in the final report so the
   user knows a follow-up run has more to look at.

   `include_superseded: true` is deliberate, but **not** because those rows can be
   pointed at — they cannot. You include them so you can see the lineage that already
   exists: which rows are already curated (so nothing is re-proposed), and where a
   chain ends, so a new proposal points at the newest **active** row rather than at a
   link partway up.

3. **Sort the ids into two lists.** From each search row use `id`, `title`, `type`,
   `project`, `status`, `topic_key`, `created_at`.

   | Condition | Goes to |
   |-----------|---------|
   | `project != "<project>"` | discarded — out of scope, always |
   | `type == "session_summary"` | discarded — session history is not curated |
   | `status != "active"` | the **context list**: background for detectors only. Never proposed, and **never a supersession target** — pointing at a superseded or obsolete row creates a dead pointer. |
   | everything else | the **curation set**: the rows detectors may propose changes for, and the only rows that may be named as supersession targets |

4. **Fetch full content** with `ion_get_observation(id)` for every row in the curation
   set — the `Where:` line and the evidence a detector needs usually sit past the
   300-byte preview. Cap at 200 rows; over that, go back to the 0.3 decision and pick a
   single type for this round. **Do not re-run `ion-mem backup`** — the 0.4 backup
   still covers the store and is still the one named in the report.

5. **Split the curation set in two.** Supersedable rows go to detectors 2a, 2b and 2c.
   Permanent rows (`bugfix`, `discovery`) go **only** to 2d. They are handed to 2b and
   2c as read-only context so those detectors can see the full picture, with the
   instruction that permanent rows are out of scope for their proposals.

---

## Phase 2 — Detectors (parallel, `Explore`)

Launch all four in a single message, each with `subagent_type: "Explore"`.

`Explore` still exposes the `ion_*` write tools. Nothing in the harness stops a
detector from calling `ion_set_status` on its own — the standing instruction below is
the only guard, so it is **mandatory in every brief**, verbatim, never summarised away.

Each receives: the `<tree>` path, its slice of the inventory as plain text (`id`,
`type`, `title`, `topic_key`, `created_at`, full content), its budget, and this
standing instruction:

> Read-only. Do not edit, write, or commit. Do not call any `ion_*` tool — the main
> thread owns all memory access and does all writing after user review. Never quote or
> paraphrase credential values, whether they come from `.env` files, CI secrets,
> configs or the observation content you were handed — name the file or the row,
> never the value; render anything credential-shaped as `<redacted>`. Return proposals
> only: `id`, proposal (`superseded→<id>` | `obsolete` | `REVIEW`), a one-line reason,
> and concrete evidence. No evidence → return `REVIEW`, not a change. Report ≤ 400
> words.

The redaction rule binds the main thread too. Observation content is arbitrary text
that someone once pasted into memory, and this run echoes it into three places a
detector never sees: the Phase 3 table, the `reason` string on every `ion_set_status`,
and the Phase 4 session summary. In all three, carry ids, titles and paths only, and
render any credential-shaped value as `<redacted>` — a leaked secret in a `reason`
string is written into the store permanently.

### 2a — Dead evidence (`sonnet`)

Skipped entirely when 0.1 produced no real tree.

For each supersedable row, extract every concrete reference from the content — file
paths (usually the `Where:` line), package or module names, commit hashes — and check
whether it still exists in `<tree>`:

```bash
fd --full-path 'internal/auth/token.go'        # exact path
fd --type f 'token.go'                          # basename, anywhere
git -C <tree> log --follow --oneline -- internal/auth/token.go   # renames
git -C <tree> cat-file -e a1b2c3d^{commit}      # commit still reachable
```

**Renames are not death.** A path counts as gone only after all three checks fail:
exact path, basename anywhere in the tree, and `git log --follow`. When a plausible
successor path exists, the row is `REVIEW` with both paths named — not `obsolete`.

| Finding | Proposal |
|---------|----------|
| Every cited reference is gone, no successor found | `obsolete`, listing each missing path |
| Every reference is gone **and** another active row in the curation set covers the same area | `superseded→<id>` |
| Some references survive | `REVIEW` — or nothing, when the survivors carry the point |
| The row cites nothing concrete | no proposal — absence of paths is not evidence of death |

### 2b — Contradictions (`opus`)

Group supersedable rows by topic: same `topic_key` family (the part before the last
`/`), overlapping title keywords, or the same area in their `Where:` lines. For each
group of 2 or more, judge whether a newer row replaces an older one.

Accept as replacement evidence: an explicit statement (`replaces`, `instead of`,
`changed from`, `we now`, `migrated off`), a later `created_at` on a row that states the
opposite conclusion, or a newer commit cited for the same decision point.

Be conservative. Two rows about the same area are not a contradiction — they are often
two facts. **If unsure, return `REVIEW` and say what is unclear.** A wrong `superseded`
teaches the agent a false history.

Proposal shape: `superseded→<newer id>` on the older row, reason quoting the phrase that
proves it, e.g. `"row #108 states 'moved off Redis to in-process LRU'"`.

### 2c — Duplicates (`sonnet`)

Find near-identical rows stored under different `topic_key`s — same fact, different
wording. Propose `superseded→<newer id>` on the older one. Never propose `obsolete` for
a duplicate: the fact is still true, only the row is redundant.

A different level of detail is not a duplicate. Keep both when each carries something
the other does not.

Permanent rows: two `bugfix` rows about the same area stay **both active**, unless one
is a strict subset of the other — same root cause, same fix, no detail the newer one
lacks. That case belongs to 2d, not here.

### 2d — Permanent lineage (`opus`)

Only `bugfix` and `discovery` rows. Look for chains where a later row supersedes an
earlier one on the same problem: `"fixed again"`, `"the root cause was actually"`,
`"the earlier fix was incomplete"`, `"this reverts"`, a second fix in the same file for
the same symptom.

The only legal proposal here is `superseded→<newer permanent id>`. `obsolete` is
forbidden and the store refuses it. Verify the target is `bugfix` or `discovery` and
still `active` before proposing it — pointing a permanent row at a `decision` is
rejected, and pointing it at an already-superseded fix buries the lineage one link
short of the answer.

When the later row is a *different* bug in the same file, return nothing. Two bugs in
one file is normal and both are worth remembering.

---

## Phase 3 — Review Table (STOP)

1. **Merge.** One id gets one proposal. When two detectors disagree on the same id,
   keep the more conservative outcome and note the conflict in the reason:

   | Disagreement | Outcome |
   |--------------|---------|
   | one says `REVIEW` | `REVIEW` |
   | `superseded` vs `obsolete` | `superseded` — the row still has a successor worth pointing at |
   | `superseded` toward **different** targets | `REVIEW`. Two detectors reading two different successors means the lineage is not settled; picking one is a guess. Name both targets in the reason. |
   | `superseded` toward the same target | keep it, and cite both detectors' evidence |
2. **Validate every proposal before it reaches the table.** The first two checks
   **discard** a proposal — it is never printed. Every later check demotes it to
   `REVIEW` instead, so the user still sees the row and the reason.

   | Check | Why |
   |-------|-----|
   | DISCARD: proposal is `obsolete` **and** the row's type is `bugfix` or `discovery` | A detector that proposes obsoleting a permanent row got the rules wrong. Printing it invites an accept the store will refuse. Drop it and note the detector misfired. |
   | DISCARD: the row's `id` is not in the curation set | A detector may only judge rows it was given. An id from anywhere else is untrusted — discard it, never look it up to "check". |
   | `target != id` | a row cannot supersede itself |
   | target's `project == "<project>"` | the store rejects cross-project supersession |
   | target exists and is not deleted | `ion_get_observation(target)` returns it — fetch it now if it was not already in the inventory; a missing or deleted target is rejected by the store |
   | target's `status` is `active` | a superseded or obsolete target is a dead pointer. Point at the newest active row in that lineage instead. Never rely on the store to catch this. |
   | target is not itself proposed `superseded` or `obsolete` in this run | never build a chain — point at the newest row instead |
   | when the row is permanent: target's type is `bugfix` or `discovery` | the store rejects any other target |
3. **Trim** to 40 proposals, best-evidenced first. The cap counts **actionable**
   proposals only — `superseded` and `obsolete`. `REVIEW` rows change nothing, so they
   are printed on top of the 40 and never displace a real proposal. List anything
   trimmed under "Not proposed".
4. **When nothing survives, say so and stop.** Zero proposals is a good outcome, not a
   failure: report "memory is clean for `<project>` — N rows checked, no drift found",
   list any `REVIEW` rows for context, and end the run. Do not print an empty table and
   do not ask a question with nothing to accept.
5. **Otherwise print the table and STOP.** Group by detector. Permanent rows get their
   own section.

```
SUPERSEDABLE ROWS

#   ID    TYPE          CURRENT     PROPOSAL          REASON                                 EVIDENCE
1   41    decision      active      superseded→108    Replaced by the in-process LRU choice  #108: "moved off Redis to in-process LRU"
2   63    config        active      obsolete          Cited CI workflow no longer exists     .github/workflows/deploy-v1.yml absent
3   77    pattern       active      superseded→91     Same fact as #91, older wording        #91 covers it with the port example
4   85    architecture  active      REVIEW            Overlaps #90 but may be a second fact  both describe the queue boundary

PERMANENT ROWS (bugfix / discovery)
Rule: these can only be marked `superseded`, and only by another bugfix/discovery row.
They are never marked obsolete and never deleted — a fixed bug stays as a learning marker.

#   ID    TYPE          CURRENT     PROPOSAL          REASON                                 EVIDENCE
5   52    bugfix        active      superseded→119    #119 found the real root cause         #119: "the earlier fix only masked the race"

NOT PROPOSED (over the 40 cap): #12, #19, #33
```

Then ask exactly one question:

> 4 changes ready: 3 supersessions (one of them permanent) and 1 obsolete. 1 more row
> is flagged REVIEW and will not be changed.
> Accept all, pick rows by their `#` (e.g. `1,3,5`), or cancel?

Wait, and read the answer strictly.

| Answer | Meaning |
|--------|---------|
| `accept all` / `todas` / `all` | apply every actionable row |
| a list of `#` values | apply exactly those rows |
| `cancel` / `no` | end the run, nothing written |
| **a bare `yes` / `sí` / `ok` / `dale` / 👍** | **answers nothing.** It is agreement with being asked, not a choice among three options. Re-print the three options and wait again. |

Picks refer to the `#` column, never to observation ids — re-ask rather than guessing
when a number could be either. `REVIEW` rows are never applied, even under "accept
all": they are reported, not changed.

---

## Phase 4 — Apply

For each accepted proposal, one at a time, in table order:

1. **Project gate.** `ion_get_observation(id)` and assert
   `observation.project == "<project>"`. `ion_set_status` takes no `project` argument
   and will happily change a row in another project — this check is the only thing
   standing between you and someone else's memory. A mismatch **stops the run**.
2. **Record the previous state** — `observation.status`, plus `superseded_by` and
   `status_reason` when they are set. This record is the only way to undo the change,
   because a status transition writes no revision row.
3. **Change it.**

   ```json
   {"id": 41, "status": "superseded", "superseded_by": 108, "reason": "replaced by the in-process LRU decision (#108)"}
   ```

   ```json
   {"id": 63, "status": "obsolete", "reason": "cited workflow .github/workflows/deploy-v1.yml no longer exists"}
   ```

   `reason` is always set and always states the evidence, not the verdict. Write
   "cited file X is gone", not "outdated".
4. **Verify.** `ion_get_observation(id)` and assert the new `status` — and
   `superseded_by` when superseding.
5. **Handle the two failure shapes differently.**

   | Failure | Action |
   |---------|--------|
   | `ion_set_status` returns an error (rejected proposal) | Skip this proposal, report the message verbatim, continue with the remaining accepted ones. **Never** retry with different arguments — a rejection means the proposal was wrong, not underspecified. |
   | Step 4 shows a status other than the one you asked for | **STOP the whole run.** The store is not behaving as expected. Report every id changed so far with its previous state and do not apply anything else. |

Then reconcile:

```bash
ion-mem status      # status_after + obs_after
```

- `obs_after` must equal `obs_before` exactly. Any change means something deleted a
  row, which this skill never does — report it as a failure.
- Print `status_before` → `status_after` and label the line store-wide. The per-project
  truth is the changed-id list, because `ion-mem status` has no per-project status
  breakdown.

Close with a summary:

```
ion_session_summary(summary: "...", project: "<project>")
```

The summary states: tree curated and resolved project, the backup path, counts
(enumerated / proposed / accepted / applied / REVIEW / trimmed), whether enumeration
stopped at the 3-round bound while still finding new ids, the full list of changed ids
as `#<id> <previous status> → <new status>`, and this line verbatim:

> Every change is reversible: `ion_set_status(id: <id>, status: "active")` restores the
> previous state and clears `superseded_by`.

Finally report the same to the user, plus what a follow-up run should look at.

---

## Recover

Nothing was deleted, so recovery is a status change — not a restore.

| Path | When | How |
|------|------|-----|
| Per-observation revert | One or a few proposals were wrong | `ion_set_status(id: <id>, status: "active")` for each id from the report. This clears `superseded_by` and `status_reason` too. |
| Re-supersede | The row was already superseded before this run and the wrong pointer was written | `ion_set_status(id: <id>, status: "superseded", superseded_by: <original target>, reason: "<original reason>")` — both are in the report. |
| Full store restore | The whole run was wrong | Restore the backup file printed in 0.4 over the store database, then restart the MCP server. |

Caveats to repeat verbatim to the user:

- A status change writes no revision row, so `ion_history` will not show it. The report
  produced in Phase 4 is the record of previous statuses — keep it.
- The backup is a point-in-time copy: restoring it also rolls back anything saved after
  the backup, not just this run.
- `obsolete` does not delete: the row stays in the store and stays searchable, ranked
  lower and labeled.

---

## What NOT to Do

- **Never mark a row `obsolete` because it "seems old".** Age is not evidence. A
  four-year-old architecture decision that still holds is the most valuable row in the
  store.
- **Never mark a permanent row `obsolete`.** `bugfix` and `discovery` are the learning
  record — when something related breaks again, that row is where the agent looks first.
  The store refuses it; do not go looking for a way around that.
- **Never touch a row whose `project` is not the resolved project.** `ion_set_status`
  and `ion_get_observation` are id-global; the gate is yours to enforce.
- **Never change content.** No `ion_update`, no `ion_save`, no rewording a title to
  "fix" it. If a row is wrong, that is a human edit, not curation.
- **Never delete.** No `ion_delete`, no `ion-mem prune`, at any `hard` value, in any
  phase, for any reason a subagent gives you.
- **Never act on a subagent's proposal without the Phase 3 STOP.** Detectors propose;
  only the user accepts.
- **Never supersede toward a row that is not active** — neither one already
  `superseded`/`obsolete` in the store, nor one this run is retiring. Both are dead
  pointers that send a future agent one link short of the answer. Point at the newest
  active row in the lineage.
- **Never treat "no evidence" as "safe to retire".** A row with nothing citable is a
  `REVIEW` at most, and usually nothing at all.

Litmus test before every proposal: **can I name the thing that proves this is no longer
true?** If not, it is not a proposal.
