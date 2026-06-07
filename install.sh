#!/bin/bash
# ion-mem — install script (basic v0)
#
# Builds and installs the ion-mem binary via `go install`, then symlinks the
# Claude Code plugin into ~/.claude/plugins/ion-mem. After running, restart
# Claude Code to pick up the plugin.
#
# This is the minimum-viable installer. A future SDD change will replace it
# with a proper `ion-mem setup claude-code` CLI subcommand featuring multi-OS
# support, verification, dry-run, and uninstall.
#
# Env overrides:
#   ION_MEM_PLUGIN_DEST   target plugin path (default: ~/.claude/plugins/ion-mem)
#   GOBIN / GOPATH        standard Go install destination resolution
#
# Usage:
#   ./install.sh           install
#   ./install.sh --help    print this message
#   ./install.sh --uninstall   remove binary + plugin symlink

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_DEST="${ION_MEM_PLUGIN_DEST:-$HOME/.claude/plugins/ion-mem}"

# Resolve where `go install` will place the binary.
resolve_bin_dir() {
  local gobin gopath
  gobin="$(go env GOBIN 2>/dev/null || true)"
  if [ -n "$gobin" ]; then
    echo "$gobin"
    return
  fi
  gopath="$(go env GOPATH 2>/dev/null || true)"
  if [ -n "$gopath" ]; then
    echo "${gopath%%:*}/bin"
    return
  fi
  echo "$HOME/go/bin"
}

print_help() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
}

uninstall() {
  local bin_dir
  bin_dir="$(resolve_bin_dir)"
  echo "[ion-mem install] Removing binary at $bin_dir/ion-mem"
  rm -f "$bin_dir/ion-mem"
  echo "[ion-mem install] Removing plugin symlink at $PLUGIN_DEST"
  rm -rf "$PLUGIN_DEST"
  echo "✓ ion-mem uninstalled"
}

case "${1:-}" in
  -h|--help)
    print_help
    exit 0
    ;;
  --uninstall)
    uninstall
    exit 0
    ;;
  "")
    : # fall through to install
    ;;
  *)
    echo "ion-mem install: unknown option: $1" >&2
    print_help
    exit 2
    ;;
esac

# Sanity check: go must be on PATH.
if ! command -v go >/dev/null 2>&1; then
  echo "ion-mem install: 'go' not found on PATH. Install Go 1.25+ first." >&2
  exit 1
fi

BIN_DIR="$(resolve_bin_dir)"

echo "[ion-mem install] Building binary via 'go install'..."
(cd "$REPO_ROOT" && go install ./cmd/ion-mem)
echo "[ion-mem install] Binary installed: $BIN_DIR/ion-mem"

# Symlink plugin (replacing any existing entry so reruns are idempotent).
mkdir -p "$(dirname "$PLUGIN_DEST")"
if [ -L "$PLUGIN_DEST" ] || [ -e "$PLUGIN_DEST" ]; then
  echo "[ion-mem install] Replacing existing entry at $PLUGIN_DEST"
  rm -rf "$PLUGIN_DEST"
fi
ln -s "$REPO_ROOT/plugin/claude-code" "$PLUGIN_DEST"
echo "[ion-mem install] Plugin symlinked: $PLUGIN_DEST -> $REPO_ROOT/plugin/claude-code"

# Post-install verification: invoke the binary and print the version banner.
if [ -x "$BIN_DIR/ion-mem" ]; then
  VERSION_OUT="$("$BIN_DIR/ion-mem" version 2>/dev/null || true)"
else
  VERSION_OUT=""
fi

echo
echo "✓ ion-mem installed"
echo "  Binary:   $BIN_DIR/ion-mem"
echo "  Version:  ${VERSION_OUT:-<unable to read>}"
echo "  Plugin:   $PLUGIN_DEST"
echo

if ! echo ":$PATH:" | grep -q ":$BIN_DIR:"; then
  echo "⚠ $BIN_DIR is NOT on your PATH. Add it to your shell profile, e.g.:"
  echo "    export PATH=\"$BIN_DIR:\$PATH\""
  echo
fi

# Coexistence warning: engram and ion-mem can both be installed (no hard
# conflicts — different MCP ids, tool prefixes, data dirs, etc.) but if BOTH
# plugins are ACTIVE in Claude Code their SessionStart hooks each inject their
# own Memory Protocol on every session, leading to: (a) duplicated ~150 lines
# of additionalContext per session, (b) the agent loading 27 tools instead of
# 14, (c) potential double-saves to both stores. Warn explicitly. We do NOT
# touch the engram plugin — disabling is the user's choice.
ENGRAM_PLUGIN_DIR="$(dirname "$PLUGIN_DEST")/engram"
if [ -L "$ENGRAM_PLUGIN_DIR" ] || [ -d "$ENGRAM_PLUGIN_DIR" ]; then
  echo "⚠ engram plugin detected at $ENGRAM_PLUGIN_DIR"
  echo "  Both plugins ACTIVE → duplicated Memory Protocol injection,"
  echo "  duplicated ToolSearch (27 tools), and potential double-saves."
  echo "  To deactivate engram (reversible — does NOT delete data):"
  echo "    mv \"$ENGRAM_PLUGIN_DIR\" \"${ENGRAM_PLUGIN_DIR}.disabled\""
  echo "  Then restart Claude Code."
  echo
fi

echo "Next: restart Claude Code to load the ion-mem plugin."
echo "Uninstall: ./install.sh --uninstall"
