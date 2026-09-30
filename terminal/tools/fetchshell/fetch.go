package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type fetcher struct {
	repo  string // e.g. https://repo.msys2.org/msys
	cache string
}

var client = &http.Client{Timeout: 10 * time.Minute}

// pkg returns the path of a locked package in the cache, downloading it if
// needed. A cached file is checked again, so a corrupt one is replaced
// rather than unpacked.
func (f *fetcher) pkg(e lockEntry) (string, error) {
	path := filepath.Join(f.cache, e.File)
	if sum, err := fileSHA256(path); err == nil && sum == e.SHA256 {
		return path, nil
	}
	if err := f.download(f.repo+"/x86_64/"+e.File, path, e.SHA256); err != nil {
		return "", err
	}
	return path, nil
}

// database downloads MSYS2's current package database. It isn't cached:
// -update is for picking up new versions.
func (f *fetcher) database() ([]pkgInfo, error) {
	path := filepath.Join(f.cache, "msys.db")
	if err := f.download(f.repo+"/x86_64/msys.db", path, ""); err != nil {
		return nil, err
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readDatabase(r)
}

// download writes url to path. With want set, the file has to have that
// SHA-256 or it's deleted and the download fails.
func (f *fetcher) download(url, path, want string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), resp.Body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); want != "" && got != want {
		os.Remove(tmp)
		return fmt.Errorf("%s: SHA-256 is %s, the lock file says %s", url, got, want)
	}
	return os.Rename(tmp, path)
}

func fileSHA256(path string) (string, error) {
	r, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
