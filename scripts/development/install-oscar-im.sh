#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INSTALL_DIR="$HOME/.local/bin"

usage() {
  cat <<'USAGE'
Usage: install-oscar-im.sh [--install-dir DIR] [--help]

Builds the oscar-im CLI from this checkout and installs it in ~/.local/bin.
--install-dir is intended for isolated installs and testing.
USAGE
}

while (($#)); do
  case "$1" in
    --install-dir)
      [[ $# -ge 2 ]] || { echo "--install-dir requires a directory" >&2; exit 2; }
      INSTALL_DIR="$2"
      shift 2
      ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "${SHELL##*/}" in
  bash) PROFILE="$HOME/.bashrc" ;;
  zsh) PROFILE="$HOME/.zshrc" ;;
  *) PROFILE="$HOME/.profile" ;;
esac

mkdir -p "$INSTALL_DIR"
BUILD_OUTPUT="$(mktemp "$INSTALL_DIR/.oscar-im.XXXXXX")"
trap 'rm -f "$BUILD_OUTPUT"' EXIT
(cd "$REPO_ROOT/src" && go build -o "$BUILD_OUTPUT" ./cmd/oscar-im)
install -m 0755 "$BUILD_OUTPUT" "$INSTALL_DIR/oscar-im"

PATH_LINE='export PATH="$HOME/.local/bin:$PATH"'
if [[ "$INSTALL_DIR" == "$HOME/.local/bin" ]]; then
  touch "$PROFILE"
  if ! grep -Fqx "$PATH_LINE" "$PROFILE"; then
    printf '\n%s\n' "$PATH_LINE" >> "$PROFILE"
  fi
fi

echo "Installed oscar-im at $INSTALL_DIR/oscar-im"
if [[ "$INSTALL_DIR" == "$HOME/.local/bin" ]]; then
  echo "PATH entry is in $PROFILE. Run 'source $PROFILE' or open a new shell to use oscar-im."
else
  echo "Add $INSTALL_DIR to PATH in your shell configuration to use oscar-im."
fi
