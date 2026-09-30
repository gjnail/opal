package main

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// skippedDirs hold manuals, translations, and what's only needed to compile
// software against the libraries. ncurses installs its terminal database
// twice; usr/share/terminfo is the copy it reads.
var skippedDirs = []string{
	"usr/share/man", "usr/share/info", "usr/share/doc", "usr/share/locale",
	"usr/include", "usr/lib/pkgconfig", "usr/share/pkgconfig", "usr/share/aclocal",
	"usr/lib/terminfo",
}

// terminals are the terminal descriptions kept from ncurses' database of
// thousands: Opal Terminal sets TERM=xterm-256color, and the rest cover
// what people set TERM to by hand.
var terminals = []string{"xterm", "vt100", "vt102", "vt220", "ansi", "dumb", "cygwin", "linux", "screen", "tmux", "ms-terminal"}

// skip reports whether a file from a package is left out of Opal Bash.
func skip(name string) bool {
	if strings.HasPrefix(name, ".") { // .PKGINFO, .BUILDINFO, .MTREE, .INSTALL
		return true
	}
	for _, dir := range skippedDirs {
		if name == dir || strings.HasPrefix(name, dir+"/") {
			return true
		}
	}
	// usr/share/terminfo/78/xterm-256color: folders are named by the hex
	// code of the first letter, so case-insensitive file systems work.
	if rest, ok := strings.CutPrefix(name, "usr/share/terminfo/"); ok {
		_, entry, ok := strings.Cut(rest, "/")
		if !ok {
			return true // a letter's folder; created along with what's kept
		}
		for _, t := range terminals {
			if strings.HasPrefix(entry, t) {
				return false
			}
		}
		return true
	}
	return strings.HasSuffix(name, ".a") || strings.HasSuffix(name, ".la")
}

// extractPackage unpacks a .pkg.tar.zst into root. Windows has no
// symbolic links an MSYS2 program can rely on, so links become copies,
// as MSYS2's own installer makes them by default.
func extractPackage(file, root string) error {
	r, err := os.Open(file)
	if err != nil {
		return err
	}
	defer r.Close()
	zr, err := zstd.NewReader(r)
	if err != nil {
		return err
	}
	defer zr.Close()
	return extractTar(zr, root)
}

func extractTar(r io.Reader, root string) error {
	type link struct{ name, target string }
	var symlinks []link
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name, ok := cleanName(h.Name)
		if !ok {
			return fmt.Errorf("unsafe path %q", h.Name)
		}
		if name == "" || skip(name) {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(dst, tr); err != nil {
				return err
			}
		case tar.TypeLink:
			// A hard link names a file earlier in the same archive.
			target, ok := cleanName(h.Linkname)
			if !ok {
				return fmt.Errorf("%s: unsafe link target %q", name, h.Linkname)
			}
			if skip(target) {
				continue
			}
			if err := copyFile(filepath.Join(root, filepath.FromSlash(target)), dst); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		case tar.TypeSymlink:
			// Relative to the link's folder, or to the root when absolute.
			t := h.Linkname
			if !strings.HasPrefix(t, "/") {
				t = path.Join(path.Dir(name), t)
			}
			target, ok := cleanName(t)
			if !ok {
				return fmt.Errorf("%s: unsafe link target %q", name, h.Linkname)
			}
			symlinks = append(symlinks, link{name, target})
		}
	}
	// Symbolic links can point at files later in the archive, so they're
	// copied last. One whose target was left out is left out too.
	for _, l := range symlinks {
		src := filepath.Join(root, filepath.FromSlash(l.target))
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyTree(src, filepath.Join(root, filepath.FromSlash(l.name))); err != nil {
			return fmt.Errorf("%s: %w", l.name, err)
		}
	}
	return nil
}

// cleanName turns an archive path into a relative slash path inside the
// root. ok is false for anything that would escape it.
func cleanName(name string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	clean := path.Clean("/" + name)[1:]
	if strings.Contains(name, `\`) || strings.Contains(name, ":") {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false
		}
	}
	return clean, true
}

func writeFile(dst string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func copyFile(src, dst string) error {
	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()
	return writeFile(dst, r)
}

// copyTree copies a file, or a directory and everything in it.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		return copyFile(p, out)
	})
}

func dirSize(root string) (files int, size int64, err error) {
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		size += info.Size()
		return nil
	})
	return files, size, err
}
