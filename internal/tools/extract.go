package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"opal/internal/platform"
)

var archiveExts = []string{
	".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tar.lz", ".tar.lzma",
	".tgz", ".tbz2", ".tbz", ".txz", ".tzst",
	".zip", ".jar", ".war", ".whl", ".nupkg", ".vsix", ".apk", ".epub",
	".tar", ".7z", ".rar", ".gz", ".bz2", ".xz", ".zst",
}

// Extract unpacks archive into the current directory. An archive with a
// single top-level folder is extracted as that folder; otherwise the
// contents go into a new folder named after the archive. Nothing is
// overwritten and no entry can escape the destination.
func Extract(archive string) (string, int, error) {
	archive = platform.FromMSYS(archive)
	if _, err := os.Stat(archive); err != nil {
		return "", 0, err
	}
	lower := strings.ToLower(archive)
	ext := ""
	for _, e := range archiveExts {
		if strings.HasSuffix(lower, e) {
			ext = e
			break
		}
	}
	if ext == "" {
		return "", 0, fmt.Errorf("%s: unrecognized archive type", archive)
	}
	stem := filepath.Base(archive)
	stem = stem[:len(stem)-len(ext)]

	// Single compressed files (.gz, .bz2, .xz, .zst) just decompress beside it.
	switch ext {
	case ".gz", ".bz2", ".xz", ".zst":
		return decompressSingle(archive, ext, stem)
	}

	tmp, err := os.MkdirTemp(".", ".opal-extract-")
	if err != nil {
		return "", 0, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(tmp)
		}
	}()

	var n int
	switch ext {
	case ".zip", ".jar", ".war", ".whl", ".nupkg", ".vsix", ".apk", ".epub":
		n, err = unzip(archive, tmp)
	case ".tar":
		n, err = untar(archive, tmp, nil)
	case ".tar.gz", ".tgz":
		n, err = untar(archive, tmp, func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) })
	case ".tar.bz2", ".tbz2", ".tbz":
		n, err = untar(archive, tmp, func(r io.Reader) (io.Reader, error) { return bzip2.NewReader(r), nil })
	default:
		n, err = external(archive, ext, tmp)
	}
	if err != nil {
		return "", 0, err
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", 0, err
	}
	var dest string
	if len(entries) == 1 && entries[0].IsDir() {
		dest = freeName(entries[0].Name())
		err = os.Rename(filepath.Join(tmp, entries[0].Name()), dest)
	} else {
		dest = freeName(stem)
		err = os.Rename(tmp, dest)
		if err == nil {
			cleanup = false
		}
	}
	if err != nil {
		return "", 0, err
	}
	return dest, n, nil
}

// freeName returns name, or name-2, name-3... if it exists.
func freeName(name string) string {
	if _, err := os.Lstat(name); os.IsNotExist(err) {
		return name
	}
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s-%d", name, i)
		if _, err := os.Lstat(c); os.IsNotExist(err) {
			return c
		}
	}
}

// safeJoin rejects absolute paths and ../ escapes (zip-slip).
func safeJoin(root, name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing unsafe path %q", name)
	}
	return filepath.Join(root, clean), nil
}

func unzip(archive, dest string) (int, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		target, err := safeJoin(dest, f.Name)
		if err != nil {
			return n, err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return n, err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			continue // symlinks in zips are rare and a classic escape vector
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return n, err
		}
		rc, err := f.Open()
		if err != nil {
			return n, err
		}
		err = writeFile(target, rc, f.Mode().Perm())
		rc.Close()
		if err != nil {
			return n, err
		}
		os.Chtimes(target, f.Modified, f.Modified)
		n++
	}
	return n, nil
}

func untar(archive, dest string, wrap func(io.Reader) (io.Reader, error)) (int, error) {
	f, err := os.Open(archive)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var r io.Reader = f
	if wrap != nil {
		if r, err = wrap(f); err != nil {
			return 0, err
		}
	}
	tr := tar.NewReader(r)
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		target, err := safeJoin(dest, h.Name)
		if err != nil {
			return n, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return n, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return n, err
			}
			if err := writeFile(target, tr, os.FileMode(h.Mode).Perm()); err != nil {
				return n, err
			}
			os.Chtimes(target, h.ModTime, h.ModTime)
			n++
		case tar.TypeSymlink:
			// Only relative links that stay inside the destination; Windows
			// needs privileges for symlinks, so skip them there.
			if runtime.GOOS == "windows" || filepath.IsAbs(h.Linkname) {
				continue
			}
			resolved := filepath.Join(filepath.Dir(target), h.Linkname)
			if rel, err := filepath.Rel(dest, resolved); err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			os.MkdirAll(filepath.Dir(target), 0o755)
			if err := os.Symlink(h.Linkname, target); err == nil {
				n++
			}
		}
	}
}

func writeFile(path string, r io.Reader, perm os.FileMode) error {
	if perm == 0 {
		perm = 0o644
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// external hands formats Go can't read (xz, zstd, 7z, rar) to whatever tool
// this machine has. Windows 10+ ships bsdtar as tar.exe, which reads many.
func external(archive, ext, dest string) (int, error) {
	abs, _ := filepath.Abs(archive)
	var tries [][]string
	switch ext {
	case ".7z":
		tries = [][]string{{"7z", "x", "-y", "-o" + dest, abs}, {"7zz", "x", "-y", "-o" + dest, abs}, {"tar", "-xf", abs, "-C", dest}}
	case ".rar":
		tries = [][]string{{"unrar", "x", "-o-", abs, dest + string(filepath.Separator)}, {"7z", "x", "-y", "-o" + dest, abs}, {"unar", "-o", dest, abs}, {"tar", "-xf", abs, "-C", dest}}
	default: // .tar.xz, .tar.zst, .txz, .tzst, .tar.lz ...
		tries = [][]string{{"tar", "-xf", abs, "-C", dest}, {"7z", "x", "-y", "-o" + dest, abs}}
	}
	var lastErr error
	for _, t := range tries {
		if !platform.HasCommand(t[0]) {
			continue
		}
		cmd := exec.Command(t[0], t[1:]...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			lastErr = err
			continue
		}
		n := 0
		filepath.WalkDir(dest, func(_ string, d os.DirEntry, _ error) error {
			if d != nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return n, nil
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("no tool found for %s archives (install 7-Zip or a tar with %s support)", ext, strings.TrimPrefix(ext, "."))
}

func decompressSingle(archive, ext, stem string) (string, int, error) {
	dest := freeName(stem)
	switch ext {
	case ".gz", ".bz2":
		f, err := os.Open(archive)
		if err != nil {
			return "", 0, err
		}
		defer f.Close()
		var r io.Reader
		if ext == ".gz" {
			gz, err := gzip.NewReader(f)
			if err != nil {
				return "", 0, err
			}
			r = gz
		} else {
			r = bzip2.NewReader(f)
		}
		if err := writeFile(dest, r, 0o644); err != nil {
			os.Remove(dest)
			return "", 0, err
		}
		return dest, 1, nil
	}
	tool := map[string]string{".xz": "xz", ".zst": "zstd"}[ext]
	if !platform.HasCommand(tool) {
		return "", 0, errors.New("install " + tool + " to decompress " + ext + " files")
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, err
	}
	defer out.Close()
	cmd := exec.Command(tool, "-dc", archive)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(dest)
		return "", 0, err
	}
	return dest, 1, nil
}
