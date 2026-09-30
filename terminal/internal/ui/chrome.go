package ui

import (
	"image"
	"image/color"
	"strings"
	"time"
	"unicode"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"opal/terminal/internal/render"
	"opal/terminal/internal/vt"
)

// chromeColors are the UI's colors, derived from the terminal palette so
// the tab bar and overlays follow the theme.
type chromeColors struct {
	bg, bar, tabHover, tabActive color.NRGBA
	text, textDim                color.NRGBA
	accent                       color.NRGBA
	gradient                     []color.NRGBA
	divider, dividerActive       color.NRGBA
	ok, bad, dim                 color.NRGBA
	scrollThumb                  color.NRGBA
	searchHit, searchCurrent     color.NRGBA
	panel, panelBorder, panelSel color.NRGBA
	shade                        color.NRGBA

	// RGB versions for text labels.
	textRGB, dimRGB, barRGB, activeRGB, panelRGB, selRGB, accentRGB vt.RGB
}

func nrgba(c vt.RGB, a uint8) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: a} }

func newChrome(pal vt.Palette, accent vt.RGB, gradient []vt.RGB) *chromeColors {
	bg, fg := pal.Background, pal.Foreground
	dark := bg.Luma() < 0.5
	shift := func(t float64) vt.RGB {
		if dark {
			return bg.Mix(vt.RGB{R: 255, G: 255, B: 255}, t)
		}
		return bg.Mix(vt.RGB{}, t)
	}
	c := &chromeColors{
		bg:            nrgba(bg, 255),
		bar:           nrgba(shift(0.05), 255),
		tabHover:      nrgba(shift(0.09), 255),
		tabActive:     nrgba(bg, 255),
		text:          nrgba(fg, 255),
		textDim:       nrgba(fg.Mix(bg, 0.45), 255),
		accent:        nrgba(accent, 255),
		divider:       nrgba(shift(0.12), 255),
		dividerActive: nrgba(accent.Mix(bg, 0.3), 255),
		ok:            nrgba(pal.ANSI[2], 255),
		bad:           nrgba(pal.ANSI[1], 255),
		dim:           nrgba(fg.Mix(bg, 0.6), 255),
		scrollThumb:   nrgba(fg, 70),
		searchHit:     nrgba(pal.ANSI[3], 70),
		searchCurrent: nrgba(accent, 120),
		panel:         nrgba(shift(0.08), 255),
		panelBorder:   nrgba(shift(0.18), 255),
		panelSel:      nrgba(accent.Mix(shift(0.08), 0.75), 255),
		shade:         color.NRGBA{A: 90},
		textRGB:       fg,
		dimRGB:        fg.Mix(bg, 0.45),
		barRGB:        shift(0.05),
		activeRGB:     bg,
		panelRGB:      shift(0.08),
		selRGB:        accent.Mix(shift(0.08), 0.75),
		accentRGB:     accent,
	}
	for _, g := range gradient {
		c.gradient = append(c.gradient, nrgba(g, 255))
	}
	if len(c.gradient) == 0 {
		c.gradient = []color.NRGBA{c.accent}
	}
	return c
}

type labelKey struct {
	text     string
	fg, bg   vt.RGB
	bold     bool
	maxCells int
}

type label struct {
	op    paint.ImageOp
	size  image.Point
	cells int
	used  uint64
}

// sanitize drops control characters from text we draw in the chrome.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// label renders a string with the terminal font, truncated with an
// ellipsis to maxCells.
func (w *Window) label(s string, fg, bg vt.RGB, bold bool, maxCells int) *label {
	k := labelKey{s, fg, bg, bold, maxCells}
	if l, ok := w.labels[k]; ok {
		l.used = w.frameNo
		return l
	}
	s = sanitize(s)
	width := func(s string) int {
		t := vt.New(vt.Options{Cols: 512, Rows: 1, GraphemeClustering: true})
		t.WriteString(s)
		x, _ := t.CursorPos()
		return x
	}
	if maxCells > 0 && width(s) > maxCells {
		r := []rune(s)
		for len(r) > 0 && width(string(r))+1 > maxCells {
			r = r[:len(r)-1]
		}
		s = strings.TrimRight(string(r), " ") + "…"
	}
	cols := max(1, width(s))
	t := vt.New(vt.Options{Cols: cols, Rows: 1, GraphemeClustering: true})
	t.WriteString("\x1b[?7l")
	if bold {
		t.WriteString("\x1b[1m")
	}
	t.WriteString(s)
	line := t.ScreenLine(0).Clone()
	pal := w.pal
	pal.Foreground, pal.Background = fg, bg
	w.rend.SetPalette(pal)
	size := w.rend.Size(cols)
	img := image.NewRGBA(image.Rectangle{Max: size})
	w.rend.Draw(&render.Row{Line: line, CursorX: -1}, img)
	op := paint.NewImageOp(img)
	op.Filter = paint.FilterNearest
	l := &label{op: op, size: size, cells: cols, used: w.frameNo}
	w.labels[k] = l
	if len(w.labels) > 512 {
		for k, v := range w.labels {
			if v.used+60 < w.frameNo {
				delete(w.labels, k)
			}
		}
	}
	return l
}

// drawLabel paints a label with its top-left at pt, clipped to maxW.
func drawLabel(gtx layout.Context, l *label, pt image.Point, maxW int) {
	w := l.size.X
	if maxW > 0 && w > maxW {
		w = maxW
	}
	off := op.Offset(pt).Push(gtx.Ops)
	cl := clip.Rect{Max: image.Pt(w, l.size.Y)}.Push(gtx.Ops)
	l.op.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	off.Pop()
}

// Tab bar.

type tabBarState struct {
	hoverTab   *Tab
	hoverClose *Tab
	dragTab    *Tab
	dragX      float32
	plusTag    int
	barTag     int
	lastClick  time.Time
	plusHover  bool
}

func (w *Window) tabBarHeight() int {
	m := w.rend.Metrics()
	return m.CellH + 2*max(w.dp(5), m.CellH/3)
}

// drawTabBar lays out and paints the tabs, and handles their clicks.
func (w *Window) drawTabBar(gtx layout.Context, r image.Rectangle) {
	c := w.chrome
	m := w.rend.Metrics()
	fillRect(gtx, r, c.bar)

	// Empty space: double-click opens a tab.
	bar := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &w.tabBar.barTag)
	bar.Pop()
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &w.tabBar.barTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			if time.Since(w.tabBar.lastClick) < 400*time.Millisecond {
				w.runAction("new_tab")
			}
			w.tabBar.lastClick = time.Now()
		}
	}

	pad := w.dp(6)
	plusW := m.CellW*3 + pad
	avail := r.Dx() - 2*pad - plusW
	n := len(w.tabs)
	maxTab := 32 * m.CellW
	minTab := 8 * m.CellW
	tabW := maxTab
	if n > 0 && n*tabW > avail {
		tabW = max(minTab, avail/n)
	}
	x := r.Min.X + pad
	h := r.Dy()
	for i, t := range w.tabs {
		tr := image.Rect(x, r.Min.Y+w.dp(4), x+tabW-w.dp(2), r.Max.Y)
		t.barRect = tr
		w.drawTab(gtx, t, i == w.active, tr)
		x += tabW
	}

	// "+" button.
	pr := image.Rect(x, r.Min.Y+w.dp(4), x+m.CellW*3, r.Max.Y-w.dp(2))
	hover := w.tabBar.hoverTab == nil && w.tabBar.plusHover
	if hover {
		fillRect(gtx, pr, c.tabHover)
	}
	l := w.label("+", c.dimRGB, w.bgFor(hover, false), false, 2)
	drawLabel(gtx, l, image.Pt(pr.Min.X+(pr.Dx()-l.size.X)/2, pr.Min.Y+(pr.Dy()-l.size.Y)/2), 0)
	area := clip.Rect(pr).Push(gtx.Ops)
	event.Op(gtx.Ops, &w.tabBar.plusTag)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &w.tabBar.plusTag, Kinds: pointer.Press | pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, _ := ev.(pointer.Event)
		switch e.Kind {
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonSecondary) {
				w.openPalette("new tab: ")
			} else {
				w.runAction("new_tab")
			}
		case pointer.Enter:
			w.tabBar.plusHover = true
		case pointer.Leave:
			w.tabBar.plusHover = false
		}
	}
	_ = h
}

func (w *Window) bgFor(hover, active bool) vt.RGB {
	c := w.chrome
	switch {
	case active:
		return c.activeRGB
	case hover:
		return vt.RGB{R: c.tabHover.R, G: c.tabHover.G, B: c.tabHover.B}
	}
	return c.barRGB
}

func (w *Window) drawTab(gtx layout.Context, t *Tab, active bool, r image.Rectangle) {
	c := w.chrome
	m := w.rend.Metrics()
	hover := w.tabBar.hoverTab == t
	bg := c.bar
	switch {
	case active:
		bg = c.tabActive
	case hover:
		bg = c.tabHover
	}
	fillRect(gtx, r, bg)
	if active {
		// The theme's gradient runs along the top of the active tab.
		gradientBar(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+max(2, w.dp(2))), c.gradient)
	}

	p := t.focus
	title := t.title
	if title == "" && p != nil {
		title = p.title()
	}
	if len(t.panes()) > 1 {
		title = "⊞ " + title
	}
	fg := c.dimRGB
	if active {
		fg = c.textRGB
	}
	pad := m.CellW
	x := r.Min.X + pad
	// Status dot: bell, finished-with-error, or background activity.
	if p != nil {
		var dot color.NRGBA
		switch {
		case time.Since(p.bellAt) < 5*time.Second && !active:
			dot = c.bad
		case p.exited.Load():
			dot = c.dim
		case p.activity && !active:
			dot = c.accent
		}
		if dot.A != 0 {
			d := max(4, m.CellH/4)
			cy := r.Min.Y + r.Dy()/2
			fillRect(gtx, image.Rect(x, cy-d/2, x+d, cy+d/2+d%2), dot)
			x += d + m.CellW/2
		}
	}
	closeW := 2 * m.CellW
	maxCells := max(1, (r.Max.X-x-closeW-pad)/m.CellW)
	l := w.label(title, fg, w.bgFor(hover, active), active, maxCells)
	drawLabel(gtx, l, image.Pt(x, r.Min.Y+(r.Dy()-l.size.Y)/2), r.Max.X-x-closeW)

	// Progress (OSC 9;4) as a thin bar along the bottom.
	if p != nil && p.progress.State != vt.ProgressNone {
		col := c.accent
		switch p.progress.State {
		case vt.ProgressError:
			col = c.bad
		case vt.ProgressPaused:
			col = c.searchHit
		}
		pw := r.Dx() * p.progress.Percent / 100
		if p.progress.State == vt.ProgressIndeterminate {
			phase := int(time.Now().UnixMilli()/8) % (r.Dx() * 2)
			x0 := r.Min.X + phase - r.Dx()/2
			fillRect(gtx, image.Rect(max(r.Min.X, x0), r.Max.Y-w.dp(2), min(r.Max.X, x0+r.Dx()/3), r.Max.Y), col)
			gtx.Execute(op.InvalidateCmd{At: time.Now().Add(30 * time.Millisecond)})
		} else {
			fillRect(gtx, image.Rect(r.Min.X, r.Max.Y-w.dp(2), r.Min.X+pw, r.Max.Y), col)
		}
	}

	// Close button, shown on the active or hovered tab.
	cr := image.Rect(r.Max.X-closeW-pad/2, r.Min.Y, r.Max.X-pad/2, r.Max.Y)
	if active || hover {
		cfg := c.dimRGB
		if w.tabBar.hoverClose == t {
			fillRect(gtx, image.Rect(cr.Min.X, cr.Min.Y+w.dp(4), cr.Max.X, cr.Max.Y-w.dp(4)), c.tabHover)
			cfg = c.textRGB
		}
		cl := w.label("×", cfg, w.bgFor(hover || w.tabBar.hoverClose == t, active && w.tabBar.hoverClose != t), false, 1)
		drawLabel(gtx, cl, image.Pt(cr.Min.X+(cr.Dx()-cl.size.X)/2, cr.Min.Y+(cr.Dy()-cl.size.Y)/2), 0)
	}

	// Input: the body selects (middle-click closes, drag reorders); the
	// close button closes.
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	area.Pop()
	carea := clip.Rect(cr).Push(gtx.Ops)
	event.Op(gtx.Ops, &t.closeTag)
	pointer.CursorPointer.Add(gtx.Ops)
	carea.Pop()

	for {
		ev, ok := gtx.Event(pointer.Filter{Target: t, Kinds: pointer.Press | pointer.Release | pointer.Drag | pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, _ := ev.(pointer.Event)
		switch e.Kind {
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonTertiary) {
				w.closeTab(t)
				continue
			}
			w.activateTab(t)
			w.tabBar.dragTab = t
			w.tabBar.dragX = e.Position.X
		case pointer.Drag:
			if w.tabBar.dragTab == t {
				w.dragTab(t, e.Position.X+float32(r.Min.X))
			}
		case pointer.Release:
			w.tabBar.dragTab = nil
		case pointer.Enter:
			w.tabBar.hoverTab = t
		case pointer.Leave:
			if w.tabBar.hoverTab == t {
				w.tabBar.hoverTab = nil
			}
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &t.closeTag, Kinds: pointer.Press | pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, _ := ev.(pointer.Event)
		switch e.Kind {
		case pointer.Press:
			w.closeTab(t)
		case pointer.Enter:
			w.tabBar.hoverClose = t
		case pointer.Leave:
			if w.tabBar.hoverClose == t {
				w.tabBar.hoverClose = nil
			}
		}
	}
}

// dragTab moves t when it's dragged past a neighbour's midpoint.
func (w *Window) dragTab(t *Tab, x float32) {
	i := w.tabIndex(t)
	if i < 0 {
		return
	}
	if i > 0 {
		prev := w.tabs[i-1].barRect
		if x < float32(prev.Min.X+prev.Dx()/2) {
			w.tabs[i-1], w.tabs[i] = w.tabs[i], w.tabs[i-1]
			w.active = i - 1
			return
		}
	}
	if i < len(w.tabs)-1 {
		next := w.tabs[i+1].barRect
		if x > float32(next.Min.X+next.Dx()/2) {
			w.tabs[i+1], w.tabs[i] = w.tabs[i], w.tabs[i+1]
			w.active = i + 1
		}
	}
}

// gradientBar fills r with a horizontal gradient through stops.
func gradientBar(gtx layout.Context, r image.Rectangle, stops []color.NRGBA) {
	if len(stops) == 1 {
		fillRect(gtx, r, stops[0])
		return
	}
	segs := len(stops) - 1
	for i := 0; i < segs; i++ {
		x0 := r.Min.X + r.Dx()*i/segs
		x1 := r.Min.X + r.Dx()*(i+1)/segs
		cl := clip.Rect(image.Rect(x0, r.Min.Y, x1, r.Max.Y)).Push(gtx.Ops)
		paint.LinearGradientOp{
			Stop1:  f32pt(x0, 0),
			Color1: stops[i],
			Stop2:  f32pt(x1, 0),
			Color2: stops[i+1],
		}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		cl.Pop()
	}
}
