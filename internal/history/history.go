// Package history keeps one command history shared by every shell: each
// prompt appends the command that just finished, and Ctrl+R searches it all.
package history

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"opal/internal/platform"
)

// Entry is one command run.
type Entry struct {
	When   int64  // unix seconds
	Status int    // exit code
	DurMS  int64  // -1 if unknown
	Shell  string // zsh | bash | fish | pwsh | import:<shell>
	ID     string // shell-session id, used to drop repeats (bash)
	Cwd    string
	Cmd    string
}

// File is where the history lives.
func File() string { return filepath.Join(platform.DataDir(), "history.tsv") }

var esc = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func (e Entry) line() string {
	return fmt.Sprintf("%d\t%d\t%d\t%s\t%s\t%s\t%s\n", e.When, e.Status, e.DurMS,
		esc.Replace(e.Shell), esc.Replace(e.ID), esc.Replace(e.Cwd), esc.Replace(e.Cmd))
}

func parse(line string) (Entry, bool) {
	f := strings.SplitN(line, "\t", 7)
	if len(f) != 7 || f[6] == "" {
		return Entry{}, false
	}
	when, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return Entry{}, false
	}
	status, _ := strconv.Atoi(f[1])
	dur, _ := strconv.ParseInt(f[2], 10, 64)
	return Entry{When: when, Status: status, DurMS: dur, Shell: unescape(f[3]), ID: unescape(f[4]),
		Cwd: unescape(f[5]), Cmd: unescape(f[6])}, true
}

// Append records e. Commands starting with a space stay private, like
// HISTCONTROL=ignorespace. An entry whose ID matches a recent one is a
// repeat (bash reports the previous command when a line was ignored).
func Append(e Entry) error {
	if strings.TrimSpace(e.Cmd) == "" || strings.HasPrefix(e.Cmd, " ") {
		return nil
	}
	e.Cmd = strings.TrimRight(e.Cmd, "\r\n")
	if e.When == 0 {
		e.When = time.Now().Unix()
	}
	file := File()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if e.ID != "" && recentID(file, e.ID) {
		return nil
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(e.line())
	return err
}

// recentID looks at the tail of the file for an entry with id.
func recentID(file, id string) bool {
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	const tail = 8192
	off := fi.Size() - tail
	if off < 0 {
		off = 0
	}
	buf := make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return false
	}
	needle := "\t" + esc.Replace(id) + "\t"
	for _, l := range bytes.Split(buf, []byte{'\n'}) {
		if bytes.Contains(l, []byte(needle)) {
			if e, ok := parse(string(l)); ok && e.ID == id {
				return true
			}
		}
	}
	return false
}

// Load reads every entry, oldest first.
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
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if e, ok := parse(sc.Text()); ok {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Exists reports whether any history has been recorded yet.
func Exists() bool {
	fi, err := os.Stat(File())
	return err == nil && fi.Size() > 0
}
