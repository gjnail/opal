package ui

import (
	"image"
	"regexp"
	"sort"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"

	"opal/terminal/internal/vt"
)

// Quick select labels the things you usually want to copy out of a
// terminal (URLs, paths, hashes, addresses) so a couple of keystrokes copy
// one without touching the mouse.

var quickPatterns = regexp.MustCompile(strings.Join([]string{
	`(?:https?|ftp|file|ssh|git)://[^\s<>"'` + "`" + `]+`,
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`,
	`\b[0-9a-f]{7,40}\b`,
	`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`,
	`(?:~|\.{1,2}|[A-Za-z]:)?(?:[/\\][\w.@+-]+){2,}[/\\]?`,
	`[\w.+-]+@[\w-]+\.[\w.-]+`,
}, "|"))

// labelAlphabet puts home-row keys first.
const labelAlphabet = "asdfghjklqwertyuiopzxcvbnm"

type quickMatch struct {
	vt.Match
	text  string
	label string
}

type quickSelectOverlay struct {
	pane    *Pane
	matches []quickMatch
	typed   string
}

// labels makes n distinct labels, one letter while they fit and two
// letters after, never letting a one-letter label prefix a two-letter one.
func labels(n int) []string {
	a := labelAlphabet
	if n <= len(a) {
		out := make([]string, n)
		for i := range out {
			out[i] = a[i : i+1]
		}
		return out
	}
	// k single letters leave len(a)-k letters to start pairs with; use as
	// many single letters as still leaves room for everything.
	k := len(a)
	for k > 0 && k+(len(a)-k)*len(a) < n {
		k--
	}
	var out []string
	for i := 0; i < k && len(out) < n; i++ {
		out = append(out, a[i:i+1])
	}
	for i := k; i < len(a) && len(out) < n; i++ {
		for j := 0; j < len(a) && len(out) < n; j++ {
			out = append(out, a[i:i+1]+a[j:j+1])
		}
	}
	return out
}

func (w *Window) openQuickSelect(p *Pane) {
	p.term.Lock()
	top := p.viewTopRow()
	first := p.term.FirstAbs() + int64(top)
	ms := p.term.SearchRange(quickPatterns, first, first+int64(p.rows)-1, 500)
	var out []quickMatch
	for _, m := range ms {
		text := strings.TrimRight(p.term.Text(m.Start, m.End, false), ".,;:)]}'\"")
		if len([]rune(text)) < 4 {
			continue
		}
		out = append(out, quickMatch{Match: m, text: text})
	}
	p.term.Unlock()
	if len(out) == 0 {
		w.addToast("Nothing to select on screen")
		return
	}
	// Nearest the bottom gets the shortest labels: that's usually what
	// you just ran.
	sort.SliceStable(out, func(i, j int) bool { return out[j].Start.Before(out[i].Start) })
	for i, l := range labels(len(out)) {
		out[i].label = l
	}
	w.overlay = &quickSelectOverlay{pane: p, matches: out}
}

func (qs *quickSelectOverlay) key(w *Window, e key.Event) {
	if e.Name == key.NameEscape {
		w.overlay = nil
		return
	}
	if e.Name == key.NameDeleteBackward && qs.typed != "" {
		qs.typed = qs.typed[:len(qs.typed)-1]
	}
}

func (qs *quickSelectOverlay) text(w *Window, s string) {
	for _, r := range s {
		upper := r >= 'A' && r <= 'Z'
		qs.typed += strings.ToLower(string(r))
		var prefix bool
		for _, m := range qs.matches {
			if m.label == qs.typed {
				w.overlay = nil
				w.pendingCopy = m.text
				if upper {
					// Uppercase also pastes it at the prompt.
					qs.pane.paste(m.text)
				}
				return
			}
			if strings.HasPrefix(m.label, qs.typed) {
				prefix = true
			}
		}
		if !prefix {
			qs.typed = ""
		}
	}
}

func (qs *quickSelectOverlay) draw(gtx layout.Context, w *Window, size image.Point) {
	p := qs.pane
	c := w.chrome
	m := w.rend.Metrics()
	p.term.Lock()
	top := p.viewTopRow()
	viewFirst := p.term.FirstAbs() + int64(top)
	p.term.Unlock()
	fillRect(gtx, p.rect, c.shade)
	for _, mt := range qs.matches {
		if !strings.HasPrefix(mt.label, qs.typed) {
			continue
		}
		// Underline the match on every row it covers.
		for row := mt.Start.Row; row <= mt.End.Row; row++ {
			y := int(row - viewFirst)
			if y < 0 || y >= p.rows {
				continue
			}
			c0, c1 := 0, p.cols
			if row == mt.Start.Row {
				c0 = mt.Start.Col
			}
			if row == mt.End.Row {
				c1 = mt.End.Col + 1
			}
			yy := p.grid.Y + y*m.CellH
			fillRect(gtx, image.Rect(p.grid.X+c0*m.CellW, yy, p.grid.X+c1*m.CellW, yy+m.CellH), c.searchHit)
		}
		y := int(mt.Start.Row - viewFirst)
		if y < 0 || y >= p.rows {
			continue
		}
		l := w.label(mt.label, c.activeRGB, c.accentRGB, true, 3)
		pt := image.Pt(p.grid.X+mt.Start.Col*m.CellW, p.grid.Y+y*m.CellH)
		fillRect(gtx, image.Rectangle{Min: pt, Max: pt.Add(l.size)}, c.accent)
		drawLabel(gtx, l, pt, 0)
	}
}
