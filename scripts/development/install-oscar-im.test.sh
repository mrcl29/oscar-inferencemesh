#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
mkdir -p "$TMP_DIR/bin" "$TMP_DIR/home"
cat > "$TMP_DIR/bin/go" << 'FAKE_GO'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$PWD" "$*" > "$GO_LOG"
[[ "${FAKE_GO_FAIL:-}" != 1 ]] || exit 23
while (($#)); do
  if [[ "$1" == -o ]]; then
    printf 'fake oscar-im binary\n' > "$2"
    exit 0
  fi
  shift
done
exit 24
FAKE_GO
chmod +x "$TMP_DIR/bin/go"
export PATH="$TMP_DIR/bin:$PATH" HOME="$TMP_DIR/home" GO_LOG="$TMP_DIR/go.log"
export SHELL=/bin/bash

if "$SCRIPT_DIR/install-oscar-im.sh" > "$TMP_DIR/output"; then
    :
else
    echo "installer failed unexpectedly" >&2
    exit 1
fi
[[ "$(sed -n '1p' "$GO_LOG")" == "$SCRIPT_DIR/../../src" ]] || [[ "$(sed -n '1p' "$GO_LOG")" == "$(cd "$SCRIPT_DIR/../../src" && pwd)" ]]
grep -q -- '-o '"$TMP_DIR/home/.local/bin/.oscar-im." "$GO_LOG"
[[ "$(cat "$TMP_DIR/home/.local/bin/oscar-im")" == 'fake oscar-im binary' ]]
[[ -x "$TMP_DIR/home/.local/bin/oscar-im" ]]
grep -Fq 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.bashrc"
"$SCRIPT_DIR/install-oscar-im.sh" > /dev/null
[[ "$(grep -Fc 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.bashrc")" == 1 ]]

export SHELL=/bin/zsh
"$SCRIPT_DIR/install-oscar-im.sh" > /dev/null
grep -Fq 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.zshrc"

export SHELL=/bin/bash FAKE_GO_FAIL=1
if "$SCRIPT_DIR/install-oscar-im.sh" > /dev/null 2>&1; then
    echo "installer swallowed go build failure" >&2
    exit 1
else
    status=$?
    [[ "$status" == 23 ]]
fi
echo "installer tests passed"
