#!/bin/sh
# opal installer for macOS, Linux, WSL, Termux and Git Bash.
#
#   From a checkout (builds with Go):  ./install/install.sh
#   From a release:                    curl -fsSL https://raw.githubusercontent.com/gjnail/opal/main/install/install.sh | sh
#
# Installs to ~/.local/bin (override with OPAL_INSTALL_DIR), then runs
# `opal setup` to hook every shell it finds. Undo with: opal setup --remove
set -eu

REPO="${OPAL_REPO:-gjnail/opal}"
BIN_DIR="${OPAL_INSTALL_DIR:-$HOME/.local/bin}"
say() { printf 'opal: %s\n' "$*"; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW* | MSYS* | CYGWIN*) os=windows ;;
  *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
exe=opal
[ "$os" = windows ] && exe=opal.exe
if [ "$os" = windows ]; then
  say "On Windows, install/install.ps1 also adds opal to the PATH PowerShell sees."
fi

mkdir -p "$BIN_DIR"

# Run from a clone of the repository (./install/install.sh), build it. Piped
# from curl, $0 is just "sh", so check that this really is opal's checkout.
root=""
case "$0" in
  */install.sh) root=$(cd "$(dirname "$0")/.." 2>/dev/null && pwd || echo "") ;;
esac
if [ -n "$root" ] && grep -qs '^module opal$' "$root/go.mod" && command -v go >/dev/null 2>&1; then
  say "building from $root"
  (cd "$root" && go build -trimpath -ldflags "-X 'opal/internal/cli.SourceDir=$root'" -o "$BIN_DIR/$exe" .)
else
  ext=tar.gz
  [ "$os" = windows ] && ext=zip
  name="opal_${os}_${arch}.$ext"
  base="https://github.com/$REPO/releases/latest/download"
  say "downloading $base/$name"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  fetch() {
    if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
    else wget -qO "$2" "$1"; fi
  }
  fetch "$base/$name" "$tmp/$name"
  fetch "$base/SHA256SUMS.txt" "$tmp/SHA256SUMS.txt"
  want=$(awk -v n="$name" '$2 == n || $2 == "*" n { print $1 }' "$tmp/SHA256SUMS.txt")
  if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$name" | awk '{ print $1 }')
  else got=$(shasum -a 256 "$tmp/$name" | awk '{ print $1 }'); fi
  if [ -z "$want" ] || [ "$want" != "$got" ]; then
    echo "opal: $name doesn't match SHA256SUMS.txt; not installing it" >&2
    exit 1
  fi
  if [ "$ext" = zip ]; then (cd "$tmp" && unzip -q "$name"); else tar -xzf "$tmp/$name" -C "$tmp"; fi
  find "$tmp" -name "$exe" -type f -exec cp {} "$BIN_DIR/$exe" \;
fi
chmod +x "$BIN_DIR/$exe"
say "installed $("$BIN_DIR/$exe" version) to $BIN_DIR"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say "$BIN_DIR isn't on your PATH yet; setup will reference opal by its full path." ;;
esac
if [ "${OPAL_NO_SETUP:-}" = "" ]; then
  "$BIN_DIR/$exe" setup
fi
