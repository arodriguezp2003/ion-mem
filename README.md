# ion-mem

ion-mem is a local-first persistent memory layer for AI coding agents. It stores
observations, sessions, and prompts in a SQLite database with FTS5 full-text
indexing, exposes them through an MCP (Model Context Protocol) stdio server, and
ships a retro TUI dashboard for human inspection. Hybrid search (BM25 + optional
vector embeddings via Ollama) keeps results useful even without a cloud backend.
The system works fully offline and degrades gracefully when embeddings are
unavailable — everything lexical still works.

## Features

- **16 MCP tools** — save, update, delete, search, timeline, history, session
  lifecycle, stats, and more; all loaded automatically into Claude Code via a
  plugin hook
- **Weighted BM25 + recency decay** — title (8×), topic key (6×), type (3×),
  content (1×); 30-day half-life decay penalises stale results; fuzzy OR fallback
  fires when AND yields nothing
- **Optional semantic / hybrid search** — bge-m3 (multilingual) or
  nomic-embed-text (English) via Ollama; RRF fusion weighs vector results higher;
  graceful degradation to lexical-only without Ollama
- **Revision history** — topic-key upserts keep the last 10 revisions; retrieve
  with `ion_history`
- **Retro TUI dashboard** — projects, observations, detail, and config views;
  inline search bar; embeddings backfill with a live progress bar
- **Structured envelopes** — typed observations (architecture, decision, bugfix,
  pattern, …) with scope, project, and topic key
- **Eval harness** — YAML corpus + golden query set, three modes, MRR and P@5
  metrics

## Install

Requirements: Go 1.25+. Claude Code must be installed and on PATH.

```bash
./install.sh        # or: make install
```

The script builds the binary via `go install` with a `git describe` version
stamp, registers the repo as a Claude Code plugin marketplace, installs the
plugin, and creates a system-PATH symlink so GUI-launched Claude Code can find
the binary.

Verify the install:

```bash
ion-mem status
```

Uninstall:

```bash
./install.sh --uninstall
```

## Quickstart

1. Run `./install.sh` (or `make install`).
2. Restart Claude Code (or run `/reload-plugins` in your session).
3. Agents get the memory protocol automatically — session hooks start and stop
   sessions, the UserPromptSubmit hook loads all 16 `ion_*` tools, and the
   SKILL.md injects the save/search protocol into every new context window.

Open the TUI dashboard in a terminal:

```bash
ion-mem          # bare invocation on a TTY opens the dashboard
ion-mem dash     # explicit alias
```

One-shot search for scripting:

```bash
ion-mem search "auth-service architecture" --project=my-project
ion-mem search "JWT login" --json
```

## Embeddings (optional)

Without Ollama, everything works lexical-only. To enable semantic / hybrid search:

1. Install and start Ollama.
2. Pull a model:
   ```bash
   ollama pull bge-m3            # multilingual, recommended
   # ollama pull nomic-embed-text  # English-only, lighter
   ```
3. Open the TUI: `ion-mem`
4. Press `c` to open Config.
5. Set **EMBEDDINGS ON**, confirm the **OLLAMA URL** and **MODEL**.
6. Press Enter on **TEST CONNECTION** to verify.
7. Press Enter on **EMBED MISSING** to backfill existing observations
   incrementally (skips already-embedded rows, shows a live progress bar).

To re-embed everything from scratch, use **REGENERATE EMBEDDINGS** instead.

## TUI key reference

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate list |
| `/` | Open search bar |
| `Enter` | Open detail / run action |
| `c` | Open config view (from projects) |
| `d` | Soft-delete selected observation |
| `Esc` | Go back / cancel |
| `q` | Quit |

## CLI reference

```
ion-mem mcp                  Start the MCP stdio server (for agent integrations)
ion-mem dash                 Open the interactive TUI dashboard
ion-mem session-start        Create a session in the store
ion-mem session-end          Close a session (optional summary)
ion-mem context              Print a markdown context summary for a project
ion-mem save-prompt          Record a user prompt for a session
ion-mem search <query>       One-shot search; supports --project, --json, --limit
ion-mem status               Health snapshot: stats, recent items, alerts
ion-mem eval                 Run search quality evaluation against a golden set
ion-mem backfill-embeddings  Embed observations that lack a vector row (requires Ollama)
ion-mem version              Print the version
ion-mem help                 Show usage
```

All subcommands accept `--data-dir` to override the default `~/.ion-mem` store
location. Run `ion-mem <command> --help` for per-command flags.

## Eval harness

The eval harness measures search quality against a synthetic corpus and golden
query set. Fixtures live in `internal/eval/testdata/`.

**Corpus schema** (`corpus.yaml`):
```yaml
- title: "…"
  content: "…"
  type: "architecture|decision|pattern|…"
  topic_key: "…"   # empty string when not applicable
  age_days: 5
```

**Golden schema** (`golden.yaml`):
```yaml
- id: "Q01"
  query: "auth-service architecture"
  expected:
    - "auth-service architecture decision"  # expected result titles, in rank order
  expect_fail: false   # true for BM25 lexical-gap queries that require embeddings
  note: "…"
```

Run modes: default (BM25, in-memory corpus), `--embeddings` (hybrid, requires
Ollama), `--real-store` (your live `~/.ion-mem` store). Metrics: MRR and P@5.

```bash
ion-mem eval --golden=internal/eval/testdata/golden.yaml \
             --corpus=internal/eval/testdata/corpus.yaml
```

## Development

```bash
make build   # compile all packages
make test    # run all tests (race-clean)
make lint    # go vet ./...
make fmt     # gofmt check (exits non-zero on drift)
```

The binary version is injected at build time via
`-ldflags "-X main.version=$(git describe --tags --always --dirty)"`.
`go install` without ldflags falls back to the module version recorded in build
info, then to `"dev"`.

Database migrations are numbered SQL files applied automatically on store open.
Add new migrations as sequentially numbered files; never edit existing ones.

A cloud sync layer (multi-user, RBAC, audit) is planned as the next major slice.
