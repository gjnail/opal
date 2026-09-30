package settings

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"opal/internal/config"
)

// Settings changed in the terminal are written back to config.toml line by
// line, so comments, layout and the CLI's own tables stay as they were.

// Set writes key = value into a table of config.toml: table "" is the
// top level (shared with the prompt), "terminal", "terminal.clipboard",
// "terminal.keys", and so on. value is already TOML (quoted strings,
// numbers, true/false, arrays).
func Set(table, key, value string) error {
	if table == "" {
		return config.SetKey(key, value)
	}
	path := config.Path()
	if _, _, err := config.WriteDefault(); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := setInTable(string(b), table, key, value)
	return os.WriteFile(path, []byte(out), 0o644)
}

// Unset removes key from a table, if it's there.
func Unset(table, key string) error {
	path := config.Path()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	start, end := findTable(lines, table)
	if start < 0 {
		return nil
	}
	for i := start + 1; i < end; i++ {
		if keyRe(key).MatchString(lines[i]) {
			j := valueEnd(lines, i)
			lines = append(lines[:i], lines[j+1:]...)
			break
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

var headerRe = regexp.MustCompile(`^\s*\[\[?\s*([^\]]+?)\s*\]\]?\s*(#.*)?$`)

// findTable returns the header line of [table] and the index where the
// table ends (the next header, or len(lines)). start is -1 when missing.
func findTable(lines []string, table string) (start, end int) {
	start = -1
	for i, l := range lines {
		m := headerRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if start >= 0 {
			return start, i
		}
		if strings.TrimSpace(m[1]) == table && !strings.HasPrefix(strings.TrimSpace(l), "[[") {
			start = i
		}
	}
	return start, len(lines)
}

// keyRe matches a line assigning key (bare or quoted).
func keyRe(key string) *regexp.Regexp {
	q := regexp.QuoteMeta(key)
	return regexp.MustCompile(`^\s*(?:` + q + `|"` + q + `"|'` + q + `')\s*=`)
}

// valueEnd finds the last line of a value that starts on line i: arrays
// may span lines.
func valueEnd(lines []string, i int) int {
	_, v, _ := strings.Cut(lines[i], "=")
	depth := strings.Count(stripComment(v), "[") - strings.Count(stripComment(v), "]")
	j := i
	for depth > 0 && j+1 < len(lines) {
		j++
		c := stripComment(lines[j])
		depth += strings.Count(c, "[") - strings.Count(c, "]")
	}
	return j
}

func stripComment(s string) string {
	inStr := false
	for i, r := range s {
		switch {
		case r == '"':
			inStr = !inStr
		case r == '#' && !inStr:
			return s[:i]
		}
	}
	return s
}

// tomlKey quotes a key when it isn't a bare TOML key ("ctrl+shift+t").
func tomlKey(k string) string {
	for _, r := range k {
		if !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return strconv.Quote(k)
		}
	}
	return k
}

func setInTable(src, table, key, value string) string {
	line := tomlKey(key) + " = " + value
	lines := strings.Split(src, "\n")
	start, end := findTable(lines, table)
	if start < 0 {
		// Append the table (after its parent if the parent exists, so
		// [terminal.keys] sits near [terminal]; otherwise at the end).
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "["+table+"]", line, "")
		return strings.Join(lines, "\n")
	}
	re := keyRe(key)
	for i := start + 1; i < end; i++ {
		if re.MatchString(lines[i]) {
			j := valueEnd(lines, i)
			// Keep a trailing comment on a single-line value.
			comment := ""
			if j == i {
				if _, v, ok := strings.Cut(lines[i], "="); ok {
					if c := strings.TrimPrefix(v, stripComment(v)); c != "" {
						comment = "  " + strings.TrimSpace(c)
					}
				}
			}
			repl := append([]string{line + comment}, lines[j+1:]...)
			return strings.Join(append(lines[:i], repl...), "\n")
		}
	}
	// Insert after the table's last non-blank line.
	at := end
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	return strings.Join(lines, "\n")
}

// Quote renders a TOML string.
func Quote(s string) string { return strconv.Quote(s) }

// Number renders a float without trailing zeros.
func Number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Bool renders a TOML boolean.
func Bool(b bool) string { return fmt.Sprint(b) }
