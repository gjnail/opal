// Package picker is a small full-screen fuzzy chooser (the Ctrl+R history
// search). It talks to the terminal directly (CONIN$/CONOUT$ on Windows,
// /dev/tty elsewhere) so the calling shell can capture just the choice on
// stdout, and it looks and behaves the same in every shell.
package picker

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"

	"opal/internal/ansi"
)

// Item is one row.
type Item struct {
	Text string // what gets returned
	Meta string // dim right-hand detail
}

// Style colors the picker (SGR parameter strings, e.g. "38;2;255;0;0").
type Style struct {
	Accent, Muted, Text, Match, SelBG string
	Gem, Char                         string
}

// Options configure a run.
type Options struct {
	Title  string
	Query  string
	Scopes []string // cycled with Ctrl+R; the filter gets the index
	Filter func(query string, scope int) (items []Item, total int)
	Style  Style
}

// Run shows the picker. ok is false when the user cancelled.
func Run(o Options) (choice string, ok bool, err error) {
	in, out, err := openTTY()
	if err != nil {
		return "", false, err
	}
	defer in.Close()
	defer out.Close()
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return "", false, err
	}
	defer term.Restore(int(in.Fd()), state)
	restoreOut := enableVT(out)
	defer restoreOut()

	fmt.Fprint(out, "\x1b[?1049h\x1b[H") // alternate screen: the shell's view is untouched
	defer fmt.Fprint(out, "\x1b[?1049l")

	p := &picker{o: o, out: out, query: []rune(o.Query)}
	p.refilter()
	buf := make([]byte, 256)
	for {
		p.draw()
		n, err := in.Read(buf)
		if err != nil {
			return "", false, err
		}
		for _, k := range parseKeys(buf[:n]) {
			switch k.name {
			case "enter", "tab":
				if p.sel < len(p.items) {
					return p.items[p.sel].Text, true, nil
				}
				return "", false, nil
			case "esc", "ctrl-c", "ctrl-g":
				return "", false, nil
			case "ctrl-d":
				if len(p.query) == 0 {
					return "", false, nil
				}
			case "up", "ctrl-p", "ctrl-k":
				p.move(-1)
			case "down", "ctrl-n", "ctrl-j":
				p.move(1)
			case "pgup":
				p.move(-p.pageSize())
			case "pgdn":
				p.move(p.pageSize())
			case "backspace":
				if len(p.query) > 0 {
					p.query = p.query[:len(p.query)-1]
					p.refilter()
				}
			case "ctrl-u":
				p.query = nil
				p.refilter()
			case "ctrl-w":
				q := strings.TrimRight(string(p.query), " ")
				if i := strings.LastIndex(q, " "); i >= 0 {
					p.query = []rune(q[:i+1])
				} else {
					p.query = nil
				}
				p.refilter()
			case "ctrl-r":
				if len(o.Scopes) > 1 {
					p.scope = (p.scope + 1) % len(o.Scopes)
					p.refilter()
				}
			case "char":
				p.query = append(p.query, k.r)
				p.refilter()
			}
		}
	}
}

type picker struct {
	o      Options
	out    *os.File
	query  []rune
	scope  int
	items  []Item
	total  int
	sel    int
	offset int
}

func (p *picker) refilter() {
	p.items, p.total = p.o.Filter(string(p.query), p.scope)
	p.sel, p.offset = 0, 0
}

func (p *picker) size() (int, int) {
	w, h, err := term.GetSize(int(p.out.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

func (p *picker) pageSize() int {
	_, h := p.size()
	return max(1, h-4)
}

func (p *picker) move(d int) {
	if len(p.items) == 0 {
		return
	}
	p.sel = min(max(p.sel+d, 0), len(p.items)-1)
}

func sgr(params string) string {
	if params == "" {
		return ""
	}
	return "\x1b[" + params + "m"
}

const reset = "\x1b[0m"

// draw repaints the whole screen: header, query line, rule, results.
func (p *picker) draw() {
	w, h := p.size()
	st := p.o.Style
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[H")

	// Header: gem, title, counts and scope on the left; keys on the right.
	scope := ""
	if len(p.o.Scopes) > 0 {
		scope = " · " + p.o.Scopes[p.scope]
	}
	left := fmt.Sprintf(" %s %s", st.Gem, p.o.Title)
	info := fmt.Sprintf("  %d/%d%s", len(p.items), p.total, scope)
	keys := "enter pick · ^r scope · esc cancel "
	pad := w - ansi.Width(left) - ansi.Width(info) - ansi.Width(keys)
	if pad < 1 {
		keys, pad = "", max(1, w-ansi.Width(left)-ansi.Width(info))
	}
	b.WriteString(sgr(st.Accent) + clip(left, w) + reset + sgr(st.Muted) + clip(info, max(0, w-ansi.Width(left))))
	if keys != "" {
		b.WriteString(strings.Repeat(" ", pad) + keys)
	}
	b.WriteString(reset + "\x1b[K\r\n")

	// Query line.
	q := string(p.query)
	b.WriteString(" " + sgr(st.Accent) + st.Char + reset + " " + sgr(st.Text) + clip(q, w-4) + reset + "\x1b[K\r\n")
	b.WriteString(sgr(st.Muted) + strings.Repeat("─", w) + reset + "\r\n")

	// Results, scrolled to keep the selection visible.
	rows := max(1, h-3)
	if p.sel < p.offset {
		p.offset = p.sel
	}
	if p.sel >= p.offset+rows {
		p.offset = p.sel - rows + 1
	}
	terms := strings.Fields(strings.ToLower(q))
	for i := 0; i < rows; i++ {
		idx := p.offset + i
		if idx >= len(p.items) {
			b.WriteString("\x1b[K")
			if i < rows-1 {
				b.WriteString("\r\n")
			}
			continue
		}
		it := p.items[idx]
		selected := idx == p.sel
		bg := ""
		marker := "  "
		if selected {
			bg = st.SelBG
			marker = sgr(st.Accent) + st.Char + " " + reset
		}
		meta := it.Meta
		textW := w - 3 - ansi.Width(meta) - 2
		if textW < 20 {
			meta, textW = "", w-3
		}
		text := oneLine(it.Text)
		shown := clip(text, textW)
		if selected {
			b.WriteString(marker + sgr(bg))
		} else {
			b.WriteString(marker)
		}
		b.WriteString(highlight(shown, terms, st, bg))
		gap := w - 2 - ansi.Width(shown) - ansi.Width(meta) - 1
		if gap > 0 {
			b.WriteString(sgr(bg) + strings.Repeat(" ", gap))
		}
		if meta != "" {
			b.WriteString(sgr(bg) + sgr(st.Muted) + meta)
		}
		b.WriteString(reset + "\x1b[K")
		if i < rows-1 {
			b.WriteString("\r\n")
		}
	}
	// Park the cursor at the end of the query.
	col := 4 + ansi.Width(clip(q, w-4))
	fmt.Fprintf(&b, "\x1b[2;%dH\x1b[?25h", min(col, w))
	fmt.Fprint(p.out, b.String())
}

// highlight paints query terms inside s.
func highlight(s string, terms []string, st Style, bg string) string {
	if len(terms) == 0 {
		return sgr(st.Text) + s
	}
	lower := strings.ToLower(s)
	mark := make([]bool, len(s))
	for _, t := range terms {
		t = strings.Trim(t, "^$")
		if t == "" {
			continue
		}
		for from := 0; ; {
			i := strings.Index(lower[from:], t)
			if i < 0 {
				break
			}
			for j := from + i; j < from+i+len(t) && j < len(mark); j++ {
				mark[j] = true
			}
			from += i + len(t)
		}
	}
	var b strings.Builder
	on := false
	b.WriteString(sgr(st.Text))
	for i, r := range s {
		if mark[i] != on {
			on = mark[i]
			if on {
				b.WriteString(sgr(st.Match))
			} else {
				b.WriteString(reset + sgr(bg) + sgr(st.Text))
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\n", " ↵ ")
}

// clip cuts s to at most w cells, adding … when it had to.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.Width(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := ansi.RuneWidth(r)
		if used+rw > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

type key struct {
	name string
	r    rune
}

// parseKeys decodes a raw read into keys. Windows' VT input mode and Unix
// terminals both send xterm-style sequences.
func parseKeys(b []byte) []key {
	var out []key
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			if i+2 < len(b) && (b[i+1] == '[' || b[i+1] == 'O') {
				j := i + 2
				for j < len(b) && !(b[j] >= 0x40 && b[j] <= 0x7e) {
					j++
				}
				if j >= len(b) {
					return out
				}
				seq := string(b[i+2 : j+1])
				switch seq {
				case "A":
					out = append(out, key{name: "up"})
				case "B":
					out = append(out, key{name: "down"})
				case "5~":
					out = append(out, key{name: "pgup"})
				case "6~":
					out = append(out, key{name: "pgdn"})
				}
				i = j + 1
				continue
			}
			if i+1 == len(b) {
				out = append(out, key{name: "esc"})
			}
			i++ // Alt+key: ignore the ESC prefix
		case c == '\r' || c == '\n':
			out = append(out, key{name: "enter"})
			i++
		case c == '\t':
			out = append(out, key{name: "tab"})
			i++
		case c == 0x7f || c == 0x08:
			out = append(out, key{name: "backspace"})
			i++
		case c < 0x20:
			names := map[byte]string{0x03: "ctrl-c", 0x04: "ctrl-d", 0x07: "ctrl-g", 0x0b: "ctrl-k",
				0x0e: "ctrl-n", 0x10: "ctrl-p", 0x12: "ctrl-r", 0x15: "ctrl-u", 0x17: "ctrl-w"}
			if n, ok := names[c]; ok {
				out = append(out, key{name: n})
			}
			i++
		default:
			r, size := utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && size <= 1 {
				i++
				continue
			}
			out = append(out, key{name: "char", r: r})
			i += size
		}
	}
	return out
}
