#!/bin/sh
# opal installer for macOS, Linux, WSL, Termux and Git Bash.
#
#   From a checkout (builds with Go):  ./install/install.sh
#   From a release:                    curl -fsSL https://raw.githubusercontent.com/gjnail/opal/main/install/install.sh | sh
#
# Installs to ~/.local/bin (override with OPAL_INSTALL_DIR), then runs
# `opal setup` to hook every shell it finds. Undo with: opal setup --remove
#
# It also installs Opal Terminal, the terminal app: on macOS to
# ~/Applications/Opal Terminal.app, on Linux desktops next to opal with a
# menu entry. OPAL_NO_TERMINAL=1 skips it. On Linux it's skipped without a
# graphical session, and on WSL and Termux; OPAL_WITH_TERMINAL=1 installs it
# anyway.
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
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
base="https://github.com/$REPO/releases/latest/download"

fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  else wget -qO "$2" "$1"; fi
}

# download <name> <checksum file>: fetches a file from the latest release
# into $tmp and checks it against the release's checksum file.
download() {
  say "downloading $base/$1"
  fetch "$base/$1" "$tmp/$1" || return 1
  fetch "$base/$2" "$tmp/$2" || return 1
  want=$(awk -v n="$1" '$2 == n || $2 == "*" n { print $1 }' "$tmp/$2")
  if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$1" | awk '{ print $1 }')
  else got=$(shasum -a 256 "$tmp/$1" | awk '{ print $1 }'); fi
  if [ -z "$want" ] || [ "$want" != "$got" ]; then
    echo "opal: $1 doesn't match $2; not installing it" >&2
    return 1
  fi
}

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
  download "$name" SHA256SUMS.txt
  mkdir "$tmp/opal"
  if [ "$ext" = zip ]; then (cd "$tmp/opal" && unzip -q "../$name"); else tar -xzf "$tmp/$name" -C "$tmp/opal"; fi
  find "$tmp/opal" -name "$exe" -type f -exec cp {} "$BIN_DIR/$exe" \;
fi
chmod +x "$BIN_DIR/$exe"
say "installed $("$BIN_DIR/$exe" version) to $BIN_DIR"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say "$BIN_DIR isn't on your PATH yet; setup will reference opal by its full path." ;;
esac

# Whether to install Opal Terminal here, and if not, why not.
terminal_skip_reason() {
  [ -n "${OPAL_NO_TERMINAL:-}" ] && { echo "OPAL_NO_TERMINAL is set"; return; }
  [ "$os" = windows ] && { echo "on Windows, install/install.ps1 installs it"; return; }
  [ -n "${OPAL_WITH_TERMINAL:-}" ] && return
  [ "$os" = darwin ] && return
  if [ -n "${TERMUX_VERSION:-}" ] || [ -d /data/data/com.termux ]; then
    echo "it doesn't run on Android"; return
  fi
  if grep -qsi microsoft /proc/version; then
    echo "on WSL, install it on Windows with install/install.ps1"; return
  fi
  if [ -z "${DISPLAY:-}" ] && [ -z "${WAYLAND_DISPLAY:-}" ]; then
    echo "no graphical session found (OPAL_WITH_TERMINAL=1 installs it anyway)"; return
  fi
}

# Opal Terminal needs Go 1.26 and cgo to build (the opal CLI builds with
# older versions). Checked from $tmp: inside a module that asks for a newer
# Go, `go env` would download that toolchain first.
go_for_terminal() {
  command -v go >/dev/null 2>&1 || return 1
  v=$(cd "$tmp" && go env GOVERSION 2>/dev/null) || return 1
  cgo=$(cd "$tmp" && go env CGO_ENABLED 2>/dev/null) || return 1
  [ "$cgo" = 1 ] || return 1
  v=${v#go}
  major=${v%%.*}
  minor=${v#*.}
  minor=${minor%%[!0-9]*}
  case "$major$minor" in *[!0-9]* | "") return 1 ;; esac
  [ "$major" -gt 1 ] || [ "$minor" -ge 26 ]
}

# Leaves opal-terminal_<os>_<arch>/ in $tmp, built from the checkout or
# downloaded.
terminal_archive() {
  tname="opal-terminal_${os}_${arch}"
  if [ -n "$root" ] && [ -f "$root/terminal/scripts/package.sh" ] && go_for_terminal; then
    say "building Opal Terminal from $root/terminal"
    if (cd "$root/terminal" && sh scripts/package.sh "$os" "$arch" dev "$tmp" >/dev/null); then
      tar -xzf "$tmp/$tname.tar.gz" -C "$tmp" && return 0
    fi
    say "building Opal Terminal failed (see terminal/README.md for the libraries Linux needs); trying the release download"
  fi
  if ! download "$tname.tar.gz" opal-terminal_SHA256SUMS.txt; then
    say "the latest release has no $tname.tar.gz, or it couldn't be downloaded"
    return 1
  fi
  tar -xzf "$tmp/$tname.tar.gz" -C "$tmp"
}

install_terminal() {
  terminal_archive || return 1
  src="$tmp/opal-terminal_${os}_${arch}"
  if [ "$os" = darwin ]; then
    apps="$HOME/Applications"
    mkdir -p "$apps" || return 1
    rm -rf "$apps/Opal Terminal.app" && cp -R "$src/Opal Terminal.app" "$apps/" || return 1
    say "installed Opal Terminal to $apps/Opal Terminal.app; open it from Launchpad or with: opal terminal"
    return 0
  fi
  # Copy, then rename over the old binary, so a running Opal Terminal keeps
  # its file and the new one is used from the next launch.
  cp "$src/opal-terminal" "$BIN_DIR/.opal-terminal.new" && chmod +x "$BIN_DIR/.opal-terminal.new" &&
    mv -f "$BIN_DIR/.opal-terminal.new" "$BIN_DIR/opal-terminal" || return 1
  data="${XDG_DATA_HOME:-$HOME/.local/share}"
  icons="$data/icons/hicolor/256x256/apps"
  mkdir -p "$data/applications" "$icons" || return 1
  cp "$src/opal-terminal.png" "$icons/opal-terminal.png" || return 1
  # The full path: ~/.local/bin often isn't on the PATH desktop menus use.
  while IFS= read -r line; do
    case $line in
      Exec=opal-terminal) printf 'Exec="%s"\n' "$BIN_DIR/opal-terminal" ;;
      *) printf '%s\n' "$line" ;;
    esac
  done < "$src/opal-terminal.desktop" > "$data/applications/opal-terminal.desktop" || return 1
  command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database -q "$data/applications" 2>/dev/null
  say "installed Opal Terminal to $BIN_DIR/opal-terminal, with a menu entry; or run: opal terminal"
}

reason=$(terminal_skip_reason)
if [ -n "$reason" ]; then
  say "not installing Opal Terminal: $reason"
elif ! install_terminal; then
  say "Opal Terminal wasn't installed; opal itself is."
fi

if [ "${OPAL_NO_SETUP:-}" = "" ]; then
  "$BIN_DIR/$exe" setup
fi
