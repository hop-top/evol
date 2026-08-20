#!/bin/sh
# evol source installer. There is no packaged release yet; this builds
# the engine and every reference adapter from source.
#
#   curl -fsSL https://raw.githubusercontent.com/hop-top/evol/main/scripts/install.sh | sh
#
# Requirements: git, go. Override the target dir with EVOL_INSTALL_DIR
# (default: ~/.local/bin). Binaries: `evol` plus one
# `evol-adapter-<name>` per adapter — the names evol.example.yaml wires.
set -eu

: "${EVOL_INSTALL_DIR:=$HOME/.local/bin}"
REPO="https://github.com/hop-top/evol"

for tool in git go; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "install.sh: $tool is required" >&2
        exit 1
    }
done

src=""
if [ -f go.mod ] && grep -q '^module hop\.top/evol$' go.mod; then
    src="$(pwd)" # already inside a checkout
else
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT INT TERM
    echo "install.sh: cloning $REPO"
    git clone --quiet --depth 1 "$REPO" "$tmp/evol"
    src="$tmp/evol"
fi

mkdir -p "$EVOL_INSTALL_DIR"
cd "$src"

echo "install.sh: building evol"
GOFLAGS=-buildvcs=false go build -o "$EVOL_INSTALL_DIR/evol" .

for dir in adapters/*/; do
    name="$(basename "$dir")"
    echo "install.sh: building evol-adapter-$name"
    GOFLAGS=-buildvcs=false go build -o "$EVOL_INSTALL_DIR/evol-adapter-$name" "./$dir"
done

echo "install.sh: done — binaries in $EVOL_INSTALL_DIR"
case ":$PATH:" in
*":$EVOL_INSTALL_DIR:"*) ;;
*) echo "install.sh: note — $EVOL_INSTALL_DIR is not on your PATH" >&2 ;;
esac
