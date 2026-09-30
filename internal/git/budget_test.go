package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func tempRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644)
	return dir
}

func TestBudgetFreshThenCachedFallback(t *testing.T) {
	repo, cache := tempRepo(t), t.TempDir()

	// Plenty of time: fresh counts, and they get cached.
	s := ReadBudget(repo, 30*time.Second, cache, func(string) { t.Error("no refresh needed") })
	if s == nil || s.Partial || s.Untracked != 1 || s.Branch != "main" {
		t.Fatalf("fresh read = %+v", s)
	}

	// Blow the budget: last known counts come back instantly, marked Partial,
	// and a background refresh is requested.
	var refreshed string
	start := time.Now()
	s = ReadBudget(repo, time.Nanosecond, cache, func(root string) { refreshed = root })
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("over-budget read took %v", time.Since(start))
	}
	if !s.Partial || s.Untracked != 1 || refreshed == "" {
		t.Errorf("over-budget read = %+v (refresh %q)", s, refreshed)
	}

	// The refresher updates the cache for the next prompt.
	os.WriteFile(filepath.Join(repo, "another.txt"), []byte("y"), 0o644)
	if err := Refresh(cache, refreshed); err != nil {
		t.Fatal(err)
	}
	if c := LoadCache(cache, repo); c == nil || c.Untracked != 2 {
		t.Errorf("cache after refresh = %+v", c)
	}
}
