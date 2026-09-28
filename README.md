# ion-mem

**Persistent memory for AI coding agents.** Your agent forgets everything when the
session ends or the context window compacts — so every morning you re-explain the
same architecture, and every refactor re-derives a decision the team settled months
ago. ion-mem gives the agent a memory that survives both: decisions, conventions,
bug fixes and discoveries are saved as typed observations in a local SQLite store,
scoped per project, and retrieved by semantic search. It is local-first (nothing
leaves your machine) and agent-agnostic — the memory is exposed as 19 MCP tools, so
any MCP-capable agent can use it. Claude Code gets a plugin with hooks and skills on
top.

---

## Install

**Homebrew (recommended)**

```bash
brew install arodriguezp2003/tap/ion-mem
```

**Go**

```bash
go install github.com/arodriguezp2003/ion-mem/cmd/ion-mem@latest
```

Then register the Claude Code plugin:

```bash
claude plugin marketplace add arodriguezp2003/ion-mem
claude plugin install ion-mem@ion-mem
```

**Restart Claude Code** (or run `/reload-plugins`) so the hooks, the MCP server and
the skills load.

> Installing with `go install` puts the binary in `$GOBIN`/`$GOPATH/bin`. Claude Code
> launched from Spotlight or the Dock inherits a minimal PATH that usually does not
> include it. Either install via Homebrew, or run [`install.sh`](install.sh), which
> symlinks the binary into `/opt/homebrew/bin` or `/usr/local/bin`.

Verify:

```bash
ion-mem version
ion-mem status
```

---

## Quickstart

Once the plugin is installed, the first Claude Code session wires itself up:

| When | What happens |
|------|--------------|
| Session start | The `SessionStart` hook opens a session in the store, runs `ion-mem doctor` and warns if search is degraded. |
| After compaction | The `compact` hook replays the project's memory back into the fresh context window. |
| Every prompt | The `UserPromptSubmit` hook loads the 19 `ion_*` tools and injects the memory protocol — what to save, when, and under which topic key. |
| Session stop | The `Stop` hook closes the session. |

You do not call the tools by hand. The agent saves a decision when a decision is
made, and searches memory before re-deriving something.

Inspect what it stored:

```bash
ion-mem dash
```

The TUI shows projects, observations, detail and config views, with an inline search
bar and an embeddings backfill progress bar. A bare `ion-mem` on a TTY opens the same
dashboard.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate list |
| `/` | Open search bar |
| `Enter` | Open detail / run action |
| `c` | Open config view (from projects) |
| `d` | Soft-delete selected observation |
| `Esc` | Back / cancel |
| `q` | Quit |

Scriptable search:

```bash
ion-mem search "auth-service architecture" --project=my-project
```

---

## Commands

Every command accepts `--data-dir` to point at a store other than the default
`~/.ion-mem`. Flags must come **before** a positional query or file argument.

| Command | Purpose | Most useful flags |
|---------|---------|-------------------|
| `ion-mem` | Bare invocation on a TTY opens the dashboard. | — |
| `backfill-embeddings` | Embed every observation that has no vector row yet (needs Ollama). | `--project`, `--batch` (50), `--verbose` |
| `backup` | Compact backup of the whole store via `VACUUM INTO`. | `--out` (default `<data-dir>/backups/ion-mem-<ts>.db`) |
| `config` | Read and write settings: `get <key>`, `set <key> <value>`, `list`. | — |
| `context` | Print a markdown context summary for one project. | `--project` (required), `--scope` (`project`) |
| `dash` | Open the interactive TUI dashboard (needs a terminal). | — |
| `doctor` | Diagnose whether the configured `search.mode` is satisfiable right now. | `--json`, `--autostart`, `--wait` (5s), `--timeout` (8s) |
| `eval` | Run search-quality evaluation against a golden query set. | `--golden` (required), `--corpus`, `--mode`, `--k` (5), `--json=<path>` (a path, `-` for stdout — unlike `search`'s boolean `--json`) |
| `export` | Dump the whole store to JSONL files plus a manifest. | `--out` (default `<data-dir>/export-<ts>/`) |
| `export-project` | Export one project's memory to a portable `.ionmem.zip`. | `--project`, `--out`, `--with-prompts`, `--include-deleted`, `--allow-secrets` |
| `help` | Show usage. | — |
| `import-project <file>` | Import a `.ionmem.zip` bundle. Dry-run unless `--apply`. | `--apply`, `--prefer-bundle`, `--with-prompts`, `--project` |
| `mcp` | Start the MCP stdio server (what agents connect to). | `--profile` (`agent`\|`all`), `--project` (or `ION_MEM_PROJECT`) |
| `project rename <old> <new>` | Rename a project across the store. Dry-run unless `--apply`. | `--apply` |
| `prune` | Delete old prompts and hard-delete aged soft-deleted rows. Dry-run unless `--apply`. | `--apply`, `--prompt-days` (90), `--deleted-days` (30) |
| `save-prompt` | Record a user prompt for a session. | `--session-id` (required), `--content` (required), `--project` |
| `search <query>` | One-shot search. | `--project`, `--all-projects`, `--limit` (10), `--type`, `--json` |
| `session-end` | Mark a session ended. | `--id` (or `--all-stale`), `--summary`, `--all-stale`, `--older-than` (24h) |
| `session-start` | Create a session in the store. | `--id`, `--project`, `--cwd` (all required) |
| `status` | One-shot health snapshot: stats, recent items, alerts. | `--limit` (5) |
| `version` | Print the ion-mem version. | — |

`eval` takes a larger flag set for benchmarking: `--all-projects`,
`--vector-weight` (2.0), `--fusion` (`all`\|`strict`), `--embed-timeout` (10s),
`--ollama-url`, `--model`, plus the deprecated `--embeddings` (use `--mode hybrid`).

---

## MCP tools

The `ion-mem mcp` server exposes 19 tools. Agents call them; you normally do not.

| Tool | What it does |
|------|--------------|
| `ion_context` | Markdown summary of recent sessions and observations for the current project. |
| `ion_current_project` | Detect the active project from the working directory or a supplied path. |
| `ion_delete` | Delete an observation by ID — soft by default, `hard=true` for permanent. |
| `ion_export_project` | Export one project's memory to a portable `.ionmem.zip`. Read-only; refuses to write when the secret scan trips. |
| `ion_get_observation` | Fetch a single observation by ID, full and untruncated. |
| `ion_history` | Revision history for an observation — what it looked like before each overwrite. |
| `ion_import_project` | Import a `.ionmem.zip` bundle. Dry-run by default; never deletes; backs up before applying. |
| `ion_save_prompt` | Record a user prompt for durable context capture (deduped on session + content). |
| `ion_save` | Save an observation. Handles `topic_key` upsert, dedup and prompt capture. |
| `ion_search` | Search saved observations. Reports `mode`, `effective_mode`, `degraded` and `degraded_reason`. |
| `ion_session_end` | End an active session. |
| `ion_session_start` | Start or reuse a named session. Duplicate IDs are idempotent. |
| `ion_session_summary` | Save a session summary as an observation and end the session. |
| `ion_set_status` | Change lifecycle status: `active`, `superseded` or `obsolete`. Enforces the supersession rules. |
| `ion_stats` | Aggregate counts for the whole store, with per-project breakdowns. |
| `ion_suggest_topic_key` | Suggest a `family/specific-description` topic key. Pure function, no store access. |
| `ion_timeline` | Chronological window of observations and prompts from the same session. |
| `ion_undelete` | Recover a soft-deleted observation. |
| `ion_update` | Partially update an observation — only the fields you supply change. |

Every tool returns a structured envelope. Missing IDs, ambiguous projects and empty
results come back **inside** the result, never as a protocol error, so an agent can
always reason about the response.

---

## Skills

Four guided workflows ship with the Claude Code plugin. Each one stops for your
review before writing anything.

| Skill | What it does | Safety |
|-------|--------------|--------|
| `/ion-mem:init` | Seeds memory on an existing codebase. Subagents map the stack, docs, ADRs, git history and CI, then extract architecture, decisions, conventions and known debt — every candidate carrying `file:line`, a commit hash or a doc heading as evidence. | Backs up the store first. Prints the full candidate table and stops for confirmation. Writes only under `seed/<family>/<slug>` keys with a `Source: seeded by /ion-mem:init` marker, so it can never overwrite a hand-written observation. Capped at 40 saves per run. Rebuild mode soft-deletes only seeded rows, requires typing the project name verbatim, and logs every deleted id. |
| `/ion-mem:curate` | Finds drift: dead evidence (cited paths that no longer exist), contradictions, duplicates, and "fixed again" chains among bug fixes. | **Only ever changes `status`.** Never deletes, never edits content, never touches another project's rows. Backs up first, prints the full proposal table grouped by detector, and stops for confirmation. Anything uncertain is marked `REVIEW` and left alone. Undo with `ion_set_status(id, status: "active")`. |
| `/ion-mem:export` | Packages one project's memory into a `.ionmem.zip` for a teammate. | Never writes to the store — nothing to undo. Runs the secret scan **without** `allow_secrets` and stops with the findings if it trips. Asks before including prompts or soft-deleted rows, and checks the output file is gitignored. |
| `/ion-mem:import` | Merges a teammate's bundle into your local store. | Always dry-runs first and prints inserted / skipped / updated / conflicts / unresolved supersedes / status downgrades. Stops loudly if the bundle's project differs from yours. Backs up the store before applying, and reports the backup path. |

A fifth skill, `/ion-mem:memory`, is always active: it is the protocol that tells the
agent what to save and when.

---

## Search modes

`search.mode` controls how `ion_search` and `ion-mem search` retrieve results.

| Mode | Behavior |
|------|----------|
| `vector` **(default)** | Embeds the query and ranks by cosine similarity only — no BM25. Falls back to lexical (`degraded: true`) when embeddings are off or Ollama is unavailable. |
| `lexical` | Weighted BM25 with recency decay (title 8×, topic key 6×, type 3×, content 1×; 30-day half-life). Retries with an OR fallback and sets `fuzzy: true` when the all-terms query matches nothing. Never touches embeddings. |
| `hybrid` | AND-only BM25 fused with vector search via Reciprocal Rank Fusion — the noisy OR fallback is never run. Degrades to lexical the same way `vector` does. |

```bash
ion-mem config set search.mode vector   # or: lexical, hybrid
```

**Why vector is the default.** Benchmarked on a real mixed Spanish/English corpus
(2026-09-26): BM25 alone scored **0.42 MRR**, production hybrid **0.75**, and
vector-only with `bge-m3` **0.90**. The reason is blunt — 38 of 42 natural-language
queries fell into BM25's OR fallback, which matches on any single term and runs
roughly 5× slower than one vector search. A strict hybrid variant (fusing BM25 only
on an exact AND match) reached 0.91, edging out vector-only by 0.01 MRR at
meaningfully higher latency and complexity. Not worth it. `hybrid` — now always
strict/AND-only in production — stays available for workloads that also want exact
term recall.

Every search reports `mode`, `effective_mode`, `degraded` and `degraded_reason`, so a
silent fallback to lexical is visible in the response itself.

### `ion-mem doctor`

Checks whether the configured mode is actually satisfiable right now:

```bash
ion-mem doctor --json --autostart --timeout 5s
```

It reports `search_mode`, `effective_mode`, `embeddings_enabled`, Ollama
reachability, model presence, a probe embed call (dimensions + latency), embedding
coverage, and a verdict: **`ok`** (exit 0), **`degraded`** (exit 1 — searches are
silently falling back to lexical), or **`down`** (exit 2 — the store is unreadable).
`hints` gives the exact fix command. Set `ollama.autostart true` and `--autostart`
launches a local Ollama server when it is configured but unreachable.

---

## Embeddings with Ollama

Without Ollama everything still works — lexical only. To get semantic search:

```bash
ollama pull bge-m3                    # multilingual, recommended
# ollama pull nomic-embed-text        # English-only, lighter
ion-mem config set embeddings.enabled true
ion-mem backfill-embeddings --verbose
```

`backfill-embeddings` embeds only the observations that lack a vector row, in
batches of 50 (`--batch`), so it is safe to re-run and cheap to resume. `--verbose`
turns on DEBUG-level job logging for the run; `log.verbose true` turns it on
permanently.

The same flow is available in the TUI: `ion-mem dash`, press `c` for Config, toggle
**EMBEDDINGS ON**, run **TEST CONNECTION**, then **EMBED MISSING** for an
incremental backfill with a live progress bar (or **REGENERATE EMBEDDINGS** to
re-embed everything).

---

## Observation status and curation

Memory hygiene means changing state, not deleting. "We chose X, then moved to Y
because Z" is more useful than a store that only remembers Y.

| Status | Meaning |
|--------|---------|
| `active` (default) | Current guidance. |
| `superseded` | Replaced by a newer observation (`superseded_by` points at it), kept as searchable history. |
| `obsolete` | No longer relevant. Still never hard-deleted by status alone. |

Search does not hide superseded or obsolete rows — it **ranks them lower**. Their
score is shrunk by 0.5× (superseded) or 0.25× (obsolete) after normal scoring, so an
equally relevant active row always wins. Results carry `status`, `superseded_by` and
`status_reason`, and a superseded row also gets a `note` pointing at its replacement,
so the agent sees the lineage without a second call. Pass `include_superseded: false`
to restrict to active rows only.

```json
{"id": 42, "status": "superseded", "superseded_by": 108, "reason": "moved to LRU eviction after perf regression"}
```

`superseded_by` must point at an `active` observation in the **same** project — the
chain always ends at the newest active row, so pointing at an already-superseded row
is refused, and loops are rejected.

**`bugfix` and `discovery` observations are permanent.** They are the project's
learning record: when something breaks again, that row is where the agent looks
first. `status=obsolete` is refused outright for them, and `status=superseded` is
only accepted when `superseded_by` also points at a `bugfix`/`discovery` — a newer
fix replacing an older one, never a demotion. `ion-mem prune` never hard-deletes
them either, even after a human soft-deletes them, and reports how many it skipped.

Saving under the **same** `topic_key` upserts in place — that is the normal
supersession path. Use `ion_set_status` when a new decision under a **different**
topic key replaces an older one. `/ion-mem:curate` automates finding those cases.

---

## Sharing memory between teammates

Export one project's memory to a single file, hand it over however you like, and
your teammate imports it from their own checkout. No shared database, no server, no
account.

```bash
# Exporting machine, from inside the project:
ion-mem export-project --project=my-project --out=my-project-2026-09-27.ionmem.zip

# Teammate's machine, from their checkout of the SAME repo:
ion-mem import-project my-project-2026-09-27.ionmem.zip           # dry-run: prints, writes nothing
ion-mem import-project my-project-2026-09-27.ionmem.zip --apply   # backs up first, then applies
```

Agents get the same two operations as `ion_export_project` and `ion_import_project`;
`/ion-mem:export` and `/ion-mem:import` drive them with the checks a hand-rolled call
skips.

The format is built around four rules:

- **Never overwrites a teammate's rows.** Observations match on `sync_id` (a globally
  unique id, not the local row number). The local row wins every conflict unless you
  pass `--prefer-bundle` — and even then the replaced row is preserved as a revision.
- **Never deletes.** Import inserts new rows or, opt-in, updates existing ones. It
  never removes a row that exists locally.
- **Prompts are excluded by default.** They are the likeliest place to leak
  something. Both sides require an explicit `--with-prompts`.
- **Secrets are scanned before export.** AWS keys, `sk-…` API keys, GitHub/GitLab
  tokens, private key blocks, JWTs and non-placeholder `password=` / `secret=`
  assignments all trigger a refusal — export prints the findings and exits non-zero
  unless you pass `--allow-secrets`.

Every `--apply` import takes a full `ion-mem backup` first, so a bad import is one
restore away from undone.

---

## Configuration

```bash
ion-mem config list
ion-mem config get search.mode
ion-mem config set search.mode hybrid
```

| Key | Default | Meaning |
|-----|---------|---------|
| `embeddings.enabled` | `false` | Turn on embedding generation and vector search. |
| `embeddings.ollama_url` | `http://localhost:11434` | Ollama base URL. |
| `embeddings.model` | `bge-m3` | Embedding model name. |
| `retention.prompt_days` | `90` | Age after which `ion-mem prune` deletes user prompts. |
| `retention.deleted_days` | `30` | Age after which `ion-mem prune` hard-deletes soft-deleted observations. |
| `log.verbose` | `false` | DEBUG-level job logging. |
| `search.mode` | `vector` | `lexical`, `vector` or `hybrid`. |
| `ollama.autostart` | `false` | Let `ion-mem doctor --autostart` launch a local Ollama server when it is unreachable. |

`search.mode` and `ollama.autostart` are validated against a closed set — a typo is
rejected at `set` time, not discovered later at query time.

---

## Data & privacy

Everything lives on your machine, under `~/.ion-mem` (override with `--data-dir`):

- A single SQLite database with FTS5 full-text indexing.
- Vector rows, only if you enable embeddings.
- Backups in `~/.ion-mem/backups/`, written by `ion-mem backup` and automatically
  before any import that writes.

**Nothing leaves the machine.** There is no telemetry, no account, no sync service.
Embeddings, when enabled, are generated by an Ollama server you run yourself — by
default on `localhost`. The only way memory travels is when you explicitly run
`export-project` and hand someone the file, and that path scans for secrets and
excludes prompts before it writes.

Retention is yours to set: `ion-mem prune` (dry-run by default) removes prompts older
than `retention.prompt_days` and hard-deletes observations soft-deleted longer ago
than `retention.deleted_days`, while refusing to touch permanent `bugfix`/`discovery`
rows.

---

## Development

Requires Go 1.25+.

```bash
make build   # go build ./...
make test    # go test ./...
make lint    # go vet ./...
make fmt     # gofmt check — exits non-zero on drift
make help    # list targets
```

CI runs build, `go test -race ./...`, `go vet` and a gofmt check on every push and
pull request.

The version string is injected at build time:

```bash
go build -ldflags "-X main.version=$(git describe --tags --always --dirty)" ./cmd/ion-mem
```

Without ldflags the binary falls back to the module version in build info, then to
`"dev"`.

Database migrations are numbered SQL files applied automatically on store open. Add
new ones as the next number; never edit an existing migration.

### Release process

1. Land everything on `main` and make sure CI is green.
2. Tag and push:

   ```bash
   git tag -a v0.5.0 -m "v0.5.0"
   git push origin v0.5.0
   ```

3. The `release` workflow runs GoReleaser, which builds darwin/linux binaries for
   amd64 and arm64, publishes the GitHub Release with checksums and a grouped
   changelog, and commits the updated formula to
   [`arodriguezp2003/homebrew-tap`](https://github.com/arodriguezp2003/homebrew-tap).

See [RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md) for the one-time setup.

---

## License

MIT. See [LICENSE](LICENSE).

Author: Alejandro Rodriguez.
