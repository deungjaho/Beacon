#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
PREFIX="${BEACON_PREFIX:-$HOME/.local}"
DEST="$PREFIX/lib/beacon"

install -d "$DEST/bin" "$DEST/hooks" "$DEST/lib" "$DEST/tmux" \
  "$DEST/skills/beacon-clear" "$PREFIX/bin"
install -m 0755 "$ROOT/bin/beacon" "$DEST/bin/beacon"
install -m 0755 "$ROOT/hooks/"*.sh "$DEST/hooks/"
install -m 0755 "$ROOT/lib/"*.sh "$DEST/lib/"
install -m 0755 "$ROOT/tmux/"*.sh "$DEST/tmux/"
install -m 0644 "$ROOT/skills/beacon-clear/SKILL.md" "$DEST/skills/beacon-clear/SKILL.md"
ln -sfn "$DEST/bin/beacon" "$PREFIX/bin/beacon"

"$PREFIX/bin/beacon" doctor
printf 'installed Beacon at %s\n' "$DEST"
