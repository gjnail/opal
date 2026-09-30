package history

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
)

// Command is a unique command line with its most recent use.
type Command struct {
	Entry       // the most recent run
	Count int   // how many times it was run
	order int64 // position of the most recent run (newest = largest)
}

// Scope narrows a search.
type Scope int

const (
	AllShells Scope = iota
	ThisDir
	ThisShell
)

// Unique collapses entries into commands, newest first.
func Unique(entries []Entry) []Command {
	idx := map[string]int{}
	var out []Command
	for i, e := range entries {
		key := strings.TrimSpace(e.Cmd)
		if j, ok := idx[key]; ok {
			out[j].Count++
			if e.When >= out[j].When {
				out[j].Entry = e
				out[j].order = int64(i)
			}
			continue
		}
		idx[key] = len(out)
		out = append(out, Command{Entry: e, Count: 1, order: int64(i)})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].When != out[b].When {
			return out[a].When > out[b].When
		}
		return out[a].order > out[b].order
	})
	return out
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Search filters cmds by query within scope. Every space-separated term must
// appear (smart case; ^term anchors to the start, term$ to the end). Commands
// that start with the first term rank above ones that merely contain it;
// ties go to the most recent. When nothing matches literally, a fuzzy
// (in-order letters) pass runs instead.
func Search(cmds []Command, query string, scope Scope, cwd, shell string) []Command {
	var pool []Command
	for _, c := range cmds {
		switch scope {
		case ThisDir:
			if !samePath(c.Cwd, cwd) {
				continue
			}
		case ThisShell:
			if strings.TrimPrefix(c.Shell, "import:") != shell {
				continue
			}
		}
		pool = append(pool, c)
	}
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return pool
	}
	fold := !hasUpper(query)
	norm := func(s string) string {
		if fold {
			return strings.ToLower(s)
		}
		return s
	}
	for i := range terms {
		terms[i] = norm(terms[i])
	}

	var prefix, contains []Command
	for _, c := range pool {
		text := norm(c.Cmd)
		ok := true
		for _, t := range terms {
			if !matchTerm(text, t) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if strings.HasPrefix(text, strings.TrimPrefix(terms[0], "^")) {
			prefix = append(prefix, c)
		} else {
			contains = append(contains, c)
		}
	}
	if len(prefix)+len(contains) > 0 {
		return append(prefix, contains...)
	}
	var fuzzy []Command
	needle := norm(strings.Join(terms, ""))
	for _, c := range pool {
		if subsequence(norm(c.Cmd), needle) {
			fuzzy = append(fuzzy, c)
		}
	}
	return fuzzy
}

func matchTerm(text, t string) bool {
	switch {
	case strings.HasPrefix(t, "^") && strings.HasSuffix(t, "$") && len(t) > 1:
		return text == t[1:len(t)-1]
	case strings.HasPrefix(t, "^"):
		return strings.HasPrefix(text, t[1:])
	case strings.HasSuffix(t, "$"):
		return strings.HasSuffix(text, t[:len(t)-1])
	}
	return strings.Contains(text, t)
}

func subsequence(text, needle string) bool {
	i := 0
	for _, r := range text {
		if i < len(needle) && r == rune(needle[i]) {
			i++
		}
	}
	return i == len(needle)
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
