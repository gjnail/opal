package ui

import (
	"image"
	"runtime"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"opal/terminal/internal/vt"
)

type hoverState struct {
	link        bool
	url         string
	from, to    vt.Pos
	inScrollbar bool
	pos         f32.Point
}

// clickCounter turns presses into single, double and triple clicks.
type clickCounter struct {
	last  time.Time
	cell  image.Point
	count int
}

func (c *clickCounter) press(cell image.Point, now time.Time) int {
	if now.Sub(c.last) < 450*time.Millisecond && cell == c.cell {
		c.count = c.count%3 + 1
	} else {
		c.count = 1
	}
	c.last, c.cell = now, cell
	return c.count
}

// registerPointer declares the pane's input area. Events arrive in pane
// coordinates.
func (p *Pane) registerPointer(gtx layout.Context) {
	r := p.rect
	off := op.Offset(r.Min).Push(gtx.Ops)
	area := clip.Rect{Max: r.Size()}.Push(gtx.Ops)
	event.Op(gtx.Ops, p)
	cursor := pointer.CursorText
	if p.hover.link {
		cursor = pointer.CursorPointer
	} else if p.hover.inScrollbar {
		cursor = pointer.CursorDefault
	}
	cursor.Add(gtx.Ops)
	area.Pop()
	off.Pop()
}

// cellAt maps a pane-local point to a cell, clamped to the grid.
func (p *Pane) cellAt(pt f32.Point) (image.Point, bool) {
	m := p.win.rend.Metrics()
	x := int(pt.X) - (p.grid.X - p.rect.Min.X)
	y := int(pt.Y) - (p.grid.Y - p.rect.Min.Y)
	inside := x >= 0 && y >= 0 && x < p.cols*m.CellW && y < p.rows*m.CellH
	cx := clampInt(floorDiv(x, m.CellW), 0, p.cols-1)
	cy := clampInt(floorDiv(y, m.CellH), 0, p.rows-1)
	return image.Pt(cx, cy), inside
}

func floorDiv(a, b int) int {
	if a < 0 {
		return -((-a + b - 1) / b)
	}
	return a / b
}

// absAt converts a view cell to an absolute position. Caller holds the lock.
func (p *Pane) absAt(c image.Point) vt.Pos {
	top := p.viewTopRow()
	return vt.Pos{Row: p.term.FirstAbs() + int64(top+c.Y), Col: c.X}
}

func toVTMods(m key.Modifiers) vt.Mods {
	var out vt.Mods
	if m.Contain(key.ModShift) {
		out |= vt.ModShift
	}
	if m.Contain(key.ModAlt) {
		out |= vt.ModAlt
	}
	if m.Contain(key.ModCtrl) {
		out |= vt.ModCtrl
	}
	if m.Contain(key.ModSuper) || m.Contain(key.ModCommand) {
		out |= vt.ModSuper
	}
	return out
}

func buttonOf(b pointer.Buttons) vt.MouseButton {
	switch {
	case b.Contain(pointer.ButtonPrimary):
		return vt.MouseLeft
	case b.Contain(pointer.ButtonTertiary):
		return vt.MouseMiddle
	case b.Contain(pointer.ButtonSecondary):
		return vt.MouseRight
	}
	return vt.MouseNoButton
}

// wheelUnit is how much scroll delta makes one line.
func (p *Pane) wheelUnit() float32 {
	if runtime.GOOS == "darwin" {
		return float32(p.win.rend.Metrics().CellH)
	}
	return 40 // Windows and X11 report 120 per notch; 3 lines a notch
}

// handlePointer processes this frame's mouse events.
func (p *Pane) handlePointer(gtx layout.Context) {
	w := p.win
	cfg := w.app.cfg
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  p,
			Kinds:   pointer.Press | pointer.Release | pointer.Drag | pointer.Move | pointer.Scroll | pointer.Leave,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
			ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		debugf("pointer %v buttons=%v pos=%v scroll=%v", e.Kind, e.Buttons, e.Position, e.Scroll)
		mods := toVTMods(e.Modifiers)
		cell, inside := p.cellAt(e.Position)
		m := w.rend.Metrics()
		px := image.Pt(int(e.Position.X)-(p.grid.X-p.rect.Min.X), int(e.Position.Y)-(p.grid.Y-p.rect.Min.Y))
		tracking := p.term.MouseTracking() != vt.MouseTrackNone && mods&vt.ModShift == 0
		p.hover.pos = e.Position
		p.hover.inScrollbar = e.Position.X > float32(p.rect.Dx()-w.dp(12))

		switch e.Kind {
		case pointer.Press:
			w.focusPane(p)
			btn := buttonOf(e.Buttons)
			p.mouseDown = true
			p.pressedBtn = btn
			if tracking && inside {
				p.send(p.term.EncodeMouse(vt.MouseEvent{Button: btn, Action: vt.MousePress, Mods: mods, X: cell.X, Y: cell.Y, PX: px.X, PY: px.Y}))
				continue
			}
			switch btn {
			case vt.MouseLeft:
				if mods&vt.ModCtrl != 0 || (runtime.GOOS == "darwin" && mods&vt.ModSuper != 0) {
					if u := p.linkAt(cell); u != "" {
						openURL(u)
						continue
					}
				}
				n := p.clicks.press(cell, time.Now())
				p.term.Lock()
				pos := p.absAt(cell)
				if mods&vt.ModShift != 0 && p.sel.active {
					p.sel.extend(p.term, pos, cfg.WordChars)
				} else {
					mode := [...]selMode{selChar, selChar, selWord, selLine}[n]
					if mods&vt.ModAlt != 0 {
						mode = selBlock
					}
					p.sel.start(p.term, pos, mode, cfg.WordChars)
					if mode == selChar {
						// A plain click shouldn't leave a one-cell selection.
						p.sel.active = false
						p.sel.anchor = pos
					}
				}
				p.term.Unlock()
			case vt.MouseRight:
				if p.sel.active {
					w.copySelection(p)
					p.sel.clear()
				} else {
					w.requestPaste(p)
				}
			case vt.MouseMiddle:
				w.requestPaste(p)
			}

		case pointer.Drag:
			if tracking {
				if p.term.MouseTracking() >= vt.MouseTrackButton {
					p.send(p.term.EncodeMouse(vt.MouseEvent{Button: p.pressedBtn, Action: vt.MouseMotion, Mods: mods, X: cell.X, Y: cell.Y, PX: px.X, PY: px.Y}))
				}
				continue
			}
			if p.pressedBtn != vt.MouseLeft {
				continue
			}
			// Dragging past the top or bottom scrolls.
			if px.Y < 0 {
				p.scrollBy(max(1, -px.Y/m.CellH))
				gtx.Execute(op.InvalidateCmd{At: time.Now().Add(50 * time.Millisecond)})
			} else if px.Y > p.rows*m.CellH {
				p.scrollBy(-max(1, (px.Y-p.rows*m.CellH)/m.CellH))
				gtx.Execute(op.InvalidateCmd{At: time.Now().Add(50 * time.Millisecond)})
			}
			p.term.Lock()
			pos := p.absAt(cell)
			if !p.sel.active {
				if pos != p.sel.anchor {
					anchor := p.sel.anchor
					mode := selChar
					if mods&vt.ModAlt != 0 {
						mode = selBlock
					}
					p.sel.start(p.term, anchor, mode, cfg.WordChars)
					p.sel.extend(p.term, pos, cfg.WordChars)
				}
			} else {
				p.sel.extend(p.term, pos, cfg.WordChars)
			}
			p.term.Unlock()

		case pointer.Release:
			btn := p.pressedBtn
			p.mouseDown = false
			if tracking {
				p.send(p.term.EncodeMouse(vt.MouseEvent{Button: btn, Action: vt.MouseRelease, Mods: mods, X: cell.X, Y: cell.Y, PX: px.X, PY: px.Y}))
				continue
			}
			// A fast drag can arrive as press and release with no moves in
			// between; the release position still ends the selection.
			if btn == vt.MouseLeft {
				p.term.Lock()
				pos := p.absAt(cell)
				if p.sel.active {
					p.sel.extend(p.term, pos, cfg.WordChars)
				} else if pos != p.sel.anchor && p.clicks.count == 1 {
					mode := selChar
					if mods&vt.ModAlt != 0 {
						mode = selBlock
					}
					p.sel.start(p.term, p.sel.anchor, mode, cfg.WordChars)
					p.sel.extend(p.term, pos, cfg.WordChars)
				}
				p.term.Unlock()
			}
			if btn == vt.MouseLeft && p.sel.active && cfg.CopyOnSelect {
				w.copySelection(p)
			}
			if btn == vt.MouseLeft && !p.sel.active && p.clicks.count == 1 && mods == 0 && inside {
				p.clickToMove(cell)
			}

		case pointer.Move:
			if tracking && p.term.MouseTracking() == vt.MouseTrackAny && inside {
				p.send(p.term.EncodeMouse(vt.MouseEvent{Button: vt.MouseNoButton, Action: vt.MouseMotion, Mods: mods, X: cell.X, Y: cell.Y, PX: px.X, PY: px.Y}))
			}
			p.updateHover(cell, inside, mods)

		case pointer.Leave:
			p.hover = hoverState{}

		case pointer.Scroll:
			p.handleWheel(gtx, e, mods, cell, px, tracking)
		}
	}
}

// clickToMove moves the shell's cursor to a click inside the command being
// typed, by sending arrow keys. It needs shell integration: the input is
// the part of the line marked by OSC 133;B, and nothing happens elsewhere.
func (p *Pane) clickToMove(cell image.Point) {
	if p.scroll != 0 {
		return
	}
	p.term.Lock()
	cx, cy := p.term.CursorPos()
	alt := p.term.AltScreen()
	var moves int
	ok := false
	if !alt && cell.Y == cy {
		l := p.term.ScreenLine(cy)
		first, last := -1, -1
		for x, c := range l.Cells {
			if c.A.Semantic() == vt.SemanticInput && (c.R != 0 || x <= cx) {
				if first < 0 {
					first = x
				}
				last = x
			}
		}
		if first >= 0 {
			target := clampInt(cell.X, first, max(last+1, cx))
			// Count characters, not cells: a wide character is one arrow
			// press.
			from, to := min(target, cx), max(target, cx)
			for x := from; x < to && x < len(l.Cells); x++ {
				if !l.Cells[x].Spacer() {
					moves++
				}
			}
			if target < cx {
				moves = -moves
			}
			ok = moves != 0
		}
	}
	p.term.Unlock()
	if !ok {
		return
	}
	k := vt.KeyRight
	if moves < 0 {
		k, moves = vt.KeyLeft, -moves
	}
	var b []byte
	for i := 0; i < moves; i++ {
		b = append(b, p.term.EncodeKey(vt.KeyEvent{Key: k})...)
	}
	p.send(b)
}

// updateHover finds a link under the mouse while the open-link modifier
// is held.
func (p *Pane) updateHover(cell image.Point, inside bool, mods vt.Mods) {
	openMod := vt.ModCtrl
	if runtime.GOOS == "darwin" {
		openMod = vt.ModSuper
	}
	if !inside || mods&openMod == 0 {
		p.hover.link, p.hover.url = false, ""
		return
	}
	p.term.Lock()
	url, a, b, ok := p.term.URLAt(p.absAt(cell))
	p.term.Unlock()
	p.hover.link, p.hover.url, p.hover.from, p.hover.to = ok, url, a, b
}

func (p *Pane) linkAt(cell image.Point) string {
	p.term.Lock()
	defer p.term.Unlock()
	url, _, _, ok := p.term.URLAt(p.absAt(cell))
	if !ok {
		return ""
	}
	return url
}

func (p *Pane) handleWheel(gtx layout.Context, e pointer.Event, mods vt.Mods, cell, px image.Point, tracking bool) {
	w := p.win
	if mods&vt.ModCtrl != 0 && !tracking {
		// Ctrl+wheel zooms, like browsers.
		if e.Scroll.Y < 0 {
			w.runAction("font_bigger")
		} else if e.Scroll.Y > 0 {
			w.runAction("font_smaller")
		}
		return
	}
	p.scrollAccum += e.Scroll.Y
	unit := p.wheelUnit()
	lines := int(p.scrollAccum / unit)
	if lines == 0 {
		return
	}
	p.scrollAccum -= float32(lines) * unit
	switch {
	case tracking:
		btn := vt.MouseWheelDown
		if lines < 0 {
			btn = vt.MouseWheelUp
			lines = -lines
		}
		for i := 0; i < lines; i++ {
			p.send(p.term.EncodeMouse(vt.MouseEvent{Button: btn, Action: vt.MousePress, Mods: mods, X: cell.X, Y: cell.Y, PX: px.X, PY: px.Y}))
		}
	case p.term.AlternateScroll():
		// Full-screen programs without mouse support (less, man) get
		// arrow keys instead.
		k := vt.KeyDown
		if lines < 0 {
			k = vt.KeyUp
			lines = -lines
		}
		for i := 0; i < lines; i++ {
			p.send(p.term.EncodeKey(vt.KeyEvent{Key: k}))
		}
	default:
		p.scrollBy(-lines)
	}
}
