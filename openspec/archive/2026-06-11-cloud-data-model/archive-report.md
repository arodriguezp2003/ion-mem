---
ion-sdd-version: "1.0"
phase: ion-sdd-archive
generated: "2026-06-11"
change: "cloud-data-model"
verdict: "PASS WITH WARNINGS"
---

# Archive Report: cloud-data-model

## Outcome

The `cloud-data-model` change is complete, verified, and archived. It delivered
the Postgres data-model foundation for the cloud layer across two NEW repos.

| Field | Value |
|---|---|
| Verdict | PASS WITH WARNINGS (0 critical, 6 warning, 3 suggestion) |
| Tasks | 29/29 complete |
| Tests | All green vs real Postgres (testcontainers pgvector:pg17), re-verified independently |
| Delivery | size:exception single batch (user chose "todo de una") |
| Remote | None — local repos only ("locales primero, GitHub después") |

## Implementation location

These capabilities are implemented in SEPARATE repos (user decision — see the
repo-layout decision), not in this repo:

- `ion-mem-types` (`github.com/ionix/ion-mem-types`) — `~/ionix/ion-mem-types`
  - commit `b1042f0` — wire DTOs + Role enum + JSON round-trip tests
- `ion-mem-cloud` (`github.com/ionix/ion-mem-cloud`) — `~/ionix/ion-mem-cloud`
  - `b5440f4` — scaffold: go.mod, migrations 0001-0007, migrate pkg, testdb, migrate CLI
  - `ee1dbf6` — sqlc query files + generated internal/db
  - `8d05f6e` — integration tests via testcontainers
  - `b6f3da9` — fix(invites): DB-level 7-day expiry default (closed verify S-02)

## Capabilities shipped

- `cloud-persistence` — six-table Postgres schema, migrations, sqlc query layer, testcontainers harness. Main spec: `openspec/specs/cloud-persistence.md`.
- `cloud-rbac` — three-role enum + structural membership invariants (data layer). Permission enforcement deferred to service layer. Main spec: `openspec/specs/cloud-rbac.md`.

## Architecture decisions (load-bearing)

- Access layer: **sqlc** over pgx/v5 — every future cloud change writes queries through it.
- uuid v7 **app-side** (`google/uuid`) — no DB extension dependency.
- pgvector **inline nullable** column, dimension deferred to `cloud-semantic-search`.
- RBAC: **schema-shaped now, guarded later** — illegal states unrepresentable; illegal actions are the service layer's job.
- Migrations: **one up/down pair per table**, FK-dependency order.

## Resolved during cycle

- **S-02 closed**: `invites.expires_at` now has a DB-level `DEFAULT now() + interval '7 days'` with a covering test (commit `b6f3da9`). The 7-day TTL invariant is now enforced at the schema, not just the application.

## Deferred (tracked for future named changes)

| Item | Verify ref | Future change |
|---|---|---|
| Invite single-use acceptance + expiry evaluation | W-02, W-03 | `cloud-rest-api` |
| Active-member list query test | W-04 | test-quality follow-up |
| ion-mem-types imported by cloud (wire ↔ DB mapping) | W-05 | `cloud-sync-protocol` |
| sqlc native pgx mode (batch/copy) | W-06 | when service layer needs it |
| uuid v7 version-bit assertion across all table tests | W-01, S-03 | test-quality follow-up |
| Proper pgvector Go type (embedding as []byte) | apply dev #3 | `cloud-semantic-search` |
| Embedding generation + ANN index | by design | `cloud-semantic-search` |

## Roadmap (cloud layer, post this change)

1. `cloud-rest-api` — HTTP endpoints + permission enforcement
2. `cloud-auth-wire` — JWT/sessions/API keys + session-user identity
3. `cloud-sync-protocol` — local↔cloud delta push/pull + outbox + DTO mapping
4. `cloud-semantic-search` — pgvector population + embeddings + ANN index

## Notes

- Cloud capability specs are filed in THIS repo's `openspec/specs/` for SDD-cycle
  continuity (all artifacts ran from ion-memory). If the team later bootstraps
  openspec inside `ion-mem-cloud`, these specs should migrate there.
- `cloud-architecture` remains an active umbrella change (explore-only) documenting
  the four foundational decisions future cloud changes reference.
