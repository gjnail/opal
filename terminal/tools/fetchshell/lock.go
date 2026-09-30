package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// lockEntry is one line of packages.lock.
type lockEntry struct {
	Name, Version, File, SHA256, Base, License string
}

// sourceFile is the name MSYS2 gives the package's source archive. Split
// packages (libintl comes from gettext) share their base's.
func (e lockEntry) sourceFile() string {
	return e.Base + "-" + e.Version + ".src.tar.zst"
}

const lockHeader = `# Opal Bash's packages, pinned. Written by go run ./tools/fetchshell -update
# from packages.txt; edit that and run it again rather than editing this.
# name	version	file	sha256	base	license
`

var (
	hexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// A package file name can't point anywhere but the repository.
	safeFile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~-]*\.pkg\.tar\.zst$`)
)

func writeLockFile(file string, pkgs []pkgInfo) error {
	var b strings.Builder
	b.WriteString(lockHeader)
	for _, p := range pkgs {
		lic := strings.Join(p.Licenses, " AND ")
		if lic == "" {
			lic = "unknown"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Version, p.File, p.SHA256, p.Base, lic)
	}
	return os.WriteFile(file, []byte(b.String()), 0o644)
}

func readLockFile(file string) ([]lockEntry, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return parseLock(string(b))
}

func parseLock(s string) ([]lockEntry, error) {
	var out []lockEntry
	for i, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			return nil, fmt.Errorf("packages.lock line %d: want 6 tab-separated fields, got %d", i+1, len(f))
		}
		e := lockEntry{Name: f[0], Version: f[1], File: f[2], SHA256: f[3], Base: f[4], License: f[5]}
		if !hexSHA256.MatchString(e.SHA256) {
			return nil, fmt.Errorf("packages.lock line %d: bad SHA-256 %q", i+1, e.SHA256)
		}
		if !safeFile.MatchString(e.File) {
			return nil, fmt.Errorf("packages.lock line %d: bad file name %q", i+1, e.File)
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("packages.lock lists no packages")
	}
	return out, nil
}

// lockDiff describes what -update changed, for the person running it.
func lockDiff(old []lockEntry, pkgs []pkgInfo) []string {
	was := map[string]string{}
	for _, e := range old {
		was[e.Name] = e.Version
	}
	var out []string
	for _, p := range pkgs {
		switch v, ok := was[p.Name]; {
		case !ok:
			out = append(out, fmt.Sprintf("added   %s %s", p.Name, p.Version))
		case v != p.Version:
			out = append(out, fmt.Sprintf("updated %s %s -> %s", p.Name, v, p.Version))
		}
		delete(was, p.Name)
	}
	var gone []string
	for name, v := range was {
		gone = append(gone, fmt.Sprintf("removed %s %s", name, v))
	}
	sort.Strings(gone)
	out = append(out, gone...)
	if len(out) == 0 {
		out = append(out, "no changes")
	}
	return out
}
