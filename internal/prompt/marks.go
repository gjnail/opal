package prompt

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
)

// Semantic prompt marks (FinalTerm / OSC 133) tell a terminal where each
// prompt, command and output begins, which enables jumping between commands,
// selecting a command's output, and exit-status markers in the gutter.
// OSC 7 reports the working directory so new tabs open in the same place.
const (
	MarkPromptStart = "\x1b]133;A\x1b\\"
	MarkPromptEnd   = "\x1b]133;B\x1b\\"
	MarkOutputStart = "\x1b]133;C\x1b\\"
)

// MarksEnabled reports whether marks should be emitted: on unless turned off,
// and never on the bare Linux console or dumb terminals, which would print them.
func MarksEnabled(on bool, depth int) bool {
	t := os.Getenv("TERM")
	return on && depth > 0 && t != "linux" && t != "dumb"
}

// CommandDone is OSC 133;D with the finished command's exit status.
func CommandDone(status int) string { return fmt.Sprintf("\x1b]133;D;%d\x1b\\", status) }

// CwdMarks reports dir via OSC 7, plus OSC 9;9 for Windows Terminal and
// ConEmu, which use that to open duplicated tabs in the same directory.
func CwdMarks(dir string) string {
	if dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "" // PowerShell registry drives and the like aren't places
	}
	host, _ := os.Hostname()
	p := filepath.ToSlash(dir)
	if runtime.GOOS == "windows" {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Host: host, Path: p}
	s := "\x1b]7;" + u.String() + "\x1b\\"
	if runtime.GOOS == "windows" && (os.Getenv("WT_SESSION") != "" || os.Getenv("ConEmuPID") != "") {
		s += "\x1b]9;9;\"" + dir + "\"\x1b\\"
	}
	return s
}

// WithMarks wraps rendered lines in prompt-start/end marks, preceded by the
// previous command's completion (when one ran) and the cwd report.
func WithMarks(lines [][]Span, c *Context) [][]Span {
	if len(lines) == 0 {
		return lines
	}
	var pre []Span
	if c.DurationMS >= 0 {
		pre = append(pre, Span{Seq: CommandDone(c.Status)})
	}
	if cwd := CwdMarks(c.Cwd); cwd != "" {
		pre = append(pre, Span{Seq: cwd})
	}
	pre = append(pre, Span{Seq: MarkPromptStart})
	out := make([][]Span, len(lines))
	copy(out, lines)
	out[0] = append(pre, out[0]...)
	last := len(out) - 1
	out[last] = append(append([]Span(nil), out[last]...), Span{Seq: MarkPromptEnd})
	return out
}

// Transient is the collapsed prompt a finished command line is redrawn with:
// just the prompt character in the shell's color.
func Transient(c *Context, depth int, marks bool) string {
	line := []Span{{Text: c.Sym.Char, FG: c.Pal.Hue, Bold: true}, {Text: " "}}
	if marks {
		line = append([]Span{{Seq: MarkPromptStart}}, append(line, Span{Seq: MarkPromptEnd})...)
	}
	return Encode([][]Span{line}, c.Shell, depth)
}
