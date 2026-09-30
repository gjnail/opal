package history

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"opal/internal/platform"
)

// Source is a shell history file opal can import.
type Source struct {
	Shell string
	File  string
	parse func(path string) []Entry
}

// Sources lists the history files present on this machine.
func Sources() []Source {
	home := platform.Home()
	var cands []Source
	add := func(shell, file string, p func(string) []Entry) {
		if fi, err := os.Stat(file); err == nil && !fi.IsDir() && fi.Size() > 0 {
			cands = append(cands, Source{shell, file, p})
		}
	}
	// PowerShell (PSReadLine) keeps one file per host.
	psDir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "PowerShell", "PSReadLine")
	if runtime.GOOS != "windows" {
		psDir = filepath.Join(home, ".local", "share", "powershell", "PSReadLine")
	}
	if ents, err := os.ReadDir(psDir); err == nil {
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), "_history.txt") {
				add("pwsh", filepath.Join(psDir, e.Name()), parsePSReadLine)
			}
		}
	}
	add("bash", filepath.Join(home, ".bash_history"), parseBash)
	zdot := os.Getenv("ZDOTDIR")
	if zdot == "" {
		zdot = home
	}
	add("zsh", filepath.Join(zdot, ".zsh_history"), parseZsh)
	fishData := os.Getenv("XDG_DATA_HOME")
	if fishData == "" {
		fishData = filepath.Join(home, ".local", "share")
	}
	add("fish", filepath.Join(fishData, "fish", "fish_history"), parseFish)
	return cands
}

// Import merges the given sources into the shared history (oldest first)
// and returns how many commands were added. Imported commands without a
// timestamp are placed just before the oldest real one, in file order.
func Import(srcs []Source) (int, error) {
	var all []Entry
	for _, s := range srcs {
		all = append(all, s.parse(s.File)...)
	}
	existing, _ := Load()
	oldest := int64(1 << 62)
	for _, e := range existing {
		if e.When < oldest {
			oldest = e.When
		}
	}
	for _, e := range all {
		if e.When > 0 && e.When < oldest {
			oldest = e.When
		}
	}
	if oldest == 1<<62 {
		oldest = 1
	}
	// Undated entries count down from just before the oldest dated one,
	// keeping their relative order.
	undated := 0
	for _, e := range all {
		if e.When == 0 {
			undated++
		}
	}
	n := 0
	for i := range all {
		if all[i].When == 0 {
			all[i].When = oldest - int64(undated-n)
			n++
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].When < all[j].When })
	// Write before the existing entries so the file stays roughly ordered.
	f, err := os.CreateTemp(filepath.Dir(File()), "history-*.tmp")
	if err != nil {
		if err := os.MkdirAll(filepath.Dir(File()), 0o755); err != nil {
			return 0, err
		}
		if f, err = os.CreateTemp(filepath.Dir(File()), "history-*.tmp"); err != nil {
			return 0, err
		}
	}
	w := bufio.NewWriter(f)
	added := 0
	for _, e := range all {
		if strings.TrimSpace(e.Cmd) == "" {
			continue
		}
		w.WriteString(e.line())
		added++
	}
	for _, e := range existing {
		w.WriteString(e.line())
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(f.Name())
		return 0, err
	}
	f.Close()
	if err := os.Rename(f.Name(), File()); err != nil {
		os.Remove(f.Name())
		return 0, err
	}
	return added, nil
}

func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.Split(s, "\n")
}

// PSReadLine: one command per line; a trailing backtick continues it.
func parsePSReadLine(path string) []Entry {
	var out []Entry
	var cur []string
	for _, l := range readLines(path) {
		if strings.HasSuffix(l, "`") {
			cur = append(cur, strings.TrimSuffix(l, "`"))
			continue
		}
		cur = append(cur, l)
		if cmd := strings.Join(cur, "\n"); strings.TrimSpace(cmd) != "" {
			out = append(out, Entry{Shell: "import:pwsh", DurMS: -1, Cmd: cmd})
		}
		cur = nil
	}
	return out
}

// bash: plain lines, optionally preceded by "#<unix time>" comments.
func parseBash(path string) []Entry {
	var out []Entry
	var when int64
	for _, l := range readLines(path) {
		if strings.HasPrefix(l, "#") {
			if t, err := strconv.ParseInt(l[1:], 10, 64); err == nil {
				when = t
				continue
			}
		}
		out = append(out, Entry{When: when, Shell: "import:bash", DurMS: -1, Cmd: l})
		when = 0
	}
	return out
}

// zsh: ": <time>:<duration>;<cmd>" (extended) or bare lines; a trailing
// backslash continues a line. Non-ASCII bytes are "metafied" (0x83 + byte^32).
func parseZsh(path string) []Entry {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var buf []byte
	for i := 0; i < len(raw); i++ {
		if raw[i] == 0x83 && i+1 < len(raw) {
			i++
			buf = append(buf, raw[i]^32)
			continue
		}
		buf = append(buf, raw[i])
	}
	var out []Entry
	var cur strings.Builder
	for _, l := range strings.Split(strings.ReplaceAll(string(buf), "\r\n", "\n"), "\n") {
		if strings.HasSuffix(l, `\`) {
			cur.WriteString(strings.TrimSuffix(l, `\`) + "\n")
			continue
		}
		cur.WriteString(l)
		line := cur.String()
		cur.Reset()
		e := Entry{Shell: "import:zsh", DurMS: -1, Cmd: line}
		if strings.HasPrefix(line, ": ") {
			if semi := strings.IndexByte(line, ';'); semi > 0 {
				meta := strings.SplitN(line[2:semi], ":", 2)
				e.When, _ = strconv.ParseInt(meta[0], 10, 64)
				if len(meta) == 2 {
					if d, err := strconv.ParseInt(meta[1], 10, 64); err == nil {
						e.DurMS = d * 1000
					}
				}
				e.Cmd = line[semi+1:]
			}
		}
		out = append(out, e)
	}
	return out
}

// fish: a YAML-ish list of "- cmd: ..." with "  when: ..." lines.
func parseFish(path string) []Entry {
	var out []Entry
	for _, l := range readLines(path) {
		switch {
		case strings.HasPrefix(l, "- cmd: "):
			cmd := strings.TrimPrefix(l, "- cmd: ")
			cmd = strings.NewReplacer(`\n`, "\n", `\\`, `\`).Replace(cmd)
			out = append(out, Entry{Shell: "import:fish", DurMS: -1, Cmd: cmd})
		case strings.HasPrefix(l, "  when: ") && len(out) > 0:
			out[len(out)-1].When, _ = strconv.ParseInt(strings.TrimPrefix(l, "  when: "), 10, 64)
		}
	}
	return out
}
