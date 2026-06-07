#!/bin/bash
# ion-mem — install script (basic v0)
#
# Installs ion-mem into Claude Code via the proper plugin marketplace
# mechanism, idempotently. Steps performed (unless flagged otherwise):
#   1. Build and install the binary via `go install` ($GOBIN / $GOPATH/bin).
#   2. Append a PATH stanza to the shell rc so the binary is findable in
#      future shells (skip with --skip-path-edit).
#   3. Register this repo as a Claude Code marketplace via
#      `claude plugin marketplace add`.
#   4. Install the `ion-mem` plugin from that marketplace.
#   5. Detect a coexisting engram install and surface the impact.
# After running, restart Claude Code (or /reload-plugins) to pick everything up.
#
# A future SDD pass will replace this with a proper `ion-mem setup
# claude-code` CLI subcommand featuring multi-OS support, dry-run, structured
# uninstall, fish-shell support, and end-to-end verification.
#
# Env overrides:
#   GOBIN / GOPATH   standard Go install destination resolution
#
# Usage:
#   ./install.sh                  install everything
#   ./install.sh --skip-path-edit install but do NOT touch shell config
#   ./install.sh --help           print this message
#   ./install.sh --uninstall      remove plugin + marketplace + binary + PATH

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
SKIP_PATH_EDIT=0

# Marker block used to identify and later remove the PATH stanza we inject.
PATH_MARKER_BEGIN="# >>> ion-mem install.sh >>>"
PATH_MARKER_END="# <<< ion-mem install.sh <<<"

# Legacy symlink path used by earlier (broken) versions of this script.
# Cleaned up on every install/uninstall so re-runs don't carry stale state.
LEGACY_PLUGIN_SYMLINK="$HOME/.claude/plugins/ion-mem"

# ─── helpers ────────────────────────────────────────────────────────────────

resolve_bin_dir() {
  local gobin gopath
  gobin="$(go env GOBIN 2>/dev/null || true)"
  if [ -n "$gobin" ]; then echo "$gobin"; return; fi
  gopath="$(go env GOPATH 2>/dev/null || true)"
  if [ -n "$gopath" ]; then echo "${gopath%%:*}/bin"; return; fi
  echo "$HOME/go/bin"
}

detect_shell_rc() {
  local shell_name
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh)
      echo "$HOME/.zshrc"
      ;;
    bash)
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

add_path_to_shell_rc() {
  local rc="$1" bin_dir="$2"
  if [ -z "$rc" ]; then return 1; fi
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

remove_path_from_shell_rc() {
  local rc
  rc="$(detect_shell_rc)"
  if [ -z "$rc" ] || [ ! -f "$rc" ]; then return 0; fi
  if ! grep -qF "$PATH_MARKER_BEGIN" "$rc"; then return 0; fi
  local tmp
  tmp="$(mktemp)"
  awk -v b="$PATH_MARKER_BEGIN" -v e="$PATH_MARKER_END" '
    $0 == b { skip=1; next }
    skip == 1 && $0 == e { skip=0; next }
    skip == 0 { print }
  ' "$rc" > "$tmp" && mv "$tmp" "$rc"
  echo "[ion-mem install] Removed PATH stanza from $rc"
}

claude_available() {
  command -v claude >/dev/null 2>&1
}

# True (exit 0) when the ion-mem marketplace is already registered with claude.
marketplace_registered() {
  claude plugin marketplace list 2>/dev/null | grep -qE '^\s*ion-mem(\s|$)'
}

# True (exit 0) when the ion-mem plugin is already installed.
plugin_installed() {
  claude plugin list 2>/dev/null | grep -qE 'ion-mem@ion-mem'
}

print_help() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
}

# ─── uninstall ──────────────────────────────────────────────────────────────

uninstall() {
  local bin_dir
  bin_dir="$(resolve_bin_dir)"

  if claude_available; then
    if plugin_installed; then
      echo "[ion-mem install] Uninstalling plugin via claude plugin uninstall"
      claude plugin uninstall ion-mem@ion-mem 2>&1 || true
    fi
    if marketplace_registered; then
      echo "[ion-mem install] Removing marketplace via claude plugin marketplace remove"
      claude plugin marketplace remove ion-mem 2>&1 || true
    fi
  else
    echo "[ion-mem install] ⚠ 'claude' CLI not found — skipping plugin/marketplace removal"
  fi

  echo "[ion-mem install] Removing binary at $bin_dir/ion-mem"
  rm -f "$bin_dir/ion-mem"

  if [ -L "$LEGACY_PLUGIN_SYMLINK" ] || [ -e "$LEGACY_PLUGIN_SYMLINK" ]; then
    echo "[ion-mem install] Removing legacy plugin symlink at $LEGACY_PLUGIN_SYMLINK"
    rm -rf "$LEGACY_PLUGIN_SYMLINK"
  fi

  remove_path_from_shell_rc
  echo "✓ ion-mem uninstalled"
}

# ─── arg parsing ────────────────────────────────────────────────────────────

while [ $# -gt 0 ]; do
  case "${1:-}" in
    -h|--help)         print_help; exit 0 ;;
    --uninstall)       uninstall; exit 0 ;;
    --skip-path-edit)  SKIP_PATH_EDIT=1; shift ;;
    "")                shift ;;
    *)                 echo "ion-mem install: unknown option: $1" >&2; print_help; exit 2 ;;
  esac
done

# ─── preflight ──────────────────────────────────────────────────────────────

if ! command -v go >/dev/null 2>&1; then
  echo "ion-mem install: 'go' not found on PATH. Install Go 1.25+ first." >&2
  exit 1
fi
if ! claude_available; then
  echo "ion-mem install: 'claude' CLI not found on PATH. Install Claude Code first." >&2
  exit 1
fi

BIN_DIR="$(resolve_bin_dir)"

# ─── 1. Build binary ────────────────────────────────────────────────────────

echo "[ion-mem install] Building binary via 'go install'..."
(cd "$REPO_ROOT" && go install ./cmd/ion-mem)
echo "[ion-mem install] Binary installed: $BIN_DIR/ion-mem"

# ─── 2. PATH stanza ─────────────────────────────────────────────────────────

SHELL_RC=""
PATH_ALREADY_OK=0
echo ":$PATH:" | grep -q ":$BIN_DIR:" && PATH_ALREADY_OK=1

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

# ─── 3. Clean up legacy symlink (from broken pre-marketplace install.sh) ────

if [ -L "$LEGACY_PLUGIN_SYMLINK" ] || [ -e "$LEGACY_PLUGIN_SYMLINK" ]; then
  echo "[ion-mem install] Removing stale legacy symlink at $LEGACY_PLUGIN_SYMLINK"
  rm -rf "$LEGACY_PLUGIN_SYMLINK"
fi

# ─── 4. Register marketplace + install plugin ──────────────────────────────

if marketplace_registered; then
  echo "[ion-mem install] Marketplace 'ion-mem' already registered — refreshing"
  claude plugin marketplace update ion-mem 2>&1 | sed 's/^/  /' || true
else
  echo "[ion-mem install] Registering marketplace from $REPO_ROOT"
  claude plugin marketplace add "$REPO_ROOT" 2>&1 | sed 's/^/  /'
fi

if plugin_installed; then
  echo "[ion-mem install] Plugin 'ion-mem' already installed — refreshing"
  claude plugin update ion-mem 2>&1 | sed 's/^/  /' || true
else
  echo "[ion-mem install] Installing plugin 'ion-mem'"
  claude plugin install ion-mem 2>&1 | sed 's/^/  /'
fi

# ─── 5. Verification banner ─────────────────────────────────────────────────

if [ -x "$BIN_DIR/ion-mem" ]; then
  VERSION_OUT="$("$BIN_DIR/ion-mem" version 2>/dev/null || true)"
else
  VERSION_OUT=""
fi

echo
echo "✓ ion-mem installed"
echo "  Binary:    $BIN_DIR/ion-mem"
echo "  Version:   ${VERSION_OUT:-<unable to read>}"
echo "  Marketplace: ion-mem (from $REPO_ROOT)"
echo

if [ "$PATH_ALREADY_OK" -eq 0 ] && [ "$SKIP_PATH_EDIT" -eq 0 ] && [ -n "$SHELL_RC" ]; then
  echo "Reload PATH in this shell: source \"$SHELL_RC\""
  echo "(New shells will pick it up automatically.)"
  echo
fi

# ─── 6. Coexistence check (engram via the actual plugin cache path) ────────

ENGRAM_CACHE_DIR="$HOME/.claude/plugins/cache/engram"
if [ -d "$ENGRAM_CACHE_DIR" ]; then
  echo "⚠ engram plugin still installed at $ENGRAM_CACHE_DIR"
  echo "  Both plugins ACTIVE → duplicated Memory Protocol injection, duplicated"
  echo "  ToolSearch (27 tools), and potential double-saves to engram + ion-mem."
  echo "  Disable (reversible, keeps engram's data + plugin files):"
  echo "      claude plugin disable engram@engram"
  echo "  Uninstall (full removal of plugin, keeps ~/.engram/ data):"
  echo "      claude plugin uninstall engram@engram"
  echo
fi

echo "Next: restart Claude Code (or run /reload-plugins in your session) to load ion-mem."
echo "Uninstall: ./install.sh --uninstall"
