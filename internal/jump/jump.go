// Package jump keeps a frecency-ranked list of visited directories. The
// prompt records visits, since it already runs after every cd.
package jump

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"opal/internal/platform"
)

// Entry is one remembered directory.
type Entry struct {
	Path string
	Rank float64
	Last int64 // unix seconds
}

const maxTotalRank = 9000

// File is where the database lives.
func File() string { return filepath.Join(platform.DataDir(), "jump.tsv") }

// Load reads the database; a missing file is an empty database.
func Load() ([]Entry, error) {
	f, err := os.Open(File())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		rank, err1 := strconv.ParseFloat(parts[0], 64)
		last, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 != nil || err2 != nil || parts[2] == "" {
			continue
		}
		out = append(out, Entry{Path: parts[2], Rank: rank, Last: last})
	}
	return out, sc.Err()
}

// Save writes the database atomically (temp file + rename).
func Save(entries []Entry) error {
	file := File()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "jump-*.tmp")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(tmp)
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%d\t%s\n", strconv.FormatFloat(e.Rank, 'f', 2, 64), e.Last, e.Path)
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), file); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Add records a visit to dir. The home directory is skipped: `cd` already
// gets you there.
func Add(dir string) error {
	dir = filepath.Clean(dir)
	if home, _ := os.UserHomeDir(); home != "" && samePath(dir, filepath.Clean(home)) {
		return nil
	}
	entries, err := Load()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	found := false
	total := 0.0
	for i := range entries {
		if samePath(entries[i].Path, dir) {
			entries[i].Rank++
			entries[i].Last = now
			found = true
		}
		total += entries[i].Rank
	}
	if !found {
		entries = append(entries, Entry{Path: dir, Rank: 1, Last: now})
		total++
	}
	if total > maxTotalRank { // age everything so old haunts fade
		kept := entries[:0]
		for _, e := range entries {
			e.Rank *= 0.9
			if e.Rank >= 1 {
				kept = append(kept, e)
			}
		}
		entries = kept
	}
	return Save(entries)
}

// Remove forgets dir.
func Remove(dir string) error {
	entries, err := Load()
	if err != nil {
		return err
	}
	kept := entries[:0]
	for _, e := range entries {
		if !samePath(e.Path, filepath.Clean(dir)) {
			kept = append(kept, e)
		}
	}
	return Save(kept)
}

// Score weights rank by recency.
func Score(e Entry, now int64) float64 {
	age := now - e.Last
	switch {
	case age < 3600:
		return e.Rank * 4
	case age < 86400:
		return e.Rank * 2
	case age < 604800:
		return e.Rank / 2
	}
	return e.Rank / 4
}

// Ranked returns existing directories best-first.
func Ranked() ([]Entry, error) {
	entries, err := Load()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	var out []Entry
	for _, e := range entries {
		if fi, err := os.Stat(e.Path); err == nil && fi.IsDir() {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return Score(out[i], now) > Score(out[j], now) })
	return out, nil
}

// Match reports whether path matches the query words: every word appears in
// order, and the last word matches within the final path component. Matching
// is case-insensitive unless the query contains an uppercase letter.
func Match(path string, words []string) bool {
	if len(words) == 0 {
		return true
	}
	hay := filepath.ToSlash(path)
	smart := !hasUpper(strings.Join(words, ""))
	if smart {
		hay = strings.ToLower(hay)
	}
	lastSlash := strings.LastIndex(hay, "/")
	pos := 0
	for i, w := range words {
		n := filepath.ToSlash(w)
		if smart {
			n = strings.ToLower(n)
		}
		start := pos
		if i == len(words)-1 && !strings.Contains(n, "/") && lastSlash+1 > start {
			start = lastSlash + 1 // the last word must land in the basename
		}
		idx := strings.Index(hay[start:], n)
		if idx < 0 {
			return false
		}
		pos = start + idx + len(n)
	}
	return true
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// Query returns the best directory for words, excluding the current one.
func Query(words []string, cwd string) (string, bool) {
	entries, err := Ranked()
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if cwd != "" && samePath(e.Path, cwd) {
			continue
		}
		if Match(e.Path, words) {
			return e.Path, true
		}
	}
	return "", false
}
