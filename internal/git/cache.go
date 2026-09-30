package git

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// cached is what survives between prompts for one repository.
type cached struct {
	Branch    string
	Upstream  bool
	Ahead     int
	Behind    int
	Staged    int
	Modified  int
	Untracked int
	Conflicts int
	Stash     int
	At        time.Time
}

func cacheFile(cacheDir, root string) string {
	key := filepath.Clean(root)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	h := fnv.New64a()
	h.Write([]byte(key))
	return filepath.Join(cacheDir, fmt.Sprintf("git-%016x.json", h.Sum64()))
}

// SaveCache records s for its repository.
func SaveCache(cacheDir string, s *Status) {
	if cacheDir == "" || s == nil || s.Root == "" {
		return
	}
	c := cached{s.Branch, s.Upstream, s.Ahead, s.Behind, s.Staged, s.Modified, s.Untracked, s.Conflicts, s.Stash, time.Now()}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	if os.MkdirAll(cacheDir, 0o755) != nil {
		return
	}
	file := cacheFile(cacheDir, s.Root)
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if os.WriteFile(tmp, b, 0o644) == nil && os.Rename(tmp, file) != nil {
		os.Remove(tmp)
	}
}

// LoadCache returns the last recorded status for root, if any.
func LoadCache(cacheDir, root string) *Status {
	if cacheDir == "" {
		return nil
	}
	b, err := os.ReadFile(cacheFile(cacheDir, root))
	if err != nil {
		return nil
	}
	var c cached
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &Status{Root: root, Branch: c.Branch, Upstream: c.Upstream, Ahead: c.Ahead, Behind: c.Behind,
		Staged: c.Staged, Modified: c.Modified, Untracked: c.Untracked, Conflicts: c.Conflicts, Stash: c.Stash}
}

// Refresh computes a full status for root, with no budget, and caches it. A
// lock file keeps several prompts from piling up refreshes of one repo.
func Refresh(cacheDir, root string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	lock := cacheFile(cacheDir, root) + ".lock"
	if fi, err := os.Stat(lock); err == nil && time.Since(fi.ModTime()) < 2*time.Minute {
		return nil // someone is already on it
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	f.Close()
	defer os.Remove(lock)
	s := Read(root, 2*time.Minute)
	if s == nil || s.Partial {
		return fmt.Errorf("git status failed in %s", root)
	}
	SaveCache(cacheDir, s)
	return nil
}
