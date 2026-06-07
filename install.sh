#!/bin/bash
# ion-mem — install script (basic v0)
#
# Builds and installs the ion-mem binary via `go install`, symlinks the
# Claude Code plugin into ~/.claude/plugins/ion-mem, and (unless
# --skip-path-edit is passed) idempotently appends a PATH stanza to your
# shell config so the binary is findable in future shells. After running,
# restart Claude Code to pick up the plugin.
#
# This is the minimum-viable installer. A future SDD change will replace it
# with a proper `ion-mem setup claude-code` CLI subcommand featuring multi-OS
# support, dry-run, structured uninstall, and verification.
#
# Env overrides:
#   ION_MEM_PLUGIN_DEST   target plugin path (default: ~/.claude/plugins/ion-mem)
#   GOBIN / GOPATH        standard Go install destination resolution
#
# Usage:
#   ./install.sh                  install (writes PATH to shell rc if needed)
#   ./install.sh --skip-path-edit install but do NOT touch shell config
#   ./install.sh --help           print this message
#   ./install.sh --uninstall      remove binary + plugin + PATH stanza

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_DEST="${ION_MEM_PLUGIN_DEST:-$HOME/.claude/plugins/ion-mem}"
SKIP_PATH_EDIT=0

# Marker block used to identify and later remove the PATH stanza we inject.
# Anything between these two lines belongs to us.
PATH_MARKER_BEGIN="# >>> ion-mem install.sh >>>"
PATH_MARKER_END="# <<< ion-mem install.sh <<<"

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

# detect_shell_rc returns the path to the shell config file we will edit.
# Empty string when the shell is fish/unknown — caller falls back to printing
# the manual instruction.
detect_shell_rc() {
  local shell_name
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh)
      echo "$HOME/.zshrc"
      ;;
    bash)
      # Prefer an existing file; otherwise default to .bashrc.
      local f
      for f in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
        if [ -f "$f" ]; then echo "$f"; return; fi
      done
      echo "$HOME/.bashrc"
      ;;
    *)
      echo ""
      ;;
  esac
}

# add_path_to_shell_rc appends the PATH stanza to $rc unless already present.
# Returns 0 when the rc file ends up containing our stanza (whether we added
# it or it was already there). Prints a single-line status message.
add_path_to_shell_rc() {
  local rc="$1" bin_dir="$2"

  if [ -z "$rc" ]; then
    return 1
  fi
  if [ -f "$rc" ] && grep -qF "$PATH_MARKER_BEGIN" "$rc" 2>/dev/null; then
    echo "ℹ PATH stanza for ion-mem already present in $rc"
    return 0
  fi

  {
    echo ""
    echo "$PATH_MARKER_BEGIN"
    echo "export PATH=\"$bin_dir:\$PATH\""
    echo "$PATH_MARKER_END"
  } >> "$rc"
  echo "✓ Added $bin_dir to PATH in $rc"
}

# remove_path_from_shell_rc strips our begin..end marker block from $rc.
# No-op when the rc file is absent or our markers are not present.
remove_path_from_shell_rc() {
  local rc
  rc="$(detect_shell_rc)"
  if [ -z "$rc" ] || [ ! -f "$rc" ]; then
    return 0
  fi
  if ! grep -qF "$PATH_MARKER_BEGIN" "$rc"; then
    return 0
  fi

  local tmp
  tmp="$(mktemp)"
  awk -v b="$PATH_MARKER_BEGIN" -v e="$PATH_MARKER_END" '
    $0 == b { skip=1; next }
    skip == 1 && $0 == e { skip=0; next }
    skip == 0 { print }
  ' "$rc" > "$tmp" && mv "$tmp" "$rc"
  echo "[ion-mem install] Removed PATH stanza from $rc"
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
  remove_path_from_shell_rc
  echo "✓ ion-mem uninstalled"
}

# ── Arg parsing ──────────────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
  case "${1:-}" in
    -h|--help)
      print_help
      exit 0
      ;;
    --uninstall)
      uninstall
      exit 0
      ;;
    --skip-path-edit)
      SKIP_PATH_EDIT=1
      shift
      ;;
    "")
      shift
      ;;
    *)
      echo "ion-mem install: unknown option: $1" >&2
      print_help
      exit 2
      ;;
  esac
done

# ── Preflight ────────────────────────────────────────────────────────────────
if ! command -v go >/dev/null 2>&1; then
  echo "ion-mem install: 'go' not found on PATH. Install Go 1.25+ first." >&2
  exit 1
fi

BIN_DIR="$(resolve_bin_dir)"

# ── Install: binary ──────────────────────────────────────────────────────────
echo "[ion-mem install] Building binary via 'go install'..."
(cd "$REPO_ROOT" && go install ./cmd/ion-mem)
echo "[ion-mem install] Binary installed: $BIN_DIR/ion-mem"

# ── Install: plugin symlink (idempotent — replaces existing entry) ──────────
mkdir -p "$(dirname "$PLUGIN_DEST")"
if [ -L "$PLUGIN_DEST" ] || [ -e "$PLUGIN_DEST" ]; then
  echo "[ion-mem install] Replacing existing entry at $PLUGIN_DEST"
  rm -rf "$PLUGIN_DEST"
fi
ln -s "$REPO_ROOT/plugin/claude-code" "$PLUGIN_DEST"
echo "[ion-mem install] Plugin symlinked: $PLUGIN_DEST -> $REPO_ROOT/plugin/claude-code"

# ── Install: PATH stanza in shell rc ────────────────────────────────────────
SHELL_RC=""
PATH_ALREADY_OK=0
if echo ":$PATH:" | grep -q ":$BIN_DIR:"; then
  PATH_ALREADY_OK=1
fi

if [ "$SKIP_PATH_EDIT" -eq 1 ]; then
  if [ "$PATH_ALREADY_OK" -eq 0 ]; then
    echo "[ion-mem install] --skip-path-edit set; not modifying shell config."
    echo "  Add manually: export PATH=\"$BIN_DIR:\$PATH\""
  fi
else
  SHELL_RC="$(detect_shell_rc)"
  if [ -n "$SHELL_RC" ]; then
    add_path_to_shell_rc "$SHELL_RC" "$BIN_DIR"
  else
    echo "⚠ Could not detect shell config (\$SHELL=${SHELL:-unset}). Add manually:"
    echo "    export PATH=\"$BIN_DIR:\$PATH\""
  fi
fi

# ── Post-install verification banner ─────────────────────────────────────────
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

if [ "$PATH_ALREADY_OK" -eq 0 ] && [ "$SKIP_PATH_EDIT" -eq 0 ] && [ -n "$SHELL_RC" ]; then
  echo "Reload PATH in this shell: source \"$SHELL_RC\""
  echo "(New shells will pick it up automatically.)"
  echo
fi

# Coexistence warning: engram + ion-mem can both be installed without hard
# conflicts (different MCP ids, tool prefixes, data dirs, etc.) but if BOTH
# plugins are ACTIVE in Claude Code, their hooks each inject their own Memory
# Protocol per session — leading to ~150 lines of duplicated additionalContext,
# 27 tools loaded instead of 14, and potential double-saves to both stores.
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
