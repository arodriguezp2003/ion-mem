# homebrew-tap

Homebrew tap for [Alejandro Rodriguez](https://github.com/arodriguezp2003)'s tools.

## Install

```bash
brew install arodriguezp2003/tap/ion-mem
```

Or tap once and install by name afterwards:

```bash
brew tap arodriguezp2003/tap
brew install ion-mem
```

## Formulae

| Formula | Description | Source |
|---------|-------------|--------|
| `ion-mem` | Persistent memory for AI coding agents — local-first, project-scoped, agent-agnostic via MCP. | [arodriguezp2003/ion-mem](https://github.com/arodriguezp2003/ion-mem) |

## Upgrading

```bash
brew update
brew upgrade ion-mem
```

## How this tap is maintained

Formulae in `Formula/` are **generated** — do not hand-edit them.

Each upstream project releases with [GoReleaser](https://goreleaser.com), which
builds the release binaries and commits the updated formula here as part of the
release. To ship a new version, tag the upstream repository; this tap updates
itself.

## Issues

Report problems against the upstream project, not this tap:
<https://github.com/arodriguezp2003/ion-mem/issues>

## License

The formulae here are MIT-licensed, matching their upstream projects.
