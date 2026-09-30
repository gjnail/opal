// Command fetchshell builds Opal Bash, the bash that comes with Opal
// Terminal on Windows: MSYS2's bash and Unix tools, laid out as an MSYS2
// root next to opal-terminal.exe, the way Git for Windows ships Git Bash.
// Run it from terminal/:
//
//	go run ./tools/fetchshell                  # write shell/ from packaging/shell/packages.lock
//	go run ./tools/fetchshell -update          # pin the newest packages again
//	go run ./tools/fetchshell -sources DIR     # download the matching source packages
//
// The lock file pins every package by SHA-256, so a build only ever unpacks
// the files that were reviewed when the lock was written. -update reads
// packages.txt, resolves what those packages depend on in MSYS2's current
// package database, and rewrites the lock.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("fetchshell: ")
	var (
		lockPath  = flag.String("lock", filepath.Join("packaging", "shell", "packages.lock"), "the pinned package list")
		rootsPath = flag.String("roots", filepath.Join("packaging", "shell", "packages.txt"), "the packages to resolve with -update")
		etcDir    = flag.String("etc", filepath.Join("packaging", "shell", "etc"), "files copied into the root's etc")
		licDir    = flag.String("licenses", filepath.Join("packaging", "shell", "licenses"), "license texts copied into the root's LICENSES")
		out       = flag.String("out", "shell", "where to write the MSYS2 root (replaced if it exists)")
		cache     = flag.String("cache", filepath.Join("build", "msys2"), "where to keep downloaded packages")
		repo      = flag.String("repo", "https://repo.msys2.org/msys", "the MSYS2 repository")
		update    = flag.Bool("update", false, "resolve -roots against MSYS2's current database and rewrite -lock")
		sources   = flag.String("sources", "", "download the source package of everything in -lock into this directory, and do nothing else")
	)
	flag.Parse()
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	f := &fetcher{repo: strings.TrimSuffix(*repo, "/"), cache: *cache}

	if *update {
		if err := runUpdate(f, *rootsPath, *lockPath); err != nil {
			log.Fatal(err)
		}
		return
	}
	lock, err := readLockFile(*lockPath)
	if err != nil {
		log.Fatal(err)
	}
	if *sources != "" {
		if err := fetchSources(f, lock, *sources); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := build(f, lock, *etcDir, *licDir, *out); err != nil {
		log.Fatal(err)
	}
}

func runUpdate(f *fetcher, rootsPath, lockPath string) error {
	roots, err := readRoots(rootsPath)
	if err != nil {
		return err
	}
	db, err := f.database()
	if err != nil {
		return err
	}
	pkgs, err := resolve(db, roots)
	if err != nil {
		return err
	}
	var old []lockEntry
	if l, err := readLockFile(lockPath); err == nil {
		old = l
	}
	if err := writeLockFile(lockPath, pkgs); err != nil {
		return err
	}
	for _, line := range lockDiff(old, pkgs) {
		fmt.Println(line)
	}
	return nil
}

// build unpacks every locked package into a fresh root, then adds Opal's
// startup files, the license texts and a list of what's inside.
func build(f *fetcher, lock []lockEntry, etcDir, licDir, out string) error {
	tmp := out + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	for _, e := range lock {
		path, err := f.pkg(e)
		if err != nil {
			return err
		}
		if err := extractPackage(path, tmp); err != nil {
			return fmt.Errorf("%s: %w", e.File, err)
		}
	}
	if err := copyTree(etcDir, filepath.Join(tmp, "etc")); err != nil {
		return err
	}
	if err := copyTree(licDir, filepath.Join(tmp, "LICENSES")); err != nil {
		return err
	}
	// /tmp is mounted on the user's temp folder (see etc/fstab), but the
	// directory has to exist for the mount point.
	if err := os.MkdirAll(filepath.Join(tmp, "tmp"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "PACKAGES.txt"), []byte(packageList(lock, f.repo)), 0o644); err != nil {
		return err
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.Rename(tmp, out); err != nil {
		return err
	}
	n, size, err := dirSize(out)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d packages, %d files, %.1f MB\n", out, len(lock), n, float64(size)/(1<<20))
	return nil
}

// packageList is PACKAGES.txt: what's in the root and where each package's
// source is, which the GPL asks us to point to.
func packageList(lock []lockEntry, repo string) string {
	var b strings.Builder
	b.WriteString(`Opal Bash is built from these MSYS2 packages (https://www.msys2.org).
The licenses they name are in the LICENSES folder, and a package's own
license file, where it has one, is in usr/share/licenses. The source of
every package is attached to the Opal release that shipped it, as
opal-terminal_shell-sources.tar, and is also at the address shown.

`)
	for _, e := range lock {
		fmt.Fprintf(&b, "%s %s\n  license: %s\n  source: %s/sources/%s\n\n", e.Name, e.Version, e.License, repo, e.sourceFile())
	}
	return b.String()
}

func fetchSources(f *fetcher, lock []lockEntry, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range lock {
		name := e.sourceFile()
		if seen[name] {
			continue
		}
		seen[name] = true
		if err := f.download(f.repo+"/sources/"+name, filepath.Join(dir, name), ""); err != nil {
			return err
		}
		fmt.Println(name)
	}
	return nil
}
