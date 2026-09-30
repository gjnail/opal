package main

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const bashDesc = `%FILENAME%
bash-5.3.020-1-x86_64.pkg.tar.zst

%NAME%
bash

%BASE%
bash

%VERSION%
5.3.020-1

%SHA256SUM%
dd601817a00a48024a58ed087e1aec786301d929f3d373ec5c1861dd76fe3029

%LICENSE%
spdx:GPL-3.0-or-later

%PROVIDES%
sh

%MAKEDEPENDS%
gcc
`

func TestParseDesc(t *testing.T) {
	p, err := parseDesc(strings.NewReader(bashDesc))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "bash" || p.Version != "5.3.020-1" || p.File != "bash-5.3.020-1-x86_64.pkg.tar.zst" || p.Base != "bash" {
		t.Fatalf("got %+v", p)
	}
	if len(p.Licenses) != 1 || p.Licenses[0] != "GPL-3.0-or-later" {
		t.Fatalf("licenses: %q", p.Licenses)
	}
	if len(p.Provides) != 1 || p.Provides[0] != "sh" || len(p.Depends) != 0 {
		t.Fatalf("provides %q, depends %q", p.Provides, p.Depends)
	}
	if _, err := parseDesc(strings.NewReader("%NAME%\nbash\n")); err == nil {
		t.Fatal("a desc without a file and checksum was accepted")
	}
}

func TestResolve(t *testing.T) {
	db := []pkgInfo{
		{Name: "bash", Provides: []string{"sh"}},
		{Name: "dash", Provides: []string{"sh"}},
		{Name: "grep", Depends: []string{"libintl>=0.20", "sh"}},
		{Name: "libintl", Depends: []string{"libiconv"}},
		{Name: "libiconv"},
		{Name: "unused"},
	}
	got, err := resolve(db, []string{"bash", "grep"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Name)
	}
	// sh comes from bash, which was asked for, not dash.
	if strings.Join(names, " ") != "bash grep libiconv libintl" {
		t.Fatalf("got %v", names)
	}

	// With no provider chosen, two candidates are an error.
	if _, err := resolve(db, []string{"grep"}); err == nil || !strings.Contains(err.Error(), "packages.txt") {
		t.Fatalf("ambiguous sh: %v", err)
	}
	if _, err := resolve(db, []string{"nope"}); err == nil {
		t.Fatal("an unknown root was accepted")
	}
	if _, err := resolve([]pkgInfo{{Name: "a", Depends: []string{"missing"}}}, []string{"a"}); err == nil {
		t.Fatal("a missing dependency was accepted")
	}
}

func TestLockRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "packages.lock")
	pkgs := []pkgInfo{{
		Name: "libintl", Version: "0.22.5-1", File: "libintl-0.22.5-1-x86_64.pkg.tar.zst",
		SHA256: strings.Repeat("ab", 32), Base: "gettext", Licenses: []string{"GPL", "LGPL"},
	}}
	if err := writeLockFile(file, pkgs); err != nil {
		t.Fatal(err)
	}
	lock, err := readLockFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := lockEntry{Name: "libintl", Version: "0.22.5-1", File: pkgs[0].File, SHA256: pkgs[0].SHA256, Base: "gettext", License: "GPL AND LGPL"}
	if len(lock) != 1 || lock[0] != want {
		t.Fatalf("got %+v", lock)
	}
	// Split packages share their base's source archive.
	if got := lock[0].sourceFile(); got != "gettext-0.22.5-1.src.tar.zst" {
		t.Fatalf("source file %s", got)
	}
}

func TestLockRejectsBadLines(t *testing.T) {
	sum := strings.Repeat("0", 64)
	for _, line := range []string{
		"bash\t5\tbash.pkg.tar.zst\t" + sum + "\tbash",                // five fields
		"bash\t5\tbash.pkg.tar.zst\tXYZ\tbash\tGPL",                   // not a SHA-256
		"bash\t5\t../../evil.pkg.tar.zst\t" + sum + "\tbash\tGPL",     // leaves the repository
		"bash\t5\thttps://x/bash.pkg.tar.zst\t" + sum + "\tbash\tGPL", // a URL
	} {
		if _, err := parseLock(line + "\n"); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}

func TestSkip(t *testing.T) {
	for name, want := range map[string]bool{
		".PKGINFO":                             true,
		"usr/bin/bash.exe":                     false,
		"usr/share/man":                        true,
		"usr/share/man/man1/bash.1.gz":         true,
		"usr/share/locale/de/LC_MESSAGES/x.mo": true,
		"usr/lib/libintl.dll.a":                true,
		"usr/lib/terminfo/78/xterm":            true,
		"usr/share/terminfo/78":                true,
		"usr/share/terminfo/78/xterm-256color": false,
		"usr/share/terminfo/61/aaa-60":         true,
		"usr/share/licenses/ncurses/LICENSE":   false,
		"usr/share/mandatory":                  false,
	} {
		if got := skip(name); got != want {
			t.Errorf("skip(%q) = %v, want %v", name, got, want)
		}
	}
}

type entry struct {
	name, body, link string
	typ              byte
}

func tarOf(t *testing.T, entries []entry) *bytes.Buffer {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Linkname: e.link, Mode: 0o644, Size: int64(len(e.body))}
		if e.typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	return &buf
}

func TestExtractLinksBecomeCopies(t *testing.T) {
	root := t.TempDir()
	err := extractTar(tarOf(t, []entry{
		{name: ".PKGINFO", body: "pkgname = gawk", typ: tar.TypeReg},
		{name: "usr/bin/", typ: tar.TypeDir},
		// A symbolic link before its target, as tar allows.
		{name: "usr/bin/awk.exe", link: "gawk.exe", typ: tar.TypeSymlink},
		{name: "usr/bin/gawk-5.exe", body: "MZ gawk", typ: tar.TypeReg},
		{name: "usr/bin/gawk.exe", link: "usr/bin/gawk-5.exe", typ: tar.TypeLink},
		{name: "usr/share/man/man1/gawk.1", body: "manual", typ: tar.TypeReg},
		{name: "usr/bin/dangling", link: "/usr/bin/nothing", typ: tar.TypeSymlink},
	}), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"usr/bin/gawk-5.exe", "usr/bin/gawk.exe", "usr/bin/awk.exe"} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || string(b) != "MZ gawk" {
			t.Errorf("%s: %q, %v", name, b, err)
		}
	}
	for _, name := range []string{".PKGINFO", "usr/share/man", "usr/bin/dangling"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); err == nil {
			t.Errorf("%s was extracted", name)
		}
	}
}

func TestExtractRefusesEscapes(t *testing.T) {
	for _, e := range []entry{
		{name: "../outside", body: "x", typ: tar.TypeReg},
		{name: "usr/../../outside", body: "x", typ: tar.TypeReg},
		{name: `usr\..\..\outside`, body: "x", typ: tar.TypeReg},
		{name: "C:/Windows/evil", body: "x", typ: tar.TypeReg},
		{name: "usr/bin/x", link: "../../../outside", typ: tar.TypeSymlink},
		{name: "usr/bin/y", link: "../../outside", typ: tar.TypeLink},
	} {
		root := filepath.Join(t.TempDir(), "root")
		if err := extractTar(tarOf(t, []entry{e}), root); err == nil {
			t.Errorf("%s -> %s was accepted", e.name, e.link)
		}
	}
}
