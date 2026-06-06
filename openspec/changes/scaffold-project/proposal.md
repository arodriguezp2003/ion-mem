# Proposal: Scaffold ion-mem Go Project

## Intent

Stand up the empty but valid Go project skeleton for ion-mem so subsequent SDD changes (store-mvp, mcp-mvp, cloud-mvp) can land code without first negotiating module path, package layout, or CI. Greenfield repo only has `.git/` and `.atl/`; this change creates the minimum scaffolding that compiles, passes `go test`, and is enforced by CI on every push.

## Why now

Strategic direction (`architecture/ion-memory-strategy`) is locked: ion-mem is an internal Ionix fork of engram, the local layer is a faithful clone, the cloud layer is the team-grade differentiator. Mirroring engram's package layout from day one keeps upstream merges cheap, and a green CI gate prevents the first real feature PR from also debating tooling.

## Scope

### In Scope

- `go.mod` pinned to Go 1.25, module path `github.com/ionix/ion-mem` (placeholder; final host/org confirmed before tagged release).
- `cmd/ion-mem/main.go` with empty `main()`.
- `internal/{store,mcp,server,cloud,tui,setup,sync,project}/doc.go` — package docs stating intent, no implementation.
- `Makefile` with `build`, `test`, `lint`, `fmt` targets.
- `.github/workflows/ci.yml` — `go build ./...`, `go test ./...`, `gofmt -l .`, `go vet ./...` on push and PR.
- `.gitignore` — Go stdlib defaults plus `.engram/`, `*.db`, `*.db-journal`, `dist/`, local env files.
- `README.md` — short: what ion-mem is, fork relationship to upstream, current status (scaffold), build/test commands.
- `LICENSE` — MIT (resolved: matches upstream engram and keeps door open to open-source later).

### Out of Scope

- Third-party Go dependencies (added by store-mvp, mcp-mvp, cloud-mvp).
- Any business logic in `internal/*` beyond `doc.go`.
- Cloud schema, RBAC, invites, sync — deferred to cloud-mvp.
- TUI, plugins, docs site, goreleaser config (see open question 4).

## Capabilities

### New Capabilities

- `project-scaffold`: repository structure, build/test entry points, CI gate, and license metadata that every future capability assumes exists.

### Modified Capabilities

None.

## Approach

Mirror engram's `cmd/` + `internal/` + `plugin/` layout so upstream diffs apply cleanly. Ship no third-party deps yet — empty `require` block in `go.mod` keeps the supply-chain surface zero until a capability actually needs a library. CI is the acceptance gate: green build + green test + clean `gofmt` is the contract that lets later changes land safely. `doc.go` per package documents intent so a future maintainer reading `internal/store/doc.go` knows the package exists for SQLite + FTS5 storage even before code arrives.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `/` (repo root) | New | `go.mod`, `Makefile`, `.gitignore`, `README.md`, `LICENSE` |
| `cmd/ion-mem/` | New | Empty binary entry point |
| `internal/*/` | New | Eight package skeletons with `doc.go` only |
| `.github/workflows/` | New | `ci.yml` build + test + fmt + vet |

## Open Questions

1. ~~License~~ — **Resolved**: MIT.
2. **Remote repo host**: GitHub Ionix org, GitLab internal, or other? Deferred — module path uses `github.com/ionix/ion-mem` as placeholder; updated via `go mod edit -module` + import rewrite before first tagged release.
3. ~~Module path~~ — **Resolved (provisional)**: `github.com/ionix/ion-mem`. Final host confirmed with open question 2.
4. **Goreleaser**: include skeleton config now or defer until first tagged release? — **Recommendation: defer**. Internal-only fork may never need it; add via a separate change when release tagging is needed.
5. **Dashboard package**: engram has `internal/cloud/dashboard` (templ + HTMX). Include `doc.go` placeholder now or wait until cloud-mvp introduces the templ dependency? — **Recommendation: include `doc.go` placeholder** since cloud-mvp will need it and stating intent now avoids drift.

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Upstream merge friction if layout drifts | Medium | Mirror engram package names exactly; deviations recorded in `architecture/ion-memory-strategy` |
| `gentle-ai refresh` overwrites `.atl/skill-registry.md` | Low | Skill registry is generated, not hand-edited; project-specific skills are documented in engram |
| License decision blocks public visibility or upstream contribution | Medium | Surface as open question 1; do not commit a placeholder license that misrepresents intent |
| Module path changes later force `go mod edit` and import rewrites across the repo | Low | Decide module path before merging this change (open question 3) |

## Rollback Plan

This change creates new files only — no existing code is modified. Rollback is `git revert` of the scaffold commit, which returns the repo to its `.git/` + `.atl/` state. No data migration, no schema rollback, no consumer impact.

## Dependencies

- None at the code level (no third-party Go deps in this change).
- Open question 1 (license) must be answered before tagging the first public release; not blocking for this change itself if scaffolding lands behind a private repo.

## Success Criteria

- [ ] `go build ./...` exits 0.
- [ ] `go test ./...` exits 0 with empty test suite.
- [ ] `gofmt -l .` produces no output.
- [ ] `go vet ./...` exits 0.
- [ ] CI workflow runs on push and PR, all four checks green.
- [ ] Package layout mirrors engram (`cmd/<binary>`, `internal/{store,mcp,server,cloud,tui,setup,sync,project}`).
- [ ] `README.md` states fork relationship, current status, and build/test commands.
- [ ] Open question 2 (final repo host) tracked separately; not blocking for this change since `go mod edit` resolves later.
