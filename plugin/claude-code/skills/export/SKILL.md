---
name: ion-mem-export
description: "Trigger: \"ion-mem:export\", \"export project memory\", \"exportar memoria del proyecto\", \"compartir memoria\", \"share memory with\", \"pasarle la memoria\". Export one project's memory to a .ionmem.zip bundle for a teammate. NOT the bare `ion-mem export` CLI (whole-store JSONL dump), NOT `ion-mem backup`."
---

# ion-mem Export — Hand One Project's Memory to a Teammate

Package a single project's observations into a portable `.ionmem.zip` file that a
teammate on the **same repo** can import into their own store. No shared database, no
server, no cloud account — one file, handed over on a private channel.

The bundle is **private data**. It carries decisions, discoveries and — if you opt in —
raw user prompts. Treat it like a database dump, because that is what it is.

**Do NOT run this skill for**: `ion-mem export` (the whole-store JSONL operator dump —
a different command that exports every project), `ion-mem backup`, exporting a file
from the repo, or publishing memory anywhere public.

## Non-Negotiable Safety Contract

| Rule | Enforcement |
|------|-------------|
| Read-only against the store | The five tools this skill calls are `ion_export_project`, `ion_current_project`, `ion_search`, `ion_get_observation` and `ion_history` — none of them writes. `ion_save`, `ion_update`, `ion_delete` and `ion_set_status` are forbidden in every phase. Redacting an observation (2.5) is a separate, explicitly requested action, not part of this flow. |
| Explicit project and cwd everywhere | `project: "<resolved>"` **and** `cwd: "<tree>"` on every call. Never rely on server-side cwd detection — the MCP server's own process cwd is not your working tree. |
| Absolute output path | `out` is always an absolute path you computed. A relative `out` is only legal when `cwd` resolves it, and guessing which directory that is has no upside. |
| Secret scan is never skipped | The first `ion_export_project` call **always** runs without `allow_secrets`. `allow_secrets: true` may only appear in a second call, after the user typed the exact confirmation phrase in Phase 2. |
| Prompts are opt-in | `with_prompts` defaults to `false` and stays false unless the user says yes to the Phase 1 question. Prompts are raw conversation text — the most likely place a secret hides. |
| Soft-deleted rows are opt-in | `include_deleted` defaults to `false`. Rows someone deleted were deleted for a reason. |
| No secrets in the report | Never print a matched secret value, not even truncated or redacted. Print the `sync_id`, the title, and the pattern name — never the match. |
| Nothing to undo | This skill writes exactly one file outside the store and changes nothing inside it. No backup is needed, and none is taken. |

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

Then check that the tree looks like a real codebase: a package manifest
(`package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, `pom.xml`, `build.gradle`,
`Gemfile`, `composer.json`, `pubspec.yaml`, `mix.exs`) or a source root (`src/`, `cmd/`,
`app/`, `lib/`, `internal/`, `packages/`).

**Whenever neither is present, STOP and ask** — even if there are no sibling worktrees:

- Other worktrees or branches exist → list every worktree path with its branch and ask
  which one to export from.
- None exist → offer `export anyway / cancel`. On `export anyway`, keep the current tree
  as `<tree>` and continue: the tree only decides which project is detected and where
  the file lands, and 0.2 confirms the project before anything is read.

Do not guess, and do not check out or switch branches yourself. Call the result
`<tree>`; every later `git` command, `cwd` argument and `out` path targets it.

### 0.2 Resolve the project — and confirm it

```
ion_current_project(cwd: "<tree>")
```

Always pass `cwd`. Without it the MCP server falls back to its own process cwd or
`ION_MEM_PROJECT`, which silently exports the wrong project — and you would hand a
teammate someone else's memory.

Print the resolution verbatim and STOP:

> Project resolved: `acme` (source: git-remote, path: /Users/me/acme)
> Only this project's memory goes into the bundle. Correct? [yes / no]

`no` → ask for the project name to use, or cancel. **No `ion_export_project` call may
run before this line is confirmed.** Carry `<project>` into every later call.

---

## Phase 1 — Preview and Scope (STOP)

### 1.1 Count what would travel

```bash
ion-mem status      # the "by project" block prints "<project>  N obs  M prompts"
```

`N` and `M` come from that per-project line. When the CLI is unavailable, call
`ion_stats()` instead — it takes **no arguments**, not even `project`, so find your
project yourself in the returned `stats.by_project` array and read its
`observation_count` and `prompt_count`. Never use the length of an `ion_context` or
`ion_search` result as a count: both are capped samples.

`N` is **non-deleted** observations only, so it is exactly what a default export
carries. When `<project>` has no line in the `by project` block at all, or `N` is `0`,
STOP: there is nothing to share. Say so and ask whether they meant a different project
before going any further.

### 1.2 Ask about prompts and soft-deleted rows

Ask both in one message, and STOP:

> Exporting `acme`: **142 observations**, and **1 204 prompts** are on record.
>
> 1. **Include prompts?** Default **no**. Prompts are the raw text you and your
>    teammates typed — full conversation turns, not curated observations. They are the
>    most likely place for a pasted token, a customer name or an internal URL to be
>    sitting in the clear. [yes / no]
> 2. **Include soft-deleted observations?** Default **no**. Rows someone deleted stay
>    recoverable locally; shipping them to a teammate undoes that decision on their
>    machine. [yes / no]

A bare `yes` / `ok` / `dale` answers **nothing** here — it is agreement with being
asked, not an answer to two questions. Re-ask, one question at a time, and wait.

Unanswered means **no**. Record the two booleans as `with_prompts` and
`include_deleted`.

---

## Phase 2 — Secret Scan (STOP)

### 2.1 Scan by exporting without `allow_secrets`

Compute the output path first:

```bash
date -u +%Y%m%d          # the tool's own default uses the UTC date; match it
```

`<out>` = `<tree>/<project>-<YYYYMMDD>.ionmem.zip`, absolute.

Then call:

```json
{
  "project": "<project>",
  "cwd": "<tree>",
  "out": "<out>",
  "with_prompts": <Phase 1 answer 1, default false>,
  "include_deleted": <Phase 1 answer 2, default false>,
  "allow_secrets": false
}
```

`allow_secrets` is the one value that is hard-coded here — the other two are whatever
Phase 1 returned. This single call **is** the scan: the tool scans the assembled bundle
and refuses to create the file when it finds anything. Nothing is written on a refusal.

The scan covers exactly what the bundle would contain. Saying no to prompts in Phase 1
means prompts are **not scanned** — because they are not in the bundle either.

### 2.2 Read the response

| Response | Meaning | Next |
|----------|---------|------|
| `status: "ok"`, `findings: []` | Clean. The file is already written. | Go to Phase 3 — **do not call the tool again.** |
| `status: "error"`, `error_code: "invalid_argument"`, `result` starts with `refusing to export:` | Secrets found. Nothing was written. | Continue with 2.3. |
| `status: "error"`, any other `error_code` | A real failure (bad path, ambiguous project, database error). | Report it verbatim and stop. Do not retry with different arguments. |

### 2.3 Build the findings table

The refusal message names the count and up to **five** `sync_id:pattern` pairs, then
`and N more`. It carries no titles. Get the full list with titles from the CLI, which
prints every finding and — like the tool — writes nothing.

**First, prove the CLI reads the same store as the MCP server.** They resolve their data
directory independently, and a CLI pointed at a different database would hand you titles
belonging to other observations. You already read `ion-mem status` in 1.1 — compare its
per-project `N obs` figure against your project's `observation_count` in `ion_stats()`
(no arguments; find the project in `stats.by_project` yourself):

| Outcome | What to do |
|---------|------------|
| They agree | Run the CLI below as-is. |
| They disagree, and `ion-mem status` printed a `Data dir: <path>  (12.4 MB)` line | Strip the `(size)` suffix and retry with `--data-dir <path>`. Still disagreeing → skip the CLI. |
| They disagree with no usable data dir, or the CLI is missing | **Skip the CLI entirely** and use the fallback below. |

Run the scan, the check and the cleanup as **one** shell invocation — `SCAN_TMP` does
not survive between separate Bash calls, and a lost variable means a leaked bundle:

```bash
SCAN_TMP="$(mktemp -d)"
ion-mem export-project --project <project> --out "$SCAN_TMP/scan.ionmem.zip"
#   add --with-prompts / --include-deleted to match the Phase 1 answers exactly
#   add --data-dir <path> per the table above
#   NEVER add --allow-secrets here
test -f "$SCAN_TMP/scan.ionmem.zip" && echo UNEXPECTED-FILE || echo clean
rm -rf "$SCAN_TMP"
```

Use `;` or newlines between those commands, never `&&`: `export-project` **exits
non-zero** when it finds secrets, which is the expected path here, and an `&&` chain
would swallow the cleanup.

It prints `  [<sync_id>] <title> (<pattern>)` per finding and writes nothing. The
`--out` goes to a throwaway directory, never to `<out>`: a CLI that found no secrets
would write a real bundle there, and that must never be the file you are about to hand
over. `UNEXPECTED-FILE` means the CLI disagreed with the tool about whether this bundle
has secrets — trust the tool, say so, and use the fallback. The `rm -rf` runs either
way; never leave a bundle sitting in a temp directory.

**Fallback**, whenever the CLI is unavailable, reads a different store, or reports a
different finding count: say so plainly and build the table from the `sync_id:pattern`
pairs in the refusal message, with the TITLE column reading `(unavailable)`. Fewer
columns is fine; wrong titles are not.

Print the table:

```
SECRET-SHAPED CONTENT FOUND — nothing was written

#   SYNC ID                               PATTERN        TITLE
1   9f3c1a20-5b7e-4f1a-8c22-0e6d4a11b8c7  github_token   Wired CI deploy to the staging cluster
2   2b7d5e11-0a49-4c8f-91dd-7c3e5f0a4419  password       Set up the local Postgres container
3   c04a8f37-16b2-4d55-a7e1-9b28c6f30d5a  private_key    Rotated the signing key after the outage
```

Patterns the scanner knows: `aws_key`, `api_key` (`sk-…`), `github_token`,
`gitlab_token`, `private_key`, `jwt`, and non-placeholder `password=` / `secret=`
assignments. It is deliberately conservative, so a hit is usually real — but a false
positive is possible (a doc showing an example token).

**Never print the matched value**, not even partially. Name the observation, not the
secret.

### 2.4 STOP and offer exactly three options

> 3 observations contain secret-shaped content. Nothing has been written.
>
> 1. **Fix the observations first** (recommended) — I list each one so you can decide.
>    Then we re-scan.
> 2. **Export anyway** — the secrets go into the file your teammate receives. To choose
>    this, type exactly: `export with secrets`
> 3. **Cancel** — nothing is written and nothing changes.

Wait, and read the answer strictly.

| Answer | Action |
|--------|--------|
| `1` / `fix` / `arreglar` | Go to 2.5. |
| The exact string `export with secrets` | Go to 2.6. |
| `2` / `export anyway` / anything close but not exact | **Not accepted.** Re-print option 2's exact phrase and wait again. |
| `3` / `cancel` / `no` | End the run. Nothing was written. |
| a bare `yes` / `sí` / `ok` / 👍 | **Answers nothing.** Re-print the three options and wait again. |

### 2.5 Fixing the observations (the human decides, you propose)

You have `sync_id`s; the write tools take local `id`s. Bridge the two:

```
ion_search(query: "<words from the finding's title>", project: "<project>", limit: 50)
```

Each result carries both `id` and `sync_id`. Match on `sync_id` — never on the title
alone, because two observations can share a title. If no search finds it, say so and
leave that row for the user to locate in the TUI (`ion-mem dash`).

For each matched row, `ion_get_observation(id)` and show the user the surrounding
sentence — with the secret itself masked — then recommend one of:

- **`ion_update(id: <id>, content: "<same content, secret replaced by a placeholder>")`**
  — the secret was incidental; the observation is still worth sharing. This is what you
  will usually recommend. Pass only the fields you are changing; omitted fields keep
  their current values.
- **`ion_set_status(id: <id>, status: "obsolete", reason: "…")`** — the observation
  only existed to hold that value. `bugfix` and `discovery` rows can never be marked
  obsolete (the store refuses it); for those, recommend `ion_update` instead.

**This skill does not run those calls.** Print each one exactly, one row at a time, and
let the user decide. Rewriting someone's memory to make an export succeed is not an
export step — and "fix them all" is not approval for edits they have not read.

If the user asks you to apply a call anyway, that is fine, but say plainly that you are
stepping outside the export flow: run only the specific calls they approved, one row at
a time, and note in the final report which observations you changed.

A secret in memory usually means the secret is also live somewhere. Say so, and suggest
rotating it — editing the observation hides the string, it does not revoke the key.

Once the fixes are in, **go back to 2.1 and re-scan from scratch.** A partial fix is a
failed fix, and only the scan can tell you which it was.

### 2.6 Export with secrets (only after the exact phrase)

Repeat the same call from 2.1 with `allow_secrets: true` and every other argument
unchanged. The response is `status: "ok"` and `findings` now lists every finding as
`{sync_id, title, pattern}` — use it to restate, in the Phase 3 report, exactly which
observations carry secrets into the file.

---

## Phase 3 — Report the Written File

The bundle exists on disk. Print the manifest summary from the success envelope:

```
Exported: /Users/me/acme/acme-20260927.ionmem.zip

  project:        acme
  exported_at:    2026-09-27T14:02:11Z
  observations:   142
  revisions:      38
  prompts:        not included (with_prompts was false)
  format version: 1
```

`observations`, `revisions` and `prompts` come from `manifest.counts`; say
"not included" when `manifest.includes_prompts` is `false` rather than printing `0`,
because the two mean different things.

When 2.6 was used, add:

```
  ⚠ 3 observations carry secret-shaped content into this file (allow_secrets was set):
      [9f3c1a20-…] github_token — Wired CI deploy to the staging cluster
      …
```

### 3.1 Say how to hand it over

State this verbatim:

> This file is a database dump of `<project>`'s memory. Send it over a private channel
> — a DM, an encrypted share, a drive folder your team already controls. Do not attach
> it to a public issue, a PR, a shared Slack channel outside the team, or anything
> indexed. On the other side, `/ion-mem:import` merges it without overwriting their
> rows.

### 3.2 Check that the file cannot be committed

The bundle sits inside the repo tree, which means one `git add -A` away from the
history.

```bash
git -C "<tree>" check-ignore -q "<out>" && echo ignored || echo NOT-IGNORED
```

- `ignored` → say so in one line and move on.
- `NOT-IGNORED` → tell the user and offer the fix. **Do not edit `.gitignore`
  yourself** unless they say yes:

  > `acme-20260927.ionmem.zip` is not gitignored — one `git add -A` would commit your
  > team's memory to the repo. Add `*.ionmem.zip` to `<tree>/.gitignore`? [yes / no]

  On `yes`, append the line (and a short comment) to `<tree>/.gitignore`, then re-run
  `git check-ignore` to prove it took effect.

Finally report: resolved project, absolute file path, counts, whether prompts and
soft-deleted rows were included, whether `allow_secrets` was used and for which
observations, and the gitignore status.

---

## Recover

**There is nothing to recover.** This skill is read-only against the store: no
observation is created, changed, deleted or re-statused, and no backup is needed.

The only artifact is the `.ionmem.zip` file. To undo the export, delete that file — and
if you already sent it, treat anything inside it as disclosed: rotate any secret it
carried, exactly as you would after pasting it into a chat.

The one exception is a redaction the user asked you to apply in 2.5. That is a normal
`ion_update`, so it kept the previous content as a revision: read it back with
`ion_history(id: <id>)` and restore it with `ion_update` if the edit was wrong.

---

## What NOT to Do

- **Never call `ion_export_project` with `allow_secrets: true` first.** The refusal is
  the feature. A scan you skipped is a scan that did not happen.
- **Never treat a near-match as the confirmation phrase.** `export anyway`, `yes`,
  `dale` and `ok` are not `export with secrets`.
- **Never print a matched secret value** to justify a finding — not truncated, not
  masked-but-recognisable. The `sync_id`, title and pattern name are enough.
- **Never redact an observation on your own initiative.** You surface the row and the
  exact call; the human decides whether that content should change. Memory is theirs,
  and a rewrite to make an export succeed is the wrong reason to touch it.
- **Never export a project you did not confirm in 0.2**, and never re-use a `project`
  value a teammate mentioned in passing instead of the resolved one.
- **Never write the bundle outside `<tree>`** without being asked — least of all into a
  synced cloud folder, `/tmp` (some systems world-read it) or a directory you did not
  check for gitignore coverage.
- **Never hand over a bundle by pasting its contents.** It is a zip; there is nothing
  useful to paste, and trying produces a corrupted file.
