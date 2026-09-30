// Package git reads repository state for the prompt. The branch comes from
// .git/HEAD directly (no process spawn); dirty counts come from a single
// `git status --porcelain=v2` call bounded by a timeout.
package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Status is a snapshot of a repository.
type Status struct {
	Root      string
	GitDir    string
	Branch    string
	Detached  bool
	Commit    string // short sha when detached
	Upstream  bool
	Ahead     int
	Behind    int
	Staged    int
	Modified  int
	Untracked int
	Conflicts int
	Stash     int
	State     string // "rebase 2/5", "merge", "cherry-pick", "revert", "bisect"
	Partial   bool   // counts unavailable (timeout or no git binary)
}

// Dirty reports whether the work tree has any changes.
func (s *Status) Dirty() bool {
	return s.Staged+s.Modified+s.Untracked+s.Conflicts > 0
}

// Find walks up from dir to the repository root. It understands .git files
// (worktrees and submodules) as well as .git directories.
func Find(dir string) (root, gitDir string, ok bool) {
	dir = filepath.Clean(dir)
	for {
		p := filepath.Join(dir, ".git")
		if fi, err := os.Stat(p); err == nil {
			if fi.IsDir() {
				return dir, p, true
			}
			if b, err := os.ReadFile(p); err == nil {
				line := strings.TrimSpace(string(b))
				if gd, found := strings.CutPrefix(line, "gitdir:"); found {
					gd = strings.TrimSpace(gd)
					if !filepath.IsAbs(gd) {
						gd = filepath.Join(dir, gd)
					}
					return dir, filepath.Clean(gd), true
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

// Read returns the status of the repo containing dir, or nil, waiting as long
// as timeout for git.
func Read(dir string, timeout time.Duration) *Status {
	return ReadBudget(dir, timeout, "", nil)
}

// ReadBudget is Read for the prompt. If git answers within budget the result
// is fresh (and cached in cacheDir). If not, which happens in huge repos or on
// a cold disk, it returns the last known counts marked Partial right away
// and calls refresh(root) so a background process can update the cache
// for the next prompt. The prompt never waits longer than budget.
func ReadBudget(dir string, budget time.Duration, cacheDir string, refresh func(root string)) *Status {
	root, gitDir, ok := Find(dir)
	if !ok {
		return nil
	}
	s := &Status{Root: root, GitDir: gitDir}
	readHead(s)
	readState(s)

	type result struct {
		out []byte
		err error
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan result, 1)
	go func() {
		out, err := runStatusCtx(ctx, dir, true)
		var notFound *exec.Error
		if err != nil && ctx.Err() == nil && !errors.As(err, &notFound) {
			out, err = runStatusCtx(ctx, dir, false) // git < 2.35 has no --show-stash
		}
		done <- result{out, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			s.Partial = true
			return s
		}
		parsePorcelain(s, r.out)
		if cacheDir != "" {
			SaveCache(cacheDir, s)
		}
		return s
	case <-time.After(budget):
		cancel()
		if c := LoadCache(cacheDir, root); c != nil && c.Branch == s.Branch {
			s.Upstream, s.Ahead, s.Behind, s.Stash = c.Upstream, c.Ahead, c.Behind, c.Stash
			s.Staged, s.Modified, s.Untracked, s.Conflicts = c.Staged, c.Modified, c.Untracked, c.Conflicts
		}
		s.Partial = true
		if refresh != nil {
			refresh(root)
		}
		return s
	}
}

func runStatus(dir string, timeout time.Duration, stash bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return runStatusCtx(ctx, dir, stash)
}

func runStatusCtx(ctx context.Context, dir string, stash bool) ([]byte, error) {
	args := []string{"--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z"}
	if stash {
		args = append(args, "--show-stash")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, err
}

func isTimeout(err error) bool { return err == context.DeadlineExceeded }

func readHead(s *Status) {
	b, err := os.ReadFile(filepath.Join(s.GitDir, "HEAD"))
	if err != nil {
		return
	}
	head := strings.TrimSpace(string(b))
	if ref, ok := strings.CutPrefix(head, "ref: "); ok {
		s.Branch = strings.TrimPrefix(ref, "refs/heads/")
		return
	}
	s.Detached = true
	if len(head) >= 7 {
		s.Commit = head[:7]
	}
}

func readState(s *Status) {
	gd := s.GitDir
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(gd, name))
		return err == nil
	}
	readInt := func(name string) int {
		b, err := os.ReadFile(filepath.Join(gd, name))
		if err != nil {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return n
	}
	switch {
	case has("rebase-merge"):
		s.State = progress("rebase", readInt("rebase-merge/msgnum"), readInt("rebase-merge/end"))
		if s.Detached {
			if b, err := os.ReadFile(filepath.Join(gd, "rebase-merge", "head-name")); err == nil {
				s.Branch = strings.TrimPrefix(strings.TrimSpace(string(b)), "refs/heads/")
				s.Detached = false
			}
		}
	case has("rebase-apply"):
		name := "rebase"
		if has("rebase-apply/applying") {
			name = "am"
		}
		s.State = progress(name, readInt("rebase-apply/next"), readInt("rebase-apply/last"))
	case has("MERGE_HEAD"):
		s.State = "merge"
	case has("CHERRY_PICK_HEAD"):
		s.State = "cherry-pick"
	case has("REVERT_HEAD"):
		s.State = "revert"
	case has("BISECT_LOG"):
		s.State = "bisect"
	}
}

func progress(name string, n, total int) string {
	if total > 0 {
		return name + " " + strconv.Itoa(n) + "/" + strconv.Itoa(total)
	}
	return name
}

// parsePorcelain reads `git status --porcelain=v2 --branch -z` output.
func parsePorcelain(s *Status, out []byte) {
	recs := bytes.Split(out, []byte{0})
	for i := 0; i < len(recs); i++ {
		r := string(recs[i])
		if r == "" {
			continue
		}
		switch r[0] {
		case '#':
			f := strings.Fields(r)
			if len(f) < 3 {
				continue
			}
			switch f[1] {
			case "branch.head":
				if f[2] == "(detached)" {
					s.Detached = true
				} else {
					s.Branch, s.Detached = f[2], false
				}
			case "branch.oid":
				if len(f[2]) >= 7 && f[2] != "(initial)" {
					s.Commit = f[2][:7]
				}
			case "branch.upstream":
				s.Upstream = true
			case "branch.ab":
				if len(f) >= 4 {
					s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
					s.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
				}
			case "stash":
				s.Stash, _ = strconv.Atoi(f[2])
			}
		case '1', '2':
			if len(r) >= 4 {
				if r[2] != '.' {
					s.Staged++
				}
				if r[3] != '.' {
					s.Modified++
				}
			}
			if r[0] == '2' {
				i++ // renames carry the original path as an extra record
			}
		case 'u':
			s.Conflicts++
		case '?':
			s.Untracked++
		}
	}
}

// MainBranch guesses the repository's main branch: origin/HEAD if known,
// otherwise the first of main, trunk, mainline, default, stable, master that
// exists locally.
func MainBranch() string {
	if out, err := exec.Command("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			return strings.TrimPrefix(b, "origin/")
		}
	}
	candidates := []string{"main", "trunk", "mainline", "default", "stable", "master"}
	args := []string{"for-each-ref", "--format=%(refname:short)"}
	for _, c := range candidates {
		args = append(args, "refs/heads/"+c)
	}
	if out, err := exec.Command("git", args...).Output(); err == nil {
		have := map[string]bool{}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			have[strings.TrimSpace(l)] = true
		}
		for _, c := range candidates {
			if have[c] {
				return c
			}
		}
	}
	return "master"
}
