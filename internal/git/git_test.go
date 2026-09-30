package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePorcelain(t *testing.T) {
	recs := []string{
		"# branch.oid 1234567890abcdef",
		"# branch.head feature/x",
		"# branch.upstream origin/feature/x",
		"# branch.ab +3 -1",
		"# stash 2",
		"1 M. N... 100644 100644 100644 abc abc staged.go",
		"1 .M N... 100644 100644 100644 abc abc modified.go",
		"1 MM N... 100644 100644 100644 abc abc both.go",
		"2 R. N... 100644 100644 100644 abc abc R100 new.go", "old.go",
		"u UU N... 100644 100644 100644 100644 a b c conflict.go",
		"? untracked.txt",
		"? other.txt",
		"",
	}
	s := &Status{}
	parsePorcelain(s, []byte(strings.Join(recs, "\x00")))
	want := Status{Branch: "feature/x", Commit: "1234567", Upstream: true, Ahead: 3, Behind: 1, Stash: 2,
		Staged: 3, Modified: 2, Conflicts: 1, Untracked: 2}
	if *s != want {
		t.Errorf("got  %+v\nwant %+v", *s, want)
	}
}

func TestFindAndHead(t *testing.T) {
	root := t.TempDir()
	gd := filepath.Join(root, ".git")
	os.MkdirAll(filepath.Join(gd, "rebase-merge"), 0o755)
	os.WriteFile(filepath.Join(gd, "HEAD"), []byte("0123456789abcdef\n"), 0o644)
	os.WriteFile(filepath.Join(gd, "rebase-merge", "msgnum"), []byte("2\n"), 0o644)
	os.WriteFile(filepath.Join(gd, "rebase-merge", "end"), []byte("5\n"), 0o644)
	os.WriteFile(filepath.Join(gd, "rebase-merge", "head-name"), []byte("refs/heads/topic\n"), 0o644)
	sub := filepath.Join(root, "a", "b")
	os.MkdirAll(sub, 0o755)

	r, g, ok := Find(sub)
	if !ok || r != root || g != gd {
		t.Fatalf("Find = %q %q %v", r, g, ok)
	}
	s := &Status{Root: r, GitDir: g}
	readHead(s)
	readState(s)
	if s.State != "rebase 2/5" || s.Branch != "topic" || s.Detached {
		t.Errorf("rebase state wrong: %+v", s)
	}
}

func TestWorktreeGitFile(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "main", ".git", "worktrees", "wt")
	os.MkdirAll(real, 0o755)
	os.WriteFile(filepath.Join(real, "HEAD"), []byte("ref: refs/heads/wt-branch\n"), 0o644)
	wt := filepath.Join(root, "wt")
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../main/.git/worktrees/wt\n"), 0o644)
	_, g, ok := Find(wt)
	if !ok || g != real {
		t.Fatalf("worktree gitdir = %q (%v), want %q", g, ok, real)
	}
	s := &Status{GitDir: g}
	readHead(s)
	if s.Branch != "wt-branch" {
		t.Errorf("branch = %q", s.Branch)
	}
}
