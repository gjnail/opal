package ui

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"opal/terminal/internal/fonts"
	"opal/terminal/internal/notify"
	"opal/terminal/internal/render"
	"opal/terminal/internal/settings"
	"opal/terminal/internal/vt"
)

// Window is one top-level window with its tabs.
type Window struct {
	app *App
	gw  *app.Window

	tabs   []*Tab
	active int

	fonts   *fonts.Set
	rend    *render.Renderer
	fontPt  float64
	pxPerDp float32
	// The settings the loaded fonts were made for.
	fontDp       float32
	fontPtLoaded float64
	pal          vt.Palette
	chrome       *chromeColors
	labels       map[labelKey]*label
	keys         map[string]string
	frameNo      uint64
	tabBar       tabBarState
	overlay      overlay
	toasts       []toast
	focused      bool
	gotFocus     bool
	mode         app.WindowMode
	title        string

	blinkOn    bool
	blinkStart time.Time

	pasteTarget *Pane
	pendingKey  *key.Event // Ctrl+Alt: might be AltGr typing a character
	dragSplit   *node
	pending     atomic.Bool
	startDir    string
	startProf   *settings.Profile
	firstErr    string
	lastSize    image.Point
	lastCaret   f32.Point
	pendingCopy string
	// rowsRendered counts row cache misses in the current frame.
	rowsRendered int
	// restore holds a saved window to rebuild on the first frame.
	restore             *sessionWindow
	openSettingsOnStart bool
	// closing is set once the last tab closed and the window is going away.
	closing   bool
	hwnd      uintptr // native window, where the platform has one
	wantPaste bool
}

type toast struct {
	text  string
	until time.Time
}

func newWindow(a *App, prof *settings.Profile, dir string) *Window {
	w := &Window{
		app:       a,
		labels:    map[labelKey]*label{},
		keys:      keymap(a.cfg.Keys),
		fontPt:    a.cfg.FontSize,
		blinkOn:   true,
		startDir:  dir,
		startProf: prof,
	}
	w.pal = a.cfg.Palette()
	w.chrome = newChrome(w.pal, a.cfg.Accent(), a.cfg.Gradient())
	return w
}

// invalidate asks for a new frame; safe from any goroutine.
func (w *Window) invalidate() {
	if w.gw != nil {
		w.gw.Invalidate()
	}
}

func (w *Window) dp(v float32) int {
	return int(v*w.pxPerDp + 0.5)
}

func f32pt(x, y int) f32.Point { return f32.Pt(float32(x), float32(y)) }

// pxPerPt converts font points to pixels: points are 1/72 inch on
// Windows and Linux (96 dpi logical), and one logical pixel on macOS.
func (w *Window) pxPerPt() float32 {
	if runtime.GOOS == "darwin" {
		return w.pxPerDp
	}
	return w.pxPerDp * 96 / 72
}

// ensureFonts (re)loads fonts when the size or display density changes.
func (w *Window) ensureFonts(metric unit.Metric) {
	// Compare the inputs, not the resulting float32 pixel size: rounding
	// made that differ every frame, rebuilding the fonts and every cached
	// row each time.
	if w.fonts != nil && metric.PxPerDp == w.fontDp && w.fontPt == w.fontPtLoaded {
		return
	}
	w.pxPerDp = metric.PxPerDp
	if w.pxPerDp <= 0 {
		w.pxPerDp = 1
	}
	w.fontDp, w.fontPtLoaded = metric.PxPerDp, w.fontPt
	cfg := w.app.cfg
	f, err := fonts.New(fonts.Config{
		Families:   cfg.FontFamily,
		SizePx:     float32(w.fontPt) * w.pxPerPt(),
		LineHeight: float32(cfg.LineHeight),
		CellWidth:  float32(cfg.CellWidth),
	})
	if err != nil {
		w.firstErr = err.Error()
		return
	}
	w.fonts = f
	w.rend = render.New(f, w.pal, render.Options{BoldIsBright: cfg.BoldIsBright, MinContrast: cfg.MinContrast})
	clear(w.labels)
	m := w.rend.Metrics()
	for _, t := range w.tabs {
		for _, p := range t.panes() {
			p.term.SetCellSize(m.CellW, m.CellH)
			p.flushCache()
		}
	}
}

// run drives the window until it closes, and returns its tabs for the
// session file (nil when there were none).
func (w *Window) run() *sessionWindow {
	w.gw = new(app.Window)
	w.gw.Option(
		app.Title("Opal Terminal"),
		app.Size(unit.Dp(1100), unit.Dp(680)),
		app.MinSize(unit.Dp(360), unit.Dp(220)),
	)
	var ops op.Ops
	for {
		switch e := w.gw.Event().(type) {
		case app.DestroyEvent:
			var snap *sessionWindow
			if !w.closing && len(w.tabs) > 0 {
				snap = w.snapshot()
			}
			w.shutdown()
			return snap
		case app.ConfigEvent:
			w.mode = e.Config.Mode
		case app.ViewEvent:
			w.nativeWindow(e)
		case app.FrameEvent:
			start := time.Now()
			w.rowsRendered = 0
			gtx := app.NewContext(&ops, e)
			w.frame(gtx)
			built := time.Since(start)
			e.Frame(gtx.Ops)
			debugf("frame build=%v submit=%v rows=%d", built, time.Since(start)-built, w.rowsRendered)
		}
	}
}

func (w *Window) shutdown() {
	for _, t := range w.tabs {
		for _, p := range t.panes() {
			p.close()
		}
	}
	w.tabs = nil
}

func (w *Window) activeTab() *Tab {
	if w.active < 0 || w.active >= len(w.tabs) {
		return nil
	}
	return w.tabs[w.active]
}

func (w *Window) activePane() *Pane {
	if t := w.activeTab(); t != nil {
		return t.focus
	}
	return nil
}

func (w *Window) tabIndex(t *Tab) int {
	for i, x := range w.tabs {
		if x == t {
			return i
		}
	}
	return -1
}

func (w *Window) activateTab(t *Tab) {
	if i := w.tabIndex(t); i >= 0 {
		w.active = i
		if t.focus != nil {
			t.focus.activity = false
		}
	}
}

func (w *Window) focusPane(p *Pane) {
	for i, t := range w.tabs {
		for _, q := range t.panes() {
			if q == p {
				if t.focus != p {
					if old := t.focus; old != nil {
						old.send(old.term.FocusReport(false))
					}
					p.send(p.term.FocusReport(true))
				}
				t.focus = p
				w.active = i
				return
			}
		}
	}
}

// gridFor computes a pane's grid size for a rectangle.
func (w *Window) gridFor(r image.Rectangle) (cols, rows int) {
	m := w.rend.Metrics()
	pad := w.dp(float32(w.app.cfg.Padding))
	cols = max(2, (r.Dx()-2*pad)/m.CellW)
	rows = max(1, (r.Dy()-2*pad)/m.CellH)
	return cols, rows
}

// contentRect is where panes go, below the tab bar.
func (w *Window) contentRect(size image.Point) image.Rectangle {
	return image.Rect(0, w.tabBarHeight(), size.X, size.Y)
}

// startPane launches a profile in a pane sized for r, or a pane that
// shows why it couldn't.
func (w *Window) startPane(prof settings.Profile, dir string, r image.Rectangle) *Pane {
	return w.startPaneReplay(prof, dir, r, "")
}

// startPaneReplay is startPane with output from a previous session.
func (w *Window) startPaneReplay(prof settings.Profile, dir string, r image.Rectangle, replay string) *Pane {
	cols, rows := w.gridFor(r)
	p, err := newPane(w, prof, dir, cols, rows, replay)
	if err != nil {
		return w.errorPane(err, cols, rows)
	}
	p.rect = r
	return p
}

// errorPane is a terminal with no process that explains a failure.
func (w *Window) errorPane(err error, cols, rows int) *Pane {
	p := &Pane{win: w, cols: cols, rows: rows, cache: map[uint64]*rowImage{}, profile: settings.Profile{Name: "error"}}
	p.id = int(paneIDs.Add(1))
	p.term = vt.New(vt.Options{Cols: cols, Rows: rows, Palette: w.pal})
	p.term.WriteString("\x1b[31m" + strings.ReplaceAll(err.Error(), "\n", "\r\n") + "\x1b[0m\r\n")
	p.pty = deadPTY{}
	p.exited.Store(true)
	p.exitCode.Store(-1)
	return p
}

func (w *Window) newTab(prof settings.Profile, dir string, size image.Point) {
	p := w.startPane(prof, dir, w.contentRect(size))
	t := newTab(p)
	w.tabs = append(w.tabs, t)
	w.active = len(w.tabs) - 1
}

func (w *Window) splitPane(dir splitDir) {
	t := w.activeTab()
	p := w.activePane()
	if t == nil || p == nil {
		return
	}
	r := p.rect
	if dir == splitCols {
		r.Max.X = r.Min.X + r.Dx()/2
	} else {
		r.Max.Y = r.Min.Y + r.Dy()/2
	}
	np := w.startPane(p.profile, p.currentDir(), r)
	t.split(p, np, dir)
}

// closePane closes a pane (and its tab when it was the last one).
func (w *Window) closePane(p *Pane) {
	for i, t := range w.tabs {
		if t.find(p) == nil {
			continue
		}
		p.close()
		if t.remove(p) {
			w.removeTab(i)
		}
		return
	}
}

func (w *Window) closeTab(t *Tab) {
	i := w.tabIndex(t)
	if i < 0 {
		return
	}
	for _, p := range t.panes() {
		p.close()
	}
	w.removeTab(i)
}

func (w *Window) removeTab(i int) {
	w.tabs = append(w.tabs[:i], w.tabs[i+1:]...)
	if w.active >= len(w.tabs) {
		w.active = len(w.tabs) - 1
	} else if w.active > i {
		w.active--
	}
	if len(w.tabs) == 0 {
		w.closing = true
		w.gw.Perform(system.ActionClose)
	}
}

func (w *Window) broadcast(from *Pane, b []byte) {
	t := w.activeTab()
	if t == nil {
		return
	}
	for _, p := range t.panes() {
		if p != from {
			p.send(b)
		}
	}
}

func (w *Window) addToast(format string, args ...any) {
	w.toasts = append(w.toasts, toast{text: fmt.Sprintf(format, args...), until: time.Now().Add(2500 * time.Millisecond)})
	if len(w.toasts) > 4 {
		w.toasts = w.toasts[len(w.toasts)-4:]
	}
	w.invalidate()
}

// Clipboard.

func (w *Window) writeClipboard(gtx layout.Context, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}

func (w *Window) copySelection(p *Pane) {
	p.term.Lock()
	s := p.sel.text(p.term)
	p.term.Unlock()
	if s == "" {
		return
	}
	w.pendingCopy = s
	w.invalidate()
}

func (w *Window) requestPaste(p *Pane) {
	w.pasteTarget = p
	w.wantPaste = true
	w.invalidate()
}

// frame draws the window and handles input.
func (w *Window) frame(gtx layout.Context) {
	w.frameNo++
	w.ensureFonts(gtx.Metric)
	if w.rend == nil {
		return
	}
	size := gtx.Constraints.Max
	w.lastSize = size

	// Whole-window key input.
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, w)
	key.InputHintOp{Tag: w, Hint: key.HintAny}.Add(gtx.Ops)
	area.Pop()
	if !w.gotFocus {
		gtx.Execute(key.FocusCmd{Tag: w})
	}

	if len(w.tabs) == 0 && !w.closing {
		if w.restore != nil {
			w.restoreTabs(w.restore, w.contentRect(size))
			w.restore = nil
		}
		if len(w.tabs) == 0 {
			prof := w.app.cfg.DefaultProfile()
			if w.startProf != nil {
				prof = *w.startProf
			}
			w.newTab(prof, w.startDir, size)
		}
	}

	if w.openSettingsOnStart {
		w.openSettingsOnStart = false
		w.openSettings()
	}

	w.handleInput(gtx, size)
	if len(w.tabs) == 0 {
		return
	}
	if w.pendingCopy != "" {
		w.writeClipboard(gtx, w.pendingCopy)
		w.addToast("Copied %d characters", len([]rune(w.pendingCopy)))
		w.pendingCopy = ""
	}
	if w.wantPaste {
		gtx.Execute(clipboard.ReadCmd{Tag: w})
		w.wantPaste = false
	}
	w.processTermEvents(gtx)

	fillRect(gtx, image.Rectangle{Max: size}, w.chrome.bg)
	barH := w.tabBarHeight()
	w.drawTabBar(gtx, image.Rect(0, 0, size.X, barH))

	t := w.activeTab()
	content := w.contentRect(size)
	gap := max(1, w.dp(2))
	t.layout(content, gap)
	pad := w.dp(float32(w.app.cfg.Padding))
	for _, p := range t.panes() {
		if p.rect.Empty() {
			continue
		}
		p.grid = p.rect.Min.Add(image.Pt(pad, pad))
		cols, rows := w.gridFor(p.rect)
		p.resize(cols, rows)
		p.registerPointer(gtx)
		p.handlePointer(gtx)
		focused := p == t.focus && w.focused
		p.draw(gtx, focused)
		if len(t.panes()) > 1 && t.zoom == nil && p != t.focus {
			// Dim unfocused panes a little so the active one stands out.
			fillRect(gtx, p.rect, w.chrome.shade)
		}
		if p.broadcast {
			strokeRect(gtx, p.rect, max(1, w.dp(1)), w.chrome.bad)
		}
	}
	if t.zoom == nil {
		w.drawDividers(gtx, t)
	}
	w.placeIME(gtx, t.focus)
	w.drawOverlay(gtx, size)
	w.drawToasts(gtx, size)

	// Title follows the active pane.
	if p := t.focus; p != nil {
		title := p.title()
		if t.title != "" {
			title = t.title
		}
		if title != w.title {
			w.title = title
			w.gw.Option(app.Title(title))
		}
	}
	w.scheduleBlink(gtx)
	for _, tb := range w.tabs {
		for _, p := range tb.panes() {
			if p.exited.Load() && !p.exitHandled {
				w.handleExit(p)
			}
		}
	}
}

// placeIME tells the input method where the cursor is, so its candidate
// window opens next to what's being typed.
func (w *Window) placeIME(gtx layout.Context, p *Pane) {
	if p == nil || !w.focused {
		return
	}
	m := w.rend.Metrics()
	p.term.Lock()
	cx, cy := p.term.CursorPos()
	p.term.Unlock()
	pos := f32.Pt(float32(p.grid.X+cx*m.CellW), float32(p.grid.Y+cy*m.CellH+m.Baseline))
	if pos == w.lastCaret {
		return
	}
	w.lastCaret = pos
	gtx.Execute(key.SelectionCmd{
		Tag:   w,
		Caret: key.Caret{Pos: pos, Ascent: float32(m.Baseline), Descent: float32(m.CellH - m.Baseline)},
	})
}

// handleExit closes panes whose program ended cleanly and leaves a note
// in ones that failed, so the error stays readable.
func (w *Window) handleExit(p *Pane) {
	p.exitHandled = true
	code := p.exitCode.Load()
	if code == 0 && p.profile.Name != "error" {
		w.closePane(p)
		return
	}
	if p.profile.Name != "error" {
		p.term.WriteString(fmt.Sprintf("\r\n\x1b[0;2m[process exited with code %d; press Enter to restart or Ctrl+Shift+W to close]\x1b[0m", code))
	}
}

func (w *Window) scheduleBlink(gtx layout.Context) {
	p := w.activePane()
	if p == nil || !w.focused {
		w.blinkOn = true
		return
	}
	const period = 530 * time.Millisecond
	since := time.Since(w.blinkStart)
	if since > 10*time.Second {
		// Stop blinking after a while idle, like most terminals.
		w.blinkOn = true
		return
	}
	w.blinkOn = (since/period)%2 == 0
	if p.cursorBlinks() {
		next := w.blinkStart.Add((since/period + 1) * period)
		gtx.Execute(op.InvalidateCmd{At: next})
	}
}

// resetBlink makes the cursor solid after typing.
func (w *Window) resetBlink() {
	w.blinkStart = time.Now()
	w.blinkOn = true
}

// drawDividers paints split dividers and lets them be dragged.
func (w *Window) drawDividers(gtx layout.Context, t *Tab) {
	for _, n := range t.splits() {
		col := w.chrome.divider
		if w.dragSplit == n {
			col = w.chrome.dividerActive
		}
		fillRect(gtx, n.divider, col)
		hit := n.divider.Inset(-w.dp(3))
		area := clip.Rect(hit).Push(gtx.Ops)
		event.Op(gtx.Ops, n)
		if n.dir == splitCols {
			pointer.CursorColResize.Add(gtx.Ops)
		} else {
			pointer.CursorRowResize.Add(gtx.Ops)
		}
		area.Pop()
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: n, Kinds: pointer.Press | pointer.Drag | pointer.Release})
			if !ok {
				break
			}
			e, _ := ev.(pointer.Event)
			switch e.Kind {
			case pointer.Press:
				w.dragSplit = n
			case pointer.Drag:
				// Positions are relative to the hit area.
				if n.dir == splitCols {
					x := float64(hit.Min.X) + float64(e.Position.X) - float64(n.rect.Min.X)
					n.ratio = clampF(x/float64(n.rect.Dx()), 0.1, 0.9)
				} else {
					y := float64(hit.Min.Y) + float64(e.Position.Y) - float64(n.rect.Min.Y)
					n.ratio = clampF(y/float64(n.rect.Dy()), 0.1, 0.9)
				}
			case pointer.Release:
				w.dragSplit = nil
			}
		}
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func strokeRect(gtx layout.Context, r image.Rectangle, t int, col color.NRGBA) {
	fillRect(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), col)
	fillRect(gtx, image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), col)
	fillRect(gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), col)
	fillRect(gtx, image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), col)
}

func (w *Window) drawToasts(gtx layout.Context, size image.Point) {
	now := time.Now()
	var live []toast
	for _, t := range w.toasts {
		if now.Before(t.until) {
			live = append(live, t)
		}
	}
	w.toasts = live
	if len(live) == 0 {
		return
	}
	c := w.chrome
	m := w.rend.Metrics()
	y := size.Y - w.dp(16)
	for i := len(live) - 1; i >= 0; i-- {
		l := w.label(live[i].text, c.textRGB, c.panelRGB, false, 60)
		pad := m.CellW
		r := image.Rect(size.X-l.size.X-2*pad-w.dp(16), y-l.size.Y-pad, size.X-w.dp(16), y+pad/2)
		fillRect(gtx, r.Inset(-1), c.panelBorder)
		fillRect(gtx, r, c.panel)
		drawLabel(gtx, l, image.Pt(r.Min.X+pad, r.Min.Y+pad/2+1), 0)
		y = r.Min.Y - w.dp(8)
	}
	gtx.Execute(op.InvalidateCmd{At: live[0].until})
}

// handleInput processes key, focus and clipboard events for the window.
func (w *Window) handleInput(gtx layout.Context, size image.Point) {
	allMods := key.ModCtrl | key.ModShift | key.ModAlt | key.ModSuper | key.ModCommand
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: w},
			key.Filter{Focus: w, Optional: allMods},
			key.Filter{Focus: w, Name: key.NameTab, Optional: allMods},
			transfer.TargetFilter{Target: w, Type: "application/text"},
		)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			w.gotFocus = true
			if w.focused != e.Focus {
				w.focused = e.Focus
				if p := w.activePane(); p != nil {
					p.send(p.term.FocusReport(e.Focus))
				}
			}
			w.resetBlink()
		case key.Event:
			w.flushPendingKey()
			w.handleKey(gtx, e, size)
		case key.EditEvent:
			w.pendingKey = nil // it was AltGr typing a character
			w.handleText(e.Text)
		case transfer.DataEvent:
			rc := e.Open()
			b, _ := io.ReadAll(io.LimitReader(rc, 64<<20))
			rc.Close()
			w.deliverPaste(string(b))
		}
	}
	w.flushPendingKey()
}

// flushPendingKey sends a held Ctrl+Alt key that turned out not to be
// AltGr text.
func (w *Window) flushPendingKey() {
	if w.pendingKey == nil {
		return
	}
	e := *w.pendingKey
	w.pendingKey = nil
	w.sendKeyToPane(e)
}

func (w *Window) deliverPaste(s string) {
	if w.overlay != nil {
		w.overlay.text(w, strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " "))
		return
	}
	p := w.pasteTarget
	if p == nil {
		p = w.activePane()
	}
	w.pasteTarget = nil
	if p == nil {
		return
	}
	if p.clipboardReply {
		p.clipboardReply = false
		p.send(p.term.ClipboardReply('c', s))
		return
	}
	p.paste(s)
}

func (w *Window) handleKey(gtx layout.Context, e key.Event, size image.Point) {
	if e.State == key.Press {
		w.resetBlink()
	}
	// Overlays (command palette, search) get keys first.
	if w.overlay != nil {
		if e.State == key.Press {
			w.overlay.key(w, e)
		}
		return
	}
	if e.State == key.Press {
		c := chord(e.Modifiers, e.Name)
		if action, ok := w.keys[c]; ok && w.actionApplies(action) {
			w.runActionSized(action, size)
			return
		}
		// Ctrl+C copies when something is selected, and interrupts
		// otherwise.
		if p := w.activePane(); p != nil && c == "ctrl+c" && p.sel.active {
			w.copySelection(p)
			p.sel.clear()
			return
		}
		if p := w.activePane(); p != nil && p.exited.Load() && e.Name == key.NameReturn && p.profile.Name != "error" {
			w.restartPane(p)
			return
		}
	}
	// Ctrl+Alt+key may be AltGr producing a character; wait to see whether
	// an EditEvent follows in this batch.
	if runtime.GOOS == "windows" && e.State == key.Press && e.Modifiers.Contain(key.ModCtrl) && e.Modifiers.Contain(key.ModAlt) && len(e.Name) == 1 {
		ec := e
		w.pendingKey = &ec
		return
	}
	w.sendKeyToPane(e)
}

func (w *Window) sendKeyToPane(e key.Event) {
	p := w.activePane()
	if p == nil {
		return
	}
	flags := p.term.KittyKeyboardFlags()
	if e.State == key.Release && flags&2 == 0 {
		return
	}
	ev, ok := toVTKey(e, flags&8 != 0)
	if !ok {
		return
	}
	if e.State == key.Press {
		p.sel.clear()
	}
	p.sendKey(ev)
}

func (w *Window) handleText(s string) {
	if w.overlay != nil {
		w.overlay.text(w, s)
		return
	}
	p := w.activePane()
	if p == nil {
		return
	}
	// Control characters arrive as key events; keep only text here.
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return
	}
	p.sel.clear()
	if p.term.KittyKeyboardFlags()&8 != 0 {
		for _, r := range s {
			p.sendKey(vt.KeyEvent{Key: vt.KeyChar, Rune: r, Text: string(r)})
		}
		return
	}
	b := []byte(s)
	if p.broadcast {
		w.broadcast(p, b)
	}
	p.send(b)
}

// processTermEvents reacts to what programs asked for since last frame.
func (w *Window) processTermEvents(gtx layout.Context) {
	cfg := w.app.cfg
	for ti, t := range w.tabs {
		for _, p := range t.panes() {
			for _, ev := range p.term.TakeEvents() {
				switch e := ev.(type) {
				case vt.EvBell:
					if cfg.Bell == "sound" {
						w.beep()
					}
					if cfg.Bell != "none" {
						p.bellAt = time.Now()
						w.requestAttention()
					}
				case vt.EvCWD:
					p.cwd = e.Path
				case vt.EvClipboardWrite:
					if cfg.Clipboard.Write {
						w.writeClipboard(gtx, string(e.Data))
					}
				case vt.EvClipboardRead:
					if cfg.Clipboard.Read {
						p.clipboardReply = true
						w.requestPaste(p)
					}
				case vt.EvNotify:
					msg := e.Title
					if e.Body != "" {
						if msg != "" {
							msg += ": "
						}
						msg += e.Body
					}
					w.addToast("%s", msg)
					w.requestAttention()
					if w.shouldNotify(ti) {
						notify.Send(e.Title, e.Body)
					}
				case vt.EvProgress:
					p.progress = e
				case vt.EvCommandFinished:
					if d, ok := e.Mark.Duration(); ok && (!w.focused || ti != w.active) && d.Seconds() >= cfg.NotifyAfter {
						status := "finished"
						if e.Mark.Exit > 0 {
							status = fmt.Sprintf("failed (exit %d)", e.Mark.Exit)
						}
						w.addToast("Command %s after %s", status, d.Round(time.Second))
						w.requestAttention()
						if w.shouldNotify(ti) {
							what := e.Mark.Command
							if what == "" {
								what = p.title()
							}
							notify.Send(fmt.Sprintf("Command %s", status), fmt.Sprintf("%s (%s)", what, d.Round(time.Second)))
						}
					}
				case vt.EvAttention:
					p.bellAt = time.Now()
					w.requestAttention()
				}
			}
			if ti != w.active && p.term.Seq() != p.lastSeq {
				p.activity = true
			}
			p.lastSeq = p.term.Seq()
		}
	}
}

// shouldNotify applies the notifications setting to an event from tab ti:
// by default only when the window isn't focused or the tab is in the
// background.
func (w *Window) shouldNotify(ti int) bool {
	switch w.app.cfg.Notify {
	case "never":
		return false
	case "always":
		return true
	}
	return !w.focused || ti != w.active
}

func (w *Window) restartPane(p *Pane) {
	t := w.activeTab()
	np := w.startPane(p.profile, p.currentDir(), p.rect)
	if n := t.find(p); n != nil {
		n.pane = np
		if t.focus == p {
			t.focus = np
		}
		if t.zoom == p {
			t.zoom = np
		}
	}
	p.close()
}

// deadPTY stands in for a process that never started.
type deadPTY struct{}

func (deadPTY) Read([]byte) (int, error)        { return 0, io.EOF }
func (deadPTY) Write(b []byte) (int, error)     { return len(b), nil }
func (deadPTY) Resize(int, int, int, int) error { return nil }
func (deadPTY) Wait() (int, error)              { return -1, nil }
func (deadPTY) Kill() error                     { return nil }
func (deadPTY) Close() error                    { return nil }
func (deadPTY) Pid() int                        { return 0 }
func (deadPTY) Foreground() (string, int)       { return "", 0 }
