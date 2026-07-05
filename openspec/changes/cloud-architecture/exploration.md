---
ion-sdd-version: "1.0"
phase: ion-sdd-explore
generated: "2026-06-07T00:00:00Z"
mode: hybrid
change: "cloud-architecture"
topic: "Cloud architecture for ion-mem multi-developer memory sharing"
---

## Exploration: cloud-architecture

### Current State

ion-mem is a fully local system. Each developer's machine runs a SQLite database at
`~/.ion-mem/ion-mem.db` with FTS5 full-text search. The MCP server (`internal/mcp/`)
exposes 14 `ion_*` tools over stdio. Persistence is through `internal/store/` —
`Store.AddObservation` with topic-key upsert, content dedup, and soft-delete.

The schema already carries `sync_id` (a random prefixed hex ID per row), `project`, `scope`,
`topic_key`, and `deleted_at`. These fields were clearly designed with future sync in mind —
there is no synchronization code yet (`internal/cloud/` and `internal/sync/` contain only
package doc comments).

No user identity, no auth, no network layer exists today.

### Affected Areas (when cloud layer is built)

- `internal/store/` — schema extensions for `author_id`, server-side `updated_at` vector,
  possibly `status` column for quality tiers; `sync_id` already present.
- `internal/sync/` — placeholder; will hold delta push/pull logic.
- `internal/cloud/` — placeholder; will hold server-side handlers and project/user model.
- `internal/server/` — placeholder; HTTP/API layer.
- `cmd/ion-mem/cli.go` — new subcommands (`sync`, `cloud-login`, etc.).
- `go.mod` — new dependencies (HTTP router, auth library, possibly Postgres driver or
  pgvector client).

---

### Q1. Data Location / Operating Mode

**Recommendation: Option C — Hybrid (local read cache + cloud-primary writes)**

The strongest counter-argument for A (local-primary + sync): offline-first is
genuinely valuable for devs on spotty connections, and ion-mem's usage pattern (AI agent
calling tools) is bursty and read-heavy. CRDT complexity is the stopper — observations
are long-form prose with topic-key semantics; last-writer-wins on the *whole doc* is
enough, and SQLite's `updated_at` column already tracks this.

The strongest counter-argument for B (cloud-primary): simplest consistency and easiest
RBAC, but one infra outage silences every AI agent mid-session. The MCP server has no
retry loop today; failing tool calls degrade Claude Code sessions silently.

**Why C wins for Ionix**: writes go to the cloud immediately (authoritative), local SQLite
caches reads so agents can respond at disk speed. Offline: reads still work (stale cache),
writes queue locally and flush on reconnect (a simple outbox table). This is the
`updated_at`-based "last write wins" that already exists in `topicKeyUpsert` — the cloud
just becomes the arbiter.

| Dimension | A (Local-primary) | B (Cloud-primary) | C (Hybrid) — recommended |
|---|---|---|---|
| Offline reads | Full | None | Full (cache) |
| Offline writes | Full (merge on reconnect) | None | Queued (outbox) |
| Consistency | Eventually consistent | Strong | Eventually consistent |
| Conflict model | CRDT or manual merge | None | Last-writer-wins per topic_key |
| RBAC enforcement | Hard (client-side) | Easy | Easy (server validates writes) |
| Infra cost | Lowest | Medium | Medium |
| Implementation effort | High (CRDT) | Low | Medium |

**Comparable systems**: Linear uses local-first with optimistic writes + background sync;
Notion is cloud-primary with aggressive caching; Obsidian Sync is hybrid (vault on disk,
delta sync to cloud, LWW conflict resolution — closest to what ion-mem needs).

---

### Q2. Quality Gating — How Does User B Trust User A's Memories?

**Recommendation: Type-discriminant + soft audit_log (no new mechanism)**

The memory corpus for Ionix will be small (tens of devs, thousands of observations per
project, not millions). Over-engineering quality gating now adds schema complexity that
blocks the cloud MVP.

**Why type-discriminant wins**: The `type` column (`decision`, `discovery`, `bugfix`,
`pattern`, `config`, `preference`) already encodes quality intent. A `decision` carries
more weight than a `preference`; agents can filter on type at query time. Combined with
`scope` (`project` vs `personal`), the quality signal is already there.

**Why other options lose**:

- **Tag/promote (draft → canonical)**: Requires an editor role and workflow discipline
  that small teams rarely maintain. Memories age out of `draft` indefinitely.
- **Voting**: Meaningful at scale (Stack Overflow). Useless for 5-dev teams where nobody
  votes.
- **Auto-quality heuristics**: Content length and file refs can be faked; trust scores
  create a two-tier system that new devs game.
- **Blind trust + audit_log + soft-delete**: The current approach — sufficient for now,
  but audit_log should be added at the server layer (who wrote what, when) for accountability
  without blocking reads.

**Read-time filtering vs write-time gating**: Default to **read-time filtering**. Cloud
returns everything; clients filter by `type` and `scope`. Write-time gating (refusing to
store) would break the AI agent UX — agents must not be surprised by silent write failures.
Soft-delete + server-side audit_log means a project owner can prune bad observations after
the fact.

| Approach | Effort | When it pays off |
|---|---|---|
| Type-discriminant (recommended) | Zero (exists) | Any team size |
| Audit_log (server-side) | Low (one table) | Accountability, >5 devs |
| Tag/promote | Medium | Teams with a dedicated curator |
| Voting | High | >50 active contributors |
| Auto-quality heuristics | High | ML pipeline in place |

---

### Q3. Embedded Vectors — Semantic Search in the Cloud?

**Recommendation: Stay BM25-only for cloud-architecture. Design the schema to be
vector-ready. Add pgvector as a separate change.**

**Why not now**: The FTS5 BM25 search already satisfies the core use case — technical
decisions, bug fixes, and configurations are best retrieved by keyword, not semantic
similarity. A dev searching for "N+1 query fix" expects exact-term recall, not "similar
database problems." False positives from semantic search on short technical snippets are
costly in an agent context (the agent acts on retrieved memory; garbage in, garbage out).

**The strongest counter-argument**: Multi-session memory drift. A dev saves "fixed the
auth race condition" in session 1. In session 10 they search for "concurrency bug in
middleware" — BM25 misses it, a vector search would hit it. This is a real gap for
long-running projects (>6 months, >500 observations).

**Vector-readiness without adding cost now**: Add a nullable `embedding BLOB` column to
the observations table (or a separate `observation_vectors` table in Postgres). Leave it
NULL for all existing rows. When the vector change ships, backfill. The storage overhead
is zero until embeddings are generated.

**If/when vectors are added**:

- **Model**: `text-embedding-3-small` (OpenAI) — 1536 dims, $0.02/1M tokens, good
  precision on English technical text. Cohere embed-v3 is a reasonable alternative.
  Local sentence-transformers (`all-MiniLM-L6-v2`) work well but require a GPU or
  CPU-heavy sidecar — not worth the ops complexity for Ionix scale.
- **Where**: Server-side embedding generation on write. Clients do not embed. This
  centralizes the API key and rate limiting.
- **Storage**: `pgvector` extension on Postgres. The cloud layer likely moves from SQLite
  to Postgres anyway — pgvector is a zero-friction addition.

| Option | Recall gain | Cost | Ops complexity | Recommended? |
|---|---|---|---|---|
| BM25 only (current) | Baseline | Zero | None | Yes (now) |
| BM25 + pgvector hybrid | High | ~$5/mo for Ionix scale | Low (Postgres ext) | Future change |
| Local sentence-transformers | High | Zero API cost | High (sidecar) | No |

---

### Q4. Repo Layout

**Recommendation: Monorepo subfolder (`ion-memory/server/`)**

At this stage, schema and types co-evolve. The `store.Observation` struct, `sync_id`
field, and migration machinery are all in `internal/store/` — the server must import them
directly. A separate repo forces either duplication of these types into a shared module
(painful, Go module versioning), or the server importing the client module (tight coupling
via semver).

**Why a separate repo would eventually make sense** (counter-argument): When the team
scales to dedicated backend engineers, a separate repo provides independent CI, independent
versioning, and forces a clean API boundary. For Ionix now (single team building both
sides), the coordination overhead outweighs the boundary clarity.

**Monorepo layout**:

```
ion-memory/
  cmd/
    ion-mem/          # existing CLI + MCP stdio
    ion-mem-server/   # new: HTTP server binary
  internal/
    store/            # existing SQLite layer (shared)
    mcp/              # existing MCP tools
    sync/             # new: delta push/pull (used by both client and server)
    cloud/            # new: project/user model, server handlers
    server/           # new: HTTP router, middleware, auth
```

This keeps a single `go.mod`, shared `internal/store` types, and one CI pipeline.

| Factor | Monorepo subfolder | Separate repo |
|---|---|---|
| Type sharing (store.Observation) | Free (same module) | Requires shared module or duplication |
| Schema migration coordination | Trivial (same module) | Cross-repo versioning |
| CI | Single pipeline | Two pipelines |
| Ownership boundary | Implicit | Explicit |
| Repo clone size | Larger | Smaller |
| When to switch | Never if team < 10 | When dedicated backend team exists |

---

### Recommendation Summary

| Question | Recommendation |
|---|---|
| Q1 Data location | **Hybrid** — local cache reads, cloud-primary writes, outbox for offline queuing |
| Q2 Quality gating | **Type-discriminant + server audit_log** — no new quality mechanism |
| Q3 Vectors | **BM25-only now** — schema-prepare for pgvector, add as separate change |
| Q4 Repo layout | **Monorepo subfolder** — `cmd/ion-mem-server/` + `internal/server/` |

### Risks

- `sync_id` generation currently uses `crypto/rand` on the client. In a multi-writer cloud
  scenario, UUID v7 (time-ordered) would be preferable for sortable conflict detection.
  Schema migration needed before cloud write path ships.
- No user/identity model exists anywhere in the codebase. `author_id` must be added to
  `observations` before multi-user writes land — cannot retrofit after data exists without
  a nullable default.
- `internal/server/` and `internal/cloud/` are doc-comment stubs only. No interfaces,
  no contracts. The propose phase must define the server API surface before design begins.
- The outbox (offline write queue) for hybrid mode adds local schema complexity. If Ionix
  devs are always online (office LAN), a simpler pure-cloud-write with a 3-second timeout
  + error surface may be sufficient — this assumption must be validated with the team.
- Postgres is implied for the cloud store (pgvector, RBAC, user tables), but not confirmed.
  The cloud layer may need to support SQLite for low-cost single-server deploys. Clarify
  before design phase.

### Ready for Proposal

Yes. The four blocking questions are answered with concrete recommendations. The propose
phase should define: user/project model, authentication mechanism (simple API key vs OAuth),
cloud store backend (Postgres vs SQLite), and the server API surface (REST vs gRPC).
