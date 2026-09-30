#!/bin/sh
# Builds the release archive of Opal Terminal for one platform:
#
#   scripts/package.sh <goos> <goarch> <version> [outdir]
#
# Run it from terminal/ on the system it targets: macOS and Linux builds use
# cgo, and Windows builds run fetch-conpty.ps1 (set CONPTY_DIR to a folder
# that already has conpty.dll and OpenConsole.exe to skip the download). It
# writes <outdir>/opal-terminal_<goos>_<goarch>.zip on Windows and .tar.gz
# elsewhere; outdir defaults to dist.
set -eu

if [ $# -lt 3 ]; then
	echo "usage: scripts/package.sh <goos> <goarch> <version> [outdir]" >&2
	exit 2
fi
os=$1
arch=$2
version=$3
out=${4:-dist}

name="opal-terminal_${os}_${arch}"
stage="build/$name"
rm -rf "$stage"
mkdir -p "$stage" "$out"
out=$(cd "$out" && pwd)

# Windows version resources and Info.plist want plain numbers: 1.2.3 of
# 1.2.3-rc.1.
num=$(echo "$version" | sed 's/[^0-9.].*//')
[ -n "$num" ] || num=0.0.0

cgo=1
[ "$os" = windows ] && cgo=0
ldflags="-s -w -X main.version=$version"

# The license of every Go module that goes into the binary, then the
# bundled font's. Fails if a module has no license file.
licenses() {
	echo "Opal Terminal is built with the Go modules below. Their licenses follow."
	GOOS=$os GOARCH=$arch CGO_ENABLED=$cgo go list -deps \
		-f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' . |
		sort -u |
		while read -r path ver dir; do
			# The opal CLI's packages come from this repository; see LICENSE.
			[ "$path" = opal ] && continue
			lic=$(ls "$dir" | grep -iE '^(licen[cs]e|copying)' | head -1)
			if [ -z "$lic" ]; then
				echo "package.sh: $path has no license file" >&2
				exit 1
			fi
			printf '\n%s\n%s %s\n%s\n\n' "========================================================================" "$path" "$ver" "========================================================================"
			cat "$dir/$lic"
		done
	printf '\n%s\n%s\n%s\n\n' "========================================================================" "Symbols Nerd Font Mono (bundled)" "========================================================================"
	cat internal/fonts/embedded/SymbolsNerdFont-LICENSE.txt
}
licenses > "$stage/THIRD-PARTY-LICENSES.txt"
cp README.md "$stage/README.md"
cp ../LICENSE "$stage/LICENSE"

case $os in
windows)
	# go-winres writes the icon, manifest and version info to a .syso,
	# which the Go linker picks up from the package directory. go-winres
	# itself has to build for this machine, hence the empty GOOS/GOARCH.
	GOOS= GOARCH= go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json \
		--arch "$arch" --product-version "$num" --file-version "$num"
	GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
		-ldflags "$ldflags -H=windowsgui" -o "$stage/opal-terminal.exe" .
	rm -f rsrc_windows_*.syso

	# Microsoft's ConPTY passes image sequences through; the one built into
	# Windows drops them. See scripts/fetch-conpty.ps1.
	if [ -n "${CONPTY_DIR:-}" ]; then
		cp "$CONPTY_DIR/conpty.dll" "$CONPTY_DIR/OpenConsole.exe" "$stage/"
	else
		carch=$arch
		[ "$arch" = amd64 ] && carch=x64
		ps=pwsh
		command -v pwsh > /dev/null || ps=powershell
		"$ps" -NoProfile -ExecutionPolicy Bypass -File scripts/fetch-conpty.ps1 -Dest "$stage" -Arch "$carch"
	fi
	cp packaging/ConPTY-LICENSE.txt "$stage/"
	;;
darwin)
	app="$stage/Opal Terminal.app"
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
	GOOS=darwin GOARCH=$arch CGO_ENABLED=1 go build -trimpath \
		-ldflags "$ldflags" -o "$app/Contents/MacOS/opal-terminal" .
	sed "s/@VERSION@/$num/g" packaging/Info.plist > "$app/Contents/Info.plist"
	cp assets/icon.icns "$app/Contents/Resources/icon.icns"
	# An ad-hoc signature, not a Developer ID one: it seals the bundle, and
	# Apple Silicon won't run unsigned code at all.
	codesign --force --sign - "$app"
	;;
linux)
	GOOS=linux GOARCH=$arch CGO_ENABLED=1 go build -trimpath \
		-ldflags "$ldflags" -o "$stage/opal-terminal" .
	cp packaging/opal-terminal.desktop "$stage/"
	cp assets/icon-256.png "$stage/opal-terminal.png"
	;;
*)
	echo "package.sh: unsupported system $os" >&2
	exit 2
	;;
esac

if [ "$os" = windows ]; then
	archive="$out/$name.zip"
	rm -f "$archive"
	if command -v zip > /dev/null; then
		(cd build && zip -qr "$archive" "$name")
	elif command -v 7z > /dev/null; then
		(cd build && 7z a -tzip -bso0 "$archive" "$name")
	else
		# Windows 10 and later include bsdtar, which writes zip files too.
		# (Compress-Archive in Windows PowerShell stores backslashes in the
		# entry names.)
		wintar="$(cygpath -u "$SYSTEMROOT")/System32/tar.exe"
		(cd build && "$wintar" -a -cf "$(cygpath -w "$archive")" "$name")
	fi
else
	archive="$out/$name.tar.gz"
	# COPYFILE_DISABLE keeps macOS tar from adding ._ metadata files.
	COPYFILE_DISABLE=1 tar -C build -czf "$archive" "$name"
fi
echo "$archive"
