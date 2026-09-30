package main

import (
	"archive/tar"
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// pkgInfo is one package's entry in a pacman database.
type pkgInfo struct {
	Name, Version, File, SHA256, Base string
	Licenses, Depends, Provides       []string
}

// readDatabase reads a pacman sync database: a zstd-compressed tar with a
// desc file per package.
func readDatabase(r io.Reader) ([]pkgInfo, error) {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	var out []pkgInfo
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the package database: %w", err)
		}
		if path.Base(h.Name) != "desc" {
			continue
		}
		p, err := parseDesc(tr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.Name, err)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the package database lists no packages")
	}
	return out, nil
}

// parseDesc reads a desc file: %FIELD% lines, each followed by its values
// one per line, and a blank line.
func parseDesc(r io.Reader) (pkgInfo, error) {
	fields := map[string][]string{}
	var cur string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
			cur = ""
		case strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%") && len(line) > 2:
			cur = strings.Trim(line, "%")
		case cur != "":
			fields[cur] = append(fields[cur], line)
		}
	}
	if err := sc.Err(); err != nil {
		return pkgInfo{}, err
	}
	one := func(k string) string {
		if v := fields[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	p := pkgInfo{
		Name: one("NAME"), Version: one("VERSION"), File: one("FILENAME"),
		SHA256: one("SHA256SUM"), Base: one("BASE"),
		Depends: fields["DEPENDS"], Provides: fields["PROVIDES"],
	}
	for _, l := range fields["LICENSE"] {
		p.Licenses = append(p.Licenses, strings.TrimPrefix(l, "spdx:"))
	}
	if p.Base == "" {
		p.Base = p.Name
	}
	if p.Name == "" || p.Version == "" || p.File == "" || p.SHA256 == "" {
		return pkgInfo{}, fmt.Errorf("missing NAME, VERSION, FILENAME or SHA256SUM")
	}
	return p, nil
}

// depName strips a version constraint: "gmp>=5.0" is "gmp".
func depName(dep string) string {
	if i := strings.IndexAny(dep, "<>="); i >= 0 {
		return dep[:i]
	}
	return dep
}

// resolve returns roots and everything they depend on, sorted by name. A
// dependency can name a package or something packages provide ("sh" is
// provided by bash); when several packages provide it and none of them is
// already chosen, it's an error, so the choice is made in packages.txt
// rather than by accident.
func resolve(db []pkgInfo, roots []string) ([]pkgInfo, error) {
	byName := map[string]pkgInfo{}
	providers := map[string][]string{}
	for _, p := range db {
		byName[p.Name] = p
		for _, prov := range p.Provides {
			n := depName(prov)
			providers[n] = append(providers[n], p.Name)
		}
	}
	chosen := map[string]pkgInfo{}
	queue := append([]string(nil), roots...)
	for _, r := range roots {
		if _, ok := byName[r]; !ok {
			return nil, fmt.Errorf("packages.txt: MSYS2 has no package %q", r)
		}
	}
	for len(queue) > 0 {
		dep := depName(queue[0])
		queue = queue[1:]
		name, err := pick(dep, byName, providers, chosen)
		if err != nil {
			return nil, err
		}
		if _, ok := chosen[name]; ok {
			continue
		}
		p := byName[name]
		chosen[name] = p
		queue = append(queue, p.Depends...)
	}
	out := make([]pkgInfo, 0, len(chosen))
	for _, p := range chosen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func pick(dep string, byName map[string]pkgInfo, providers map[string][]string, chosen map[string]pkgInfo) (string, error) {
	if _, ok := byName[dep]; ok {
		return dep, nil
	}
	cands := providers[dep]
	for _, c := range cands {
		if _, ok := chosen[c]; ok {
			return c, nil
		}
	}
	switch len(cands) {
	case 0:
		return "", fmt.Errorf("nothing in MSYS2 provides %q", dep)
	case 1:
		return cands[0], nil
	}
	return "", fmt.Errorf("%q is provided by %s; add the one to use to packages.txt", dep, strings.Join(cands, ", "))
}

// readRoots reads packages.txt: package names, one per line, with #
// comments.
func readRoots(file string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			roots = append(roots, line)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("%s lists no packages", file)
	}
	return roots, nil
}
