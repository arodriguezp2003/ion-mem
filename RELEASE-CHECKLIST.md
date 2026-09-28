# Release checklist — ion-mem v0.5.0

The first release needs four one-time setup steps (public repo, tap repo, PAT,
secret). Every release after that is just a tag.

---

## One-time setup

### 0. Create the public repository

This step publishes the sanitized source you are looking at right now as a new
**public** repository. It must run before any of the secret- or push-related
steps below, since those act on `arodriguezp2003/ion-mem` (the public repo)
and it has to exist first.

The public repo must **not already exist** — `gh repo create` fails if it does.
This is a distinct repository from whatever private repo you developed in;
the private repo keeps its own name and history untouched, this only creates
the new public one from the sanitized tree in `.` (the current directory).

```bash
gh repo create arodriguezp2003/ion-mem \
  --public \
  --source=. \
  --remote=origin \
  --push
```

- [ ] `arodriguezp2003/ion-mem` exists, is public, and `origin` points at it.

### 1. Create the tap repository

The tap must be **public** and named exactly `homebrew-tap` — Homebrew derives
`arodriguezp2003/tap` from `arodriguezp2003/homebrew-tap`.

```bash
gh repo create arodriguezp2003/homebrew-tap \
  --public \
  --description "Homebrew tap for Alejandro Rodriguez's tools"
```

Seed it with `homebrew-tap/README.md` and `homebrew-tap/Formula/.gitkeep`, which
now exist in this repo:

```bash
git clone https://github.com/arodriguezp2003/homebrew-tap.git /tmp/homebrew-tap
mkdir -p /tmp/homebrew-tap/Formula
cp homebrew-tap/README.md /tmp/homebrew-tap/README.md
cp homebrew-tap/Formula/.gitkeep /tmp/homebrew-tap/Formula/.gitkeep
cd /tmp/homebrew-tap
git add . && git commit -m "chore: bootstrap tap" && git push
```

> Leave `Formula/` otherwise empty. GoReleaser writes `Formula/ion-mem.rb` on the
> first release.

- [ ] `arodriguezp2003/homebrew-tap` exists, is public, has a `main` branch with at
      least one commit.

### 2. Create a fine-grained PAT scoped to the tap only

GitHub Actions' built-in `GITHUB_TOKEN` can only write to the repository it runs
in, so pushing the formula to a *different* repo needs its own token.

1. Go to **GitHub → Settings → Developer settings → Personal access tokens →
   Fine-grained tokens → Generate new token**.
2. Name: `ion-mem goreleaser → homebrew-tap`.
3. Resource owner: `arodriguezp2003`.
4. Expiration: pick a date you will actually remember (1 year is reasonable; the
   release will fail loudly when it lapses).
5. Repository access: **Only select repositories** → `arodriguezp2003/homebrew-tap`.
   Nothing else. Not `ion-mem`.
6. Repository permissions: **Contents → Read and write**. Leave every other
   permission at *No access*.
7. Generate and copy the token — it is shown once.

- [ ] Token created, scoped to `homebrew-tap` only, with Contents: Read and write.

### 3. Add the token as a secret on the public repo

```bash
gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo arodriguezp2003/ion-mem
# paste the token when prompted
```

Or via **ion-mem → Settings → Secrets and variables → Actions → New repository
secret**, name `HOMEBREW_TAP_GITHUB_TOKEN`.

- [ ] Secret `HOMEBREW_TAP_GITHUB_TOKEN` exists on `arodriguezp2003/ion-mem`.

---

## Release

### 4. Push the code

```bash
git push origin main
```

- [ ] The **CI** workflow is green on `main` (build, `go test -race`, `go vet`,
      gofmt).

### 5. Sanity-check the release config locally (optional but cheap)

```bash
goreleaser check
goreleaser release --snapshot --clean
```

This builds all four binaries without publishing anything. Inspect `dist/`.

- [ ] `goreleaser check` reports no errors.

### 6. Tag and push

```bash
git tag -a v0.5.0 -m "v0.5.0"
git push origin v0.5.0
```

- [ ] The **Release** workflow started on the tag push.

---

## Verify

### 7. Check the GitHub Release assets

Open <https://github.com/arodriguezp2003/ion-mem/releases/tag/v0.5.0> and confirm
six assets:

- [ ] `ion-mem_0.5.0_darwin_amd64.tar.gz`
- [ ] `ion-mem_0.5.0_darwin_arm64.tar.gz`
- [ ] `ion-mem_0.5.0_linux_amd64.tar.gz`
- [ ] `ion-mem_0.5.0_linux_arm64.tar.gz`
- [ ] `checksums.txt`
- [ ] Release notes show the grouped changelog (Features / Bug fixes / …)

### 8. Check the formula commit in the tap

```bash
gh api repos/arodriguezp2003/homebrew-tap/commits --jq '.[0].commit | {message, author: .author.name}'
```

- [ ] A commit `chore(formula): update ion-mem to v0.5.0` exists, authored by
      `Alejandro Rodriguez`.
- [ ] `Formula/ion-mem.rb` exists and its `url`/`sha256` point at the v0.5.0
      assets.

### 9. Install from the tap

```bash
brew install arodriguezp2003/tap/ion-mem
ion-mem version
```

- [ ] `brew install` succeeds without a build-from-source fallback.
- [ ] `ion-mem version` prints `0.5.0` (or `v0.5.0`).
- [ ] `brew test ion-mem` passes.

### 10. Check the plugin path end to end

```bash
claude plugin marketplace add arodriguezp2003/ion-mem
claude plugin install ion-mem@ion-mem
```

Restart Claude Code, then:

- [ ] `ion-mem status` runs and reports the store.
- [ ] A new Claude Code session has the `ion_*` tools available.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| GoReleaser fails at the `brews` step with 404 or 403 | PAT missing, expired, or not scoped to `homebrew-tap` | Regenerate the token with Contents: Read and write on the tap, re-set the secret, re-run the workflow |
| GoReleaser fails with "git is in a dirty state" | Uncommitted changes when tagging | Commit or stash, delete and re-push the tag |
| Formula commit lands but `brew install` builds from source | Formula `url` points at the repo, not the release archive | Check `archives.name_template` matches what the formula expects; re-run the release |
| `main.version` prints `dev` | ldflags not applied | Confirm `.goreleaser.yaml` still has `-X main.version={{.Version}}` and the build id is the one being archived |

## Subsequent releases

Steps 4, 6, 7, 8, 9. The tap and the secret are already in place — only the tag
changes.
