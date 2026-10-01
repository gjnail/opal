package ui

import (
	"fmt"
	"image"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"opal/internal/history"
	"opal/internal/jump"
	"opal/terminal/internal/vt"
)

// overlay is a modal panel (command palette, find, rename) that takes the
// keyboard while it's open.
type overlay interface {
	key(w *Window, e key.Event)
	text(w *Window, s string)
	draw(gtx layout.Context, w *Window, size image.Point)
}

func (w *Window) drawOverlay(gtx layout.Context, size image.Point) {
	if w.overlay != nil {
		w.overlay.draw(gtx, w, size)
	}
}

// lineEdit is a one-line text field.
type lineEdit struct {
	buf []rune
	cur int
}

func (e *lineEdit) String() string { return string(e.buf) }

func (e *lineEdit) set(s string) {
	e.buf = []rune(s)
	e.cur = len(e.buf)
}

func (e *lineEdit) insert(s string) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	r := []rune(s)
	e.buf = append(e.buf[:e.cur], append(r, e.buf[e.cur:]...)...)
	e.cur += len(r)
}

// key edits the text; it reports whether the text changed.
func (e *lineEdit) key(k key.Event) bool {
	ctrl := k.Modifiers.Contain(key.ModCtrl) || k.Modifiers.Contain(key.ModAlt)
	switch k.Name {
	case key.NameLeftArrow:
		if ctrl {
			e.cur = e.wordLeft()
		} else if e.cur > 0 {
			e.cur--
		}
	case key.NameRightArrow:
		if ctrl {
			e.cur = e.wordRight()
		} else if e.cur < len(e.buf) {
			e.cur++
		}
	case key.NameHome:
		e.cur = 0
	case key.NameEnd:
		e.cur = len(e.buf)
	case key.NameDeleteBackward:
		if e.cur == 0 {
			return false
		}
		from := e.cur - 1
		if ctrl {
			from = e.wordLeft()
		}
		e.buf = append(e.buf[:from], e.buf[e.cur:]...)
		e.cur = from
		return true
	case key.NameDeleteForward:
		if e.cur >= len(e.buf) {
			return false
		}
		to := e.cur + 1
		if ctrl {
			to = e.wordRight()
		}
		e.buf = append(e.buf[:e.cur], e.buf[to:]...)
		return true
	}
	return false
}

func (e *lineEdit) wordLeft() int {
	i := e.cur
	for i > 0 && e.buf[i-1] == ' ' {
		i--
	}
	for i > 0 && e.buf[i-1] != ' ' {
		i--
	}
	return i
}

func (e *lineEdit) wordRight() int {
	i := e.cur
	for i < len(e.buf) && e.buf[i] == ' ' {
		i++
	}
	for i < len(e.buf) && e.buf[i] != ' ' {
		i++
	}
	return i
}

// drawField draws the text with a bar cursor.
func (w *Window) drawField(gtx layout.Context, e *lineEdit, pt image.Point, maxCells int, placeholder string) {
	c := w.chrome
	m := w.rend.Metrics()
	s := e.String()
	fg := c.textRGB
	if s == "" && placeholder != "" {
		s, fg = placeholder, c.dimRGB
	}
	l := w.label(s, fg, c.panelRGB, false, maxCells)
	drawLabel(gtx, l, pt, 0)
	// Cursor position: width of the text before it.
	before := string(e.buf[:e.cur])
	cx := 0
	if before != "" {
		cx = w.label(before, c.textRGB, c.panelRGB, false, maxCells).size.X
	}
	fillRect(gtx, image.Rect(pt.X+cx, pt.Y+1, pt.X+cx+max(2, w.dp(2)), pt.Y+m.CellH-1), c.accent)
}

// panelRect draws a panel with a border and returns its inner rectangle.
func (w *Window) panel(gtx layout.Context, r image.Rectangle) {
	c := w.chrome
	fillRect(gtx, r.Inset(-w.dp(1)), c.panelBorder)
	fillRect(gtx, r, c.panel)
}

// Command palette.

type paletteItem struct {
	id, title, keys string
	score           int
	// run overrides running id as an action; shift is Shift+Enter.
	run func(w *Window, shift bool)
}

// The palette searches actions and the shell's aliases and functions by
// default; "$" searches only the latter, "!" Opal's shared command history
// and "@" its frecent directories.
type paletteOverlay struct {
	edit    lineEdit
	all     []paletteItem
	cmds    []paletteItem // the shell's part of all
	items   []paletteItem
	sel     int
	tags    [16]int
	cwd     string
	history []history.Command
	dirs    []jump.Entry
	loaded  map[byte]bool
}

func (w *Window) openPalette(query string) {
	po := &paletteOverlay{loaded: map[byte]bool{}}
	for _, a := range w.actionList() {
		ks := keysFor(w.keys, a.id)
		po.all = append(po.all, paletteItem{id: a.id, title: a.title, keys: strings.Join(ks, "  ")})
	}
	if p := w.activePane(); p != nil {
		po.cwd = p.currentDir()
		po.cmds = shellItems(p.shellKind())
		po.all = append(po.all, po.cmds...)
	}
	po.edit.set(query)
	po.filter()
	w.overlay = po
	w.invalidate()
}

// ago renders a time as "5m ago".
func ago(unix int64) string {
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func (po *paletteOverlay) historyItems(q string) []paletteItem {
	if !po.loaded['!'] {
		po.loaded['!'] = true
		if entries, err := history.Load(); err == nil {
			po.history = history.Unique(entries)
		}
	}
	cmds := po.history
	if q != "" {
		cmds = history.Search(cmds, q, history.AllShells, po.cwd, "")
	}
	var out []paletteItem
	for _, c := range cmds {
		if len(out) >= 300 {
			break
		}
		cmd := c.Cmd
		keys := ago(c.When)
		if c.Status != 0 {
			keys = fmt.Sprintf("exit %d  %s", c.Status, keys)
		}
		out = append(out, paletteItem{
			title: strings.ReplaceAll(cmd, "\n", " ⏎ "),
			keys:  keys,
			run: func(w *Window, shift bool) {
				p := w.activePane()
				if p == nil {
					return
				}
				// Paste so the shell treats it as typed text; Shift+Enter
				// also runs it.
				p.paste(cmd)
				if shift {
					p.send([]byte{'\r'})
				}
			},
		})
	}
	return out
}

func (po *paletteOverlay) dirItems(q string) []paletteItem {
	if !po.loaded['@'] {
		po.loaded['@'] = true
		po.dirs, _ = jump.Ranked()
	}
	home, _ := os.UserHomeDir()
	words := strings.Fields(q)
	var out []paletteItem
	for _, d := range po.dirs {
		if len(words) > 0 && !jump.Match(d.Path, words) {
			continue
		}
		path := d.Path
		title := path
		if home != "" && strings.HasPrefix(strings.ToLower(path), strings.ToLower(home)) {
			title = "~" + path[len(home):]
		}
		out = append(out, paletteItem{
			title: title,
			keys:  "new tab",
			run: func(w *Window, shift bool) {
				prof := w.app.cfg.DefaultProfile()
				if p := w.activePane(); p != nil && p.profile.Name != "error" {
					prof = p.profile
				}
				w.newTab(prof, path, w.lastSize)
			},
		})
		if len(out) >= 300 {
			break
		}
	}
	return out
}

// fuzzyScore matches query as a subsequence of s, rewarding runs and word
// starts. -1 means no match.
func fuzzyScore(query, s string) int {
	if query == "" {
		return 0
	}
	q := []rune(strings.ToLower(query))
	t := []rune(strings.ToLower(s))
	score, qi, run := 0, 0, 0
	for i := 0; i < len(t) && qi < len(q); i++ {
		if t[i] != q[qi] {
			run = 0
			continue
		}
		run++
		score += 1 + run*2
		if i == 0 || t[i-1] == ' ' || t[i-1] == ':' {
			score += 6
		}
		qi++
	}
	if qi < len(q) {
		return -1
	}
	return score - len(t)/8
}

func (po *paletteOverlay) filter() {
	q := strings.TrimSpace(po.edit.String())
	po.sel = 0
	if rest, ok := strings.CutPrefix(q, "!"); ok {
		po.items = po.historyItems(strings.TrimSpace(rest))
		return
	}
	if rest, ok := strings.CutPrefix(q, "@"); ok {
		po.items = po.dirItems(strings.TrimSpace(rest))
		return
	}
	from := po.all
	if rest, ok := strings.CutPrefix(q, "$"); ok {
		from, q = po.cmds, strings.TrimSpace(rest)
	}
	po.items = po.items[:0]
	for _, it := range from {
		s := fuzzyScore(q, it.title)
		if s < 0 {
			continue
		}
		it.score = s
		po.items = append(po.items, it)
	}
	if q != "" {
		sort.SliceStable(po.items, func(i, j int) bool { return po.items[i].score > po.items[j].score })
	}
	po.sel = 0
}

func (po *paletteOverlay) key(w *Window, e key.Event) {
	switch e.Name {
	case key.NameEscape:
		w.overlay = nil
	case key.NameUpArrow:
		if po.sel > 0 {
			po.sel--
		}
	case key.NameDownArrow:
		if po.sel < len(po.items)-1 {
			po.sel++
		}
	case key.NameReturn, key.NameEnter:
		if po.sel < len(po.items) {
			it := po.items[po.sel]
			w.overlay = nil
			if it.run != nil {
				it.run(w, e.Modifiers.Contain(key.ModShift))
			} else {
				w.runAction(it.id)
			}
		}
	case key.NamePageDown:
		po.sel = min(po.sel+len(po.tags), max(0, len(po.items)-1))
	case key.NamePageUp:
		po.sel = max(po.sel-len(po.tags), 0)
	default:
		if po.edit.key(e) {
			po.filter()
		}
	}
}

func (po *paletteOverlay) text(w *Window, s string) {
	po.edit.insert(s)
	po.filter()
}

func (po *paletteOverlay) draw(gtx layout.Context, w *Window, size image.Point) {
	c := w.chrome
	m := w.rend.Metrics()
	cells := min(90, (size.X-w.dp(40))/m.CellW)
	width := cells * m.CellW
	pad := m.CellW
	rowH := m.CellH + w.dp(6)
	n := min(len(po.items), len(po.tags))
	x0 := (size.X - width) / 2
	y0 := w.tabBarHeight() + w.dp(12)
	h := rowH + w.dp(8) + n*rowH + w.dp(8)
	r := image.Rect(x0, y0, x0+width, y0+h)
	w.panel(gtx, r)

	w.drawField(gtx, &po.edit, image.Pt(x0+pad, y0+w.dp(6)), cells-2, "Type a command  (! history, @ directories, $ aliases)")
	fillRect(gtx, image.Rect(x0, y0+rowH+w.dp(3), x0+width, y0+rowH+w.dp(4)), c.panelBorder)

	// Keep the selection in view.
	first := 0
	if po.sel >= n {
		first = po.sel - n + 1
	}
	for i := 0; i < n && first+i < len(po.items); i++ {
		it := po.items[first+i]
		ry := y0 + rowH + w.dp(8) + i*rowH
		rr := image.Rect(x0+w.dp(4), ry, x0+width-w.dp(4), ry+rowH)
		bg := c.panelRGB
		if first+i == po.sel {
			fillRect(gtx, rr, c.panelSel)
			bg = c.selRGB
		}
		keyCells := 0
		if it.keys != "" {
			kl := w.label(it.keys, c.dimRGB, bg, false, 28)
			keyCells = kl.cells + 2
			drawLabel(gtx, kl, image.Pt(rr.Max.X-pad-kl.size.X, ry+(rowH-kl.size.Y)/2), 0)
		}
		tl := w.label(it.title, c.textRGB, bg, false, cells-keyCells-3)
		drawLabel(gtx, tl, image.Pt(rr.Min.X+pad, ry+(rowH-tl.size.Y)/2), 0)

		area := clip.Rect(rr).Push(gtx.Ops)
		event.Op(gtx.Ops, &po.tags[i])
		pointer.CursorPointer.Add(gtx.Ops)
		area.Pop()
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &po.tags[i], Kinds: pointer.Press | pointer.Move})
			if !ok {
				break
			}
			if pe, ok := ev.(pointer.Event); ok {
				if pe.Kind == pointer.Press {
					w.overlay = nil
					if it.run != nil {
						it.run(w, pe.Modifiers.Contain(key.ModShift))
					} else {
						w.runAction(it.id)
					}
					return
				}
				po.sel = first + i
			}
		}
	}
}

// Find.

type searchState struct {
	matches []vt.Match
	current int
}

type searchOverlay struct {
	edit          lineEdit
	pane          *Pane
	caseSensitive bool
	regex         bool
	err           string
}

func (w *Window) openSearch(p *Pane) {
	so := &searchOverlay{pane: p}
	if p.search == nil {
		p.search = &searchState{}
	}
	// Seed with the selection, like most editors.
	p.term.Lock()
	if s := p.sel.text(p.term); s != "" && !strings.Contains(s, "\n") {
		so.edit.set(s)
	}
	p.term.Unlock()
	w.overlay = so
	so.update(w)
}

func (so *searchOverlay) update(w *Window) {
	p := so.pane
	q := so.edit.String()
	so.err = ""
	if q == "" {
		p.search.matches = nil
		return
	}
	pat := q
	if !so.regex {
		pat = regexp.QuoteMeta(q)
	}
	if !so.caseSensitive {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		so.err = "bad pattern"
		p.search.matches = nil
		return
	}
	p.term.Lock()
	p.search.matches = p.term.Search(re, 10000)
	p.term.Unlock()
	// Start at the most recent match.
	p.search.current = len(p.search.matches) - 1
	so.reveal()
}

func (so *searchOverlay) reveal() {
	s := so.pane.search
	if s.current >= 0 && s.current < len(s.matches) {
		so.pane.scrollToAbs(s.matches[s.current].Start.Row - int64(so.pane.rows/2) + 2)
	}
}

func (so *searchOverlay) key(w *Window, e key.Event) {
	s := so.pane.search
	switch {
	case e.Name == key.NameEscape:
		w.overlay = nil
		so.pane.search = nil
	case e.Name == key.NameReturn || e.Name == key.NameEnter || e.Name == key.NameF3:
		if len(s.matches) == 0 {
			return
		}
		// Enter goes up (older), Shift+Enter down, like searching back
		// through a log.
		if e.Modifiers.Contain(key.ModShift) {
			s.current = (s.current + 1) % len(s.matches)
		} else {
			s.current = (s.current - 1 + len(s.matches)) % len(s.matches)
		}
		so.reveal()
	case e.Name == "C" && e.Modifiers.Contain(key.ModAlt):
		so.caseSensitive = !so.caseSensitive
		so.update(w)
	case e.Name == "R" && e.Modifiers.Contain(key.ModAlt):
		so.regex = !so.regex
		so.update(w)
	default:
		if so.edit.key(e) {
			so.update(w)
		}
	}
}

func (so *searchOverlay) text(w *Window, s string) {
	so.edit.insert(s)
	so.update(w)
}

func (so *searchOverlay) draw(gtx layout.Context, w *Window, size image.Point) {
	c := w.chrome
	m := w.rend.Metrics()
	p := so.pane
	cells := 44
	width := cells * m.CellW
	rowH := m.CellH + w.dp(10)
	x1 := p.rect.Max.X - w.dp(20)
	x0 := max(p.rect.Min.X+w.dp(8), x1-width)
	y0 := p.rect.Min.Y + w.dp(8)
	r := image.Rect(x0, y0, x1, y0+rowH)
	w.panel(gtx, r)

	status := ""
	switch {
	case so.err != "":
		status = so.err
	case so.edit.String() == "":
	case len(p.search.matches) == 0:
		status = "no results"
	default:
		status = fmt.Sprintf("%d/%d", len(p.search.matches)-p.search.current, len(p.search.matches))
	}
	opts := ""
	if so.caseSensitive {
		opts += " Aa"
	}
	if so.regex {
		opts += " .*"
	}
	right := strings.TrimSpace(status + "  " + opts)
	rl := w.label(right, c.dimRGB, c.panelRGB, false, 18)
	drawLabel(gtx, rl, image.Pt(r.Max.X-m.CellW-rl.size.X, y0+(rowH-rl.size.Y)/2), 0)
	fieldCells := (r.Dx()-rl.size.X)/m.CellW - 4
	w.drawField(gtx, &so.edit, image.Pt(x0+m.CellW, y0+(rowH-m.CellH)/2), fieldCells, "Find (Alt+C case, Alt+R regex)")
}

// Rename tab.

type renameOverlay struct {
	edit lineEdit
	tab  *Tab
}

func (w *Window) openRename(t *Tab) {
	ro := &renameOverlay{tab: t}
	title := t.title
	if title == "" && t.focus != nil {
		title = t.focus.title()
	}
	ro.edit.set(title)
	w.overlay = ro
}

func (ro *renameOverlay) key(w *Window, e key.Event) {
	switch e.Name {
	case key.NameEscape:
		w.overlay = nil
	case key.NameReturn, key.NameEnter:
		ro.tab.title = strings.TrimSpace(ro.edit.String())
		w.overlay = nil
	default:
		ro.edit.key(e)
	}
}

func (ro *renameOverlay) text(w *Window, s string) { ro.edit.insert(s) }

func (ro *renameOverlay) draw(gtx layout.Context, w *Window, size image.Point) {
	m := w.rend.Metrics()
	cells := min(50, (size.X-w.dp(40))/m.CellW)
	width := cells * m.CellW
	rowH := m.CellH + w.dp(10)
	x0 := (size.X - width) / 2
	y0 := w.tabBarHeight() + w.dp(12)
	w.panel(gtx, image.Rect(x0, y0, x0+width, y0+rowH))
	w.drawField(gtx, &ro.edit, image.Pt(x0+m.CellW, y0+(rowH-m.CellH)/2), cells-2, "Tab name (empty for automatic)")
}
