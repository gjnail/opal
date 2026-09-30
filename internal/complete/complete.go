// Package complete produces tab-completion candidates for git (branches,
// remotes, changed files...) for shells that have no git completion of their
// own, mainly PowerShell. Each shell's init script calls `opal complete` and
// turns the answers into its own completion format.
package complete

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"opal/internal/git"
)

// Candidate is one completion. Display, if set, is a shorter label for
// menus (e.g. ~/src/app for a full path).
type Candidate struct {
	Value   string
	Desc    string
	Display string
}

// FilesSentinel tells the shell to fall back to its own path completion.
const FilesSentinel = "__files__"

// Files is the "complete paths yourself" answer.
var Files = []Candidate{{Value: FilesSentinel}}

// Filter keeps candidates starting with cur (case-insensitive unless cur has
// uppercase), sorted and de-duplicated, preserving first-seen descriptions.
func Filter(cands []Candidate, cur string) []Candidate {
	if len(cands) == 1 && cands[0].Value == FilesSentinel {
		return cands
	}
	fold := !hasUpper(cur)
	lc := strings.ToLower(cur)
	seen := map[string]bool{}
	var out []Candidate
	for _, c := range cands {
		if seen[c.Value] {
			continue
		}
		v := c.Value
		ok := strings.HasPrefix(v, cur)
		if !ok && fold {
			ok = strings.HasPrefix(strings.ToLower(v), lc)
		}
		if ok {
			seen[c.Value] = true
			out = append(out, c)
		}
	}
	return out
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

var gitCommands = []Candidate{
	{Value: "add", Desc: "Add file contents to the index"},
	{Value: "bisect", Desc: "Find the commit that introduced a bug"},
	{Value: "blame", Desc: "Show who last changed each line"},
	{Value: "branch", Desc: "List, create, or delete branches"},
	{Value: "checkout", Desc: "Switch branches or restore files"},
	{Value: "cherry-pick", Desc: "Apply changes from existing commits"},
	{Value: "clean", Desc: "Remove untracked files"},
	{Value: "clone", Desc: "Clone a repository"},
	{Value: "commit", Desc: "Record changes to the repository"},
	{Value: "config", Desc: "Get and set options"},
	{Value: "describe", Desc: "Name a commit from the nearest tag"},
	{Value: "diff", Desc: "Show changes"},
	{Value: "fetch", Desc: "Download objects and refs"},
	{Value: "grep", Desc: "Print lines matching a pattern"},
	{Value: "help", Desc: "Show help"},
	{Value: "init", Desc: "Create an empty repository"},
	{Value: "log", Desc: "Show commit logs"},
	{Value: "merge", Desc: "Join histories together"},
	{Value: "mv", Desc: "Move or rename a file"},
	{Value: "pull", Desc: "Fetch and integrate"},
	{Value: "push", Desc: "Update remote refs"},
	{Value: "rebase", Desc: "Reapply commits on another base"},
	{Value: "reflog", Desc: "Manage reflog information"},
	{Value: "remote", Desc: "Manage tracked repositories"},
	{Value: "reset", Desc: "Reset HEAD to a state"},
	{Value: "restore", Desc: "Restore working tree files"},
	{Value: "revert", Desc: "Revert commits"},
	{Value: "rm", Desc: "Remove files"},
	{Value: "shortlog", Desc: "Summarize git log output"},
	{Value: "show", Desc: "Show objects"},
	{Value: "stash", Desc: "Stash away changes"},
	{Value: "status", Desc: "Show working tree status"},
	{Value: "submodule", Desc: "Manage submodules"},
	{Value: "switch", Desc: "Switch branches"},
	{Value: "tag", Desc: "Create, list, or delete tags"},
	{Value: "worktree", Desc: "Manage multiple working trees"},
}

var gitOptions = map[string][]Candidate{
	"commit":   {{Value: "--message", Desc: "commit message"}, {Value: "--amend", Desc: "rewrite the last commit"}, {Value: "--no-edit", Desc: "keep the message"}, {Value: "--all", Desc: "stage tracked changes"}, {Value: "--fixup", Desc: "fixup! commit"}, {Value: "--verbose", Desc: "show the diff in the editor"}, {Value: "--no-verify", Desc: "skip hooks"}},
	"push":     {{Value: "--force-with-lease", Desc: "force, safely"}, {Value: "--set-upstream", Desc: "track the remote branch"}, {Value: "--tags", Desc: "push tags"}, {Value: "--delete", Desc: "delete a remote ref"}, {Value: "--dry-run", Desc: "don't actually push"}},
	"pull":     {{Value: "--rebase", Desc: "rebase instead of merge"}, {Value: "--ff-only", Desc: "fast-forward only"}, {Value: "--no-rebase", Desc: "merge"}},
	"fetch":    {{Value: "--all", Desc: "all remotes"}, {Value: "--prune", Desc: "drop deleted remote branches"}, {Value: "--tags", Desc: "fetch tags"}},
	"checkout": {{Value: "-b", Desc: "create a branch"}, {Value: "--track", Desc: "set upstream"}, {Value: "--", Desc: "restore files"}},
	"switch":   {{Value: "--create", Desc: "create a branch"}, {Value: "--detach", Desc: "detached HEAD"}, {Value: "--discard-changes", Desc: "throw away local changes"}},
	"log":      {{Value: "--oneline", Desc: "one line per commit"}, {Value: "--graph", Desc: "draw the graph"}, {Value: "--all", Desc: "all refs"}, {Value: "--stat", Desc: "diffstat"}, {Value: "--patch", Desc: "show diffs"}, {Value: "--author", Desc: "filter by author"}, {Value: "--since", Desc: "commits after a date"}},
	"diff":     {{Value: "--staged", Desc: "staged changes"}, {Value: "--stat", Desc: "diffstat"}, {Value: "--name-only", Desc: "file names only"}, {Value: "--word-diff", Desc: "word-level diff"}},
	"add":      {{Value: "--all", Desc: "everything"}, {Value: "--patch", Desc: "pick hunks"}, {Value: "--update", Desc: "tracked files only"}, {Value: "--intent-to-add", Desc: "record intent"}},
	"stash":    {{Value: "--include-untracked", Desc: "stash untracked too"}, {Value: "--message", Desc: "stash message"}, {Value: "--keep-index", Desc: "keep staged changes"}},
	"rebase":   {{Value: "--interactive", Desc: "edit the todo list"}, {Value: "--continue", Desc: "continue"}, {Value: "--abort", Desc: "abort"}, {Value: "--skip", Desc: "skip this commit"}, {Value: "--onto", Desc: "new base"}, {Value: "--autosquash", Desc: "apply fixup!/squash!"}},
	"reset":    {{Value: "--hard", Desc: "discard everything"}, {Value: "--soft", Desc: "keep changes staged"}, {Value: "--mixed", Desc: "keep changes unstaged"}},
	"branch":   {{Value: "--delete", Desc: "delete"}, {Value: "--move", Desc: "rename"}, {Value: "--all", Desc: "include remotes"}, {Value: "--verbose", Desc: "show upstream"}, {Value: "-D", Desc: "force delete"}},
	"merge":    {{Value: "--no-ff", Desc: "always make a merge commit"}, {Value: "--squash", Desc: "squash"}, {Value: "--abort", Desc: "abort"}, {Value: "--continue", Desc: "continue"}},
	"restore":  {{Value: "--staged", Desc: "unstage"}, {Value: "--source", Desc: "restore from a commit"}, {Value: "--worktree", Desc: "working tree"}},
	"status":   {{Value: "--short", Desc: "short format"}, {Value: "--branch", Desc: "show branch"}},
	"clone":    {{Value: "--depth", Desc: "shallow clone"}, {Value: "--branch", Desc: "check out a branch"}, {Value: "--recurse-submodules", Desc: "with submodules"}},
}

var subSubs = map[string][]Candidate{
	"remote":    {{Value: "add", Desc: "add a remote"}, {Value: "remove", Desc: "remove a remote"}, {Value: "rename", Desc: "rename a remote"}, {Value: "set-url", Desc: "change a URL"}, {Value: "get-url", Desc: "print a URL"}, {Value: "show", Desc: "details"}, {Value: "prune", Desc: "drop stale branches"}, {Value: "-v", Desc: "list with URLs"}},
	"stash":     {{Value: "push", Desc: "stash changes"}, {Value: "pop", Desc: "apply and drop"}, {Value: "apply", Desc: "apply"}, {Value: "list", Desc: "list stashes"}, {Value: "show", Desc: "show a stash"}, {Value: "drop", Desc: "delete a stash"}, {Value: "clear", Desc: "delete all"}, {Value: "branch", Desc: "branch from a stash"}},
	"worktree":  {{Value: "add", Desc: "new worktree"}, {Value: "list", Desc: "list"}, {Value: "remove", Desc: "remove"}, {Value: "prune", Desc: "prune"}, {Value: "move", Desc: "move"}},
	"bisect":    {{Value: "start", Desc: "start"}, {Value: "good", Desc: "mark good"}, {Value: "bad", Desc: "mark bad"}, {Value: "reset", Desc: "finish"}, {Value: "skip", Desc: "skip"}},
	"submodule": {{Value: "update", Desc: "update"}, {Value: "init", Desc: "init"}, {Value: "status", Desc: "status"}, {Value: "add", Desc: "add"}, {Value: "sync", Desc: "sync"}},
}

// Flags whose next word is a value, not a positional argument.
var valueFlags = map[string]bool{
	"-m": true, "--message": true, "-b": true, "-B": true, "-c": true, "-C": true, "-F": true,
	"--onto": true, "--source": true, "--author": true, "--since": true, "--depth": true, "-o": true,
}

// Git completes `git <args...> <cur>`.
func Git(args []string, cur string) []Candidate {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		if args[i] == "-C" || args[i] == "-c" {
			i++
		}
		i++
	}
	if i >= len(args) {
		if strings.HasPrefix(cur, "-") {
			return Filter([]Candidate{{Value: "--version", Desc: ""}, {Value: "--help", Desc: ""}, {Value: "-C", Desc: "run in a directory"}}, cur)
		}
		return Filter(append(append([]Candidate(nil), gitCommands...), gitAliases()...), cur)
	}
	sub, rest := args[i], args[i+1:]
	if strings.HasPrefix(cur, "-") {
		return Filter(gitOptions[sub], cur)
	}
	var pos []string
	dashdash, staged := false, false
	for j := 0; j < len(rest); j++ {
		a := rest[j]
		switch {
		case a == "--":
			dashdash = true
		case a == "--staged" || a == "--cached" || a == "-S":
			staged = true
		case valueFlags[a]:
			j++
		case strings.HasPrefix(a, "-"):
		default:
			pos = append(pos, a)
		}
	}
	creating := contains(rest, "-b") || contains(rest, "-B") || contains(rest, "--create") || contains(rest, "-c")

	switch sub {
	case "help":
		return Filter(gitCommands, cur)
	case "checkout":
		if dashdash {
			return Filter(changed(modeUnstaged), cur)
		}
		if creating && len(pos) == 0 {
			return nil // naming a new branch
		}
		return Filter(refs(true, true, true), cur)
	case "switch":
		if creating && len(pos) == 0 {
			return nil
		}
		return Filter(refs(true, true, false), cur)
	case "merge", "rebase", "cherry-pick", "log", "show", "reset", "revert", "shortlog", "describe", "reflog":
		if dashdash {
			return Files
		}
		return Filter(refs(false, true, true), cur)
	case "diff":
		if dashdash || len(pos) > 0 {
			if staged {
				return Filter(changed(modeStaged), cur)
			}
			return Filter(changed(modeUnstaged), cur)
		}
		return Filter(append(refs(false, true, true), changedFor(staged)...), cur)
	case "branch", "tag":
		return Filter(refs(false, sub == "branch", sub == "tag"), cur)
	case "push", "pull", "fetch":
		if len(pos) == 0 {
			return Filter(remotes(), cur)
		}
		return Filter(refs(false, true, false), cur)
	case "remote":
		if len(pos) == 0 {
			return Filter(subSubs["remote"], cur)
		}
		switch pos[0] {
		case "remove", "rm", "rename", "set-url", "get-url", "show", "prune":
			if len(pos) == 1 {
				return Filter(remotes(), cur)
			}
		}
		return nil
	case "add":
		return Filter(changed(modeAddable), cur)
	case "restore":
		if staged {
			return Filter(changed(modeStaged), cur)
		}
		return Filter(changed(modeUnstaged), cur)
	case "stash":
		if len(pos) == 0 {
			return Filter(subSubs["stash"], cur)
		}
		switch pos[0] {
		case "show", "pop", "apply", "drop", "branch":
			return Filter(stashes(), cur)
		}
		return nil
	case "worktree", "bisect", "submodule":
		if len(pos) == 0 {
			return Filter(subSubs[sub], cur)
		}
		if sub == "bisect" {
			return Filter(refs(false, true, true), cur)
		}
		return Files
	}
	return Files
}

func changedFor(staged bool) []Candidate {
	if staged {
		return changed(modeStaged)
	}
	return changed(modeUnstaged)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func gitOut(args ...string) []byte {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return out
}

func lines(b []byte) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// refs lists local branches (then remote-tracking branches, tags). With dwim,
// remote branches also appear by their short name, the way `git switch x`
// creates a tracking branch for origin/x.
func refs(dwim, branches, tags bool) []Candidate {
	var local, remote, tagList, short []Candidate
	have := map[string]bool{}
	for _, r := range lines(gitOut("for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes", "refs/tags")) {
		switch {
		case strings.HasPrefix(r, "refs/heads/"):
			n := strings.TrimPrefix(r, "refs/heads/")
			local = append(local, Candidate{Value: n, Desc: "branch"})
			have[n] = true
		case strings.HasPrefix(r, "refs/remotes/"):
			n := strings.TrimPrefix(r, "refs/remotes/")
			if strings.HasSuffix(n, "/HEAD") || !strings.Contains(n, "/") {
				continue
			}
			remote = append(remote, Candidate{Value: n, Desc: "remote branch"})
			if dwim {
				short = append(short, Candidate{Value: n[strings.Index(n, "/")+1:], Desc: "remote branch (" + n + ")"})
			}
		case strings.HasPrefix(r, "refs/tags/"):
			tagList = append(tagList, Candidate{Value: strings.TrimPrefix(r, "refs/tags/"), Desc: "tag"})
		}
	}
	var out []Candidate
	if branches {
		out = append(out, local...)
		for _, s := range short {
			if !have[s.Value] {
				out = append(out, s)
			}
		}
		out = append(out, remote...)
	}
	if tags {
		out = append(out, tagList...)
	}
	return out
}

func remotes() []Candidate {
	var out []Candidate
	for _, r := range lines(gitOut("remote")) {
		out = append(out, Candidate{Value: r, Desc: "remote"})
	}
	return out
}

func stashes() []Candidate {
	var out []Candidate
	for _, l := range lines(gitOut("stash", "list", "--format=%gd%x09%s")) {
		ref, msg, _ := strings.Cut(l, "\t")
		out = append(out, Candidate{Value: ref, Desc: msg})
	}
	return out
}

func gitAliases() []Candidate {
	var out []Candidate
	for _, l := range lines(gitOut("config", "--get-regexp", `^alias\.`)) {
		name, val, _ := strings.Cut(strings.TrimPrefix(l, "alias."), " ")
		out = append(out, Candidate{Value: name, Desc: "alias: " + val})
	}
	return out
}

type fileMode int

const (
	modeAddable  fileMode = iota // unstaged + untracked
	modeUnstaged                 // modified in the work tree
	modeStaged                   // in the index
)

// changed lists files from `git status`, relative to the current directory.
func changed(mode fileMode) []Candidate {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	root, _, ok := git.Find(cwd)
	if !ok {
		return nil
	}
	out := gitOut("status", "--porcelain=v1", "-z", "--untracked-files=all")
	recs := bytes.Split(out, []byte{0})
	var cands []Candidate
	for i := 0; i < len(recs); i++ {
		r := string(recs[i])
		if len(r) < 4 {
			continue
		}
		x, y, path := r[0], r[1], r[3:]
		if x == 'R' || x == 'C' {
			i++ // the original path follows
		}
		var desc string
		switch {
		case x == '?' && y == '?':
			if mode != modeAddable {
				continue
			}
			desc = "untracked"
		case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
			desc = "conflict"
		case mode == modeStaged:
			if x == ' ' {
				continue
			}
			desc = "staged " + statusWord(x)
		default:
			if y == ' ' {
				continue
			}
			desc = statusWord(y)
		}
		rel, err := filepath.Rel(cwd, filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			rel = path
		}
		cands = append(cands, Candidate{Value: filepath.ToSlash(rel), Desc: desc})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Value < cands[j].Value })
	return cands
}

func statusWord(c byte) string {
	switch c {
	case 'M':
		return "modified"
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'T':
		return "type changed"
	}
	return "changed"
}
