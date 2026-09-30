package ui

import (
	"fmt"
	"image"
	"math"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"opal/internal/theme"
	"opal/terminal/internal/settings"
)

// The settings page edits config.toml from inside the terminal. Every
// change is written to the file right away and applied live.

type settingKind uint8

const (
	kindHeading settingKind = iota
	kindBool
	kindChoice
	kindNumber
	kindText
	kindKey
	kindButton
)

type settingRow struct {
	kind  settingKind
	label string
	hint  string
	// Where the value lives in config.toml.
	table, key string

	boolOf   func(c *settings.Config) bool
	choices  func(c *settings.Config) []string
	current  func(c *settings.Config) string
	numOf    func(c *settings.Config) float64
	step     float64
	lo, hi   float64
	textOf   func(c *settings.Config) string
	writeTxt func(s string) error // text rows with custom encoding
	action   string               // kindKey: the action; kindButton: what to run
}

type settingsOverlay struct {
	rows      []settingRow
	sel       int
	scroll    int
	editing   bool
	edit      lineEdit
	capturing bool
	status    string
	tags      [80]int
	rowRects  []image.Rectangle
	scrollTag int
}

func (w *Window) openSettings() {
	so := &settingsOverlay{rows: w.settingRows()}
	so.sel = so.next(0, 1)
	w.overlay = so
	w.invalidate()
}

func (w *Window) settingRows() []settingRow {
	profiles := w.app.cfg.ProfilesWithDetected()
	var profileNames []string
	for _, p := range profiles {
		profileNames = append(profileNames, p.Name)
	}
	rows := []settingRow{
		{kind: kindHeading, label: "Appearance"},
		{kind: kindChoice, label: "Theme", key: "theme", hint: "Shared with the prompt, so both change together.",
			choices: func(*settings.Config) []string { return theme.List() },
			current: func(c *settings.Config) string { return c.Theme }},
		{kind: kindChoice, label: "Background", key: "background", hint: "Which variant of the theme to use.",
			choices: func(*settings.Config) []string { return []string{"dark", "light"} },
			current: func(c *settings.Config) string { return c.Background }},
		{kind: kindText, label: "Font family", table: "terminal", key: "font_family",
			hint:   "Comma-separated, tried in order. Empty means the platform default.",
			textOf: func(c *settings.Config) string { return strings.Join(c.FontFamily, ", ") },
			writeTxt: func(s string) error {
				var names []string
				for _, n := range strings.Split(s, ",") {
					if n = strings.TrimSpace(n); n != "" {
						names = append(names, settings.Quote(n))
					}
				}
				if len(names) == 0 {
					return settings.Unset("terminal", "font_family")
				}
				return settings.Set("terminal", "font_family", "["+strings.Join(names, ", ")+"]")
			}},
		{kind: kindNumber, label: "Font size (points)", table: "terminal", key: "font_size", step: 1, lo: 5, hi: 72,
			numOf: func(c *settings.Config) float64 { return c.FontSize }},
		{kind: kindNumber, label: "Line height", table: "terminal", key: "line_height", step: 0.05, lo: 0.8, hi: 2,
			numOf: func(c *settings.Config) float64 { return c.LineHeight }},
		{kind: kindNumber, label: "Cell width", table: "terminal", key: "cell_width", step: 0.05, lo: 0.8, hi: 1.5,
			numOf: func(c *settings.Config) float64 { return c.CellWidth }},
		{kind: kindChoice, label: "Cursor", table: "terminal", key: "cursor_style",
			choices: func(*settings.Config) []string { return []string{"block", "bar", "underline"} },
			current: func(c *settings.Config) string { return c.CursorStyle }},
		{kind: kindBool, label: "Blinking cursor", table: "terminal", key: "cursor_blink",
			boolOf: func(c *settings.Config) bool { return c.CursorBlink }},
		{kind: kindBool, label: "Bold text in bright colors", table: "terminal", key: "bold_is_bright",
			boolOf: func(c *settings.Config) bool { return c.BoldIsBright }},
		{kind: kindNumber, label: "Minimum contrast", table: "terminal", key: "min_contrast", step: 0.5, lo: 1, hi: 10,
			hint:  "Lifts text that's hard to read on its background. 1 is off; 4.5 is the WCAG AA level.",
			numOf: func(c *settings.Config) float64 { return c.MinContrast }},
		{kind: kindNumber, label: "Padding (pixels)", table: "terminal", key: "padding", step: 1, lo: 0, hi: 40,
			numOf: func(c *settings.Config) float64 { return float64(c.Padding) }},

		{kind: kindHeading, label: "Shell"},
		{kind: kindChoice, label: "Default shell", table: "terminal", key: "shell", hint: "What new tabs run.",
			choices: func(*settings.Config) []string { return profileNames },
			current: func(c *settings.Config) string { return c.DefaultProfile().Name }},
		{kind: kindBool, label: "Restore the last session", table: "terminal", key: "restore_session",
			hint:   "Reopen the tabs, splits and output of the last window you closed.",
			boolOf: func(c *settings.Config) bool { return c.Restore }},
		{kind: kindNumber, label: "Scrollback (lines)", table: "terminal", key: "scrollback", step: 1000, lo: 0, hi: 200000,
			hint:  "Applies to new tabs.",
			numOf: func(c *settings.Config) float64 { return float64(c.Scrollback) }},
		{kind: kindBool, label: "Grapheme clusters", table: "terminal", key: "grapheme_clustering",
			hint:   "Size emoji sequences and combining marks as whole clusters. Applies to new tabs.",
			boolOf: func(c *settings.Config) bool { return c.Graphemes }},

		{kind: kindHeading, label: "Behavior"},
		{kind: kindBool, label: "Copy on select", table: "terminal", key: "copy_on_select",
			boolOf: func(c *settings.Config) bool { return c.CopyOnSelect }},
		{kind: kindChoice, label: "Bell", table: "terminal", key: "bell",
			choices: func(*settings.Config) []string { return []string{"visual", "sound", "none"} },
			current: func(c *settings.Config) string { return c.Bell }},
		{kind: kindChoice, label: "Desktop notifications", table: "terminal", key: "notifications",
			hint:    "For notifications from programs and long commands that finish in the background.",
			choices: func(*settings.Config) []string { return []string{"unfocused", "always", "never"} },
			current: func(c *settings.Config) string { return c.Notify }},
		{kind: kindNumber, label: "Notify after (seconds)", table: "terminal", key: "notify_after", step: 5, lo: 0, hi: 3600,
			numOf: func(c *settings.Config) float64 { return c.NotifyAfter }},
		{kind: kindBool, label: "Programs may set the clipboard", table: "terminal.clipboard", key: "write",
			hint:   "OSC 52, used by editors and tmux over SSH.",
			boolOf: func(c *settings.Config) bool { return c.Clipboard.Write }},
		{kind: kindBool, label: "Programs may read the clipboard", table: "terminal.clipboard", key: "read",
			hint:   "Off by default: a program on a remote host could read what you copied.",
			boolOf: func(c *settings.Config) bool { return c.Clipboard.Read }},
		{kind: kindText, label: "Word characters", table: "terminal", key: "word_chars",
			hint:   "Characters that count as part of a word when double-clicking.",
			textOf: func(c *settings.Config) string { return c.WordChars }},

		{kind: kindHeading, label: "Keys"},
	}
	for _, a := range w.actionList() {
		if strings.HasPrefix(a.id, "profile:") {
			continue
		}
		rows = append(rows, settingRow{kind: kindKey, label: a.title, action: a.id,
			hint: "Enter records a new shortcut; Delete removes this action's shortcuts."})
	}
	rows = append(rows,
		settingRow{kind: kindHeading, label: "File"},
		settingRow{kind: kindButton, label: "Open config.toml in an editor", action: "open_config"},
	)
	return rows
}

// next finds the next selectable row from i in direction dir.
func (so *settingsOverlay) next(i, dir int) int {
	for i >= 0 && i < len(so.rows) {
		if so.rows[i].kind != kindHeading {
			return i
		}
		i += dir
	}
	return so.sel
}

// write saves one value and reloads the configuration.
func (so *settingsOverlay) write(w *Window, r settingRow, value string) {
	table := r.table
	cfg := w.app.cfg
	// Theme and background live at the top level (shared with the prompt)
	// unless [terminal] overrides them.
	if r.key == "theme" && cfg.ThemeLocal || r.key == "background" && cfg.BackgroundLocal {
		table = "terminal"
	}
	if err := settings.Set(table, r.key, value); err != nil {
		so.status = "Couldn't save: " + err.Error()
		return
	}
	so.status = ""
	w.app.reload()
}

// change moves a row's value by dir (toggle, cycle or step).
func (so *settingsOverlay) change(w *Window, dir int) {
	r := so.rows[so.sel]
	cfg := w.app.cfg
	switch r.kind {
	case kindBool:
		so.write(w, r, settings.Bool(!r.boolOf(cfg)))
	case kindChoice:
		opts := r.choices(cfg)
		if len(opts) == 0 {
			return
		}
		i := 0
		cur := r.current(cfg)
		for j, o := range opts {
			if strings.EqualFold(o, cur) {
				i = j
			}
		}
		i = (i + dir + len(opts)) % len(opts)
		so.write(w, r, settings.Quote(opts[i]))
	case kindNumber:
		v := r.numOf(cfg) + float64(dir)*r.step
		v = math.Max(r.lo, math.Min(r.hi, v))
		v = math.Round(v/r.step) * r.step
		so.write(w, r, settings.Number(math.Round(v*1000)/1000))
	}
}

// activate is Enter or a click on the value.
func (so *settingsOverlay) activate(w *Window) {
	r := so.rows[so.sel]
	switch r.kind {
	case kindBool, kindChoice:
		so.change(w, 1)
	case kindNumber:
		so.change(w, 1)
	case kindText:
		so.editing = true
		so.edit.set(r.textOf(w.app.cfg))
	case kindKey:
		so.capturing = true
		so.status = "Press the new shortcut. Esc cancels."
	case kindButton:
		w.overlay = nil
		w.runAction(r.action)
	}
}

func (so *settingsOverlay) key(w *Window, e key.Event) {
	if so.capturing {
		switch e.Name {
		case key.NameEscape:
			so.capturing = false
			so.status = ""
		case key.NameCtrl, key.NameShift, key.NameAlt, key.NameSuper, key.NameCommand:
			// Wait for a real key.
		default:
			c := chord(e.Modifiers, e.Name)
			r := so.rows[so.sel]
			so.capturing = false
			if err := settings.Set("terminal.keys", c, settings.Quote(r.action)); err != nil {
				so.status = "Couldn't save: " + err.Error()
				return
			}
			w.app.reload()
			so.status = fmt.Sprintf("%s is now bound to %s.", c, r.label)
		}
		return
	}
	if so.editing {
		switch e.Name {
		case key.NameEscape:
			so.editing = false
		case key.NameReturn, key.NameEnter:
			so.editing = false
			r := so.rows[so.sel]
			v := strings.TrimSpace(so.edit.String())
			if r.writeTxt != nil {
				if err := r.writeTxt(v); err != nil {
					so.status = "Couldn't save: " + err.Error()
				}
				w.app.reload()
			} else if v == "" {
				settings.Unset(r.table, r.key)
				w.app.reload()
			} else {
				so.write(w, r, settings.Quote(v))
			}
		default:
			so.edit.key(e)
		}
		return
	}
	switch e.Name {
	case key.NameEscape:
		w.overlay = nil
	case key.NameUpArrow:
		so.sel = so.next(so.sel-1, -1)
	case key.NameDownArrow:
		so.sel = so.next(so.sel+1, 1)
	case key.NamePageUp:
		so.sel = so.next(max(0, so.sel-10), 1)
	case key.NamePageDown:
		so.sel = so.next(min(len(so.rows)-1, so.sel+10), -1)
	case key.NameHome:
		so.sel = so.next(0, 1)
	case key.NameEnd:
		so.sel = so.next(len(so.rows)-1, -1)
	case key.NameLeftArrow:
		so.change(w, -1)
	case key.NameRightArrow:
		so.change(w, 1)
	case key.NameReturn, key.NameEnter, key.NameSpace:
		so.activate(w)
	case key.NameDeleteForward, key.NameDeleteBackward:
		r := so.rows[so.sel]
		if r.kind == kindKey {
			for _, c := range keysFor(w.keys, r.action) {
				settings.Set("terminal.keys", c, settings.Quote("none"))
			}
			w.app.reload()
			so.status = r.label + " has no shortcut now."
		} else if r.table != "" && r.kind != kindHeading && r.kind != kindButton {
			// Back to the default value.
			settings.Unset(r.table, r.key)
			w.app.reload()
			so.status = r.label + " is back to its default."
		}
	}
}

func (so *settingsOverlay) text(w *Window, s string) {
	if so.editing {
		so.edit.insert(s)
	}
}

// value renders a row's current value.
func (so *settingsOverlay) value(w *Window, r settingRow) (string, bool) {
	cfg := w.app.cfg
	switch r.kind {
	case kindBool:
		if r.boolOf(cfg) {
			return "on", true
		}
		return "off", false
	case kindChoice:
		return "‹ " + r.current(cfg) + " ›", true
	case kindNumber:
		return "‹ " + settings.Number(math.Round(r.numOf(cfg)*1000)/1000) + " ›", true
	case kindText:
		if v := r.textOf(cfg); v != "" {
			return v, true
		}
		if r.key == "font_family" && w.fonts != nil {
			return "default (" + w.fonts.Family() + ")", false
		}
		return "not set", false
	case kindKey:
		ks := keysFor(w.keys, r.action)
		if len(ks) == 0 {
			return "none", false
		}
		return strings.Join(ks, "  "), true
	}
	return "", false
}

func (so *settingsOverlay) draw(gtx layout.Context, w *Window, size image.Point) {
	c := w.chrome
	m := w.rend.Metrics()
	cells := min(100, (size.X-w.dp(40))/m.CellW)
	width := cells * m.CellW
	x0 := (size.X - width) / 2
	y0 := w.tabBarHeight() + w.dp(10)
	y1 := size.Y - w.dp(10)
	r := image.Rect(x0, y0, x0+width, y1)
	fillRect(gtx, image.Rectangle{Max: size}, c.shade)
	w.panel(gtx, r)
	pad := m.CellW * 2
	rowH := m.CellH + w.dp(8)

	title := w.label("Settings", c.textRGB, c.panelRGB, true, 20)
	drawLabel(gtx, title, image.Pt(x0+pad, y0+w.dp(10)), 0)
	help := w.label("arrows change  Enter edits  Delete resets  Esc closes", c.dimRGB, c.panelRGB, false, cells-24)
	drawLabel(gtx, help, image.Pt(r.Max.X-pad-help.size.X, y0+w.dp(10)), 0)
	top := y0 + w.dp(10) + m.CellH + w.dp(10)
	fillRect(gtx, image.Rect(x0, top-w.dp(4), r.Max.X, top-w.dp(3)), c.panelBorder)

	statusH := m.CellH + w.dp(14)
	listBottom := y1 - statusH
	visible := max(1, (listBottom-top)/rowH)
	if so.sel < so.scroll+1 {
		so.scroll = max(0, so.sel-1)
	}
	if so.sel >= so.scroll+visible {
		so.scroll = so.sel - visible + 1
	}
	so.scroll = clampInt(so.scroll, 0, max(0, len(so.rows)-visible))

	// Wheel scrolling over the list.
	listRect := image.Rect(x0, top, r.Max.X, listBottom)
	area := clip.Rect(listRect).Push(gtx.Ops)
	event.Op(gtx.Ops, &so.scrollTag)
	area.Pop()
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &so.scrollTag, Kinds: pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok && pe.Scroll.Y != 0 {
			d := 3
			if pe.Scroll.Y < 0 {
				d = -3
			}
			so.scroll = clampInt(so.scroll+d, 0, max(0, len(so.rows)-visible))
			so.sel = clampInt(so.sel, so.scroll, so.scroll+visible-1)
			so.sel = so.next(so.sel, 1)
		}
	}

	valueX := x0 + width*52/100
	labelCells := (valueX-x0-pad)/m.CellW - 1
	valueCells := (r.Max.X-pad-valueX)/m.CellW - 1
	for i := 0; i < visible && so.scroll+i < len(so.rows); i++ {
		idx := so.scroll + i
		row := so.rows[idx]
		ry := top + i*rowH
		rr := image.Rect(x0+w.dp(4), ry, r.Max.X-w.dp(4), ry+rowH)
		ty := ry + (rowH-m.CellH)/2
		if row.kind == kindHeading {
			l := w.label(strings.ToUpper(row.label), c.accentRGB, c.panelRGB, true, labelCells)
			drawLabel(gtx, l, image.Pt(x0+pad-m.CellW, ty+w.dp(2)), 0)
			continue
		}
		bg := c.panelRGB
		selected := idx == so.sel
		if selected {
			fillRect(gtx, rr, c.panelSel)
			bg = c.selRGB
		}
		l := w.label(row.label, c.textRGB, bg, false, labelCells)
		drawLabel(gtx, l, image.Pt(x0+pad, ty), 0)
		switch {
		case selected && so.editing:
			fillRect(gtx, image.Rect(valueX-w.dp(4), ry+w.dp(2), r.Max.X-pad+w.dp(4), ry+rowH-w.dp(2)), c.panel)
			w.drawField(gtx, &so.edit, image.Pt(valueX, ty), valueCells, "")
		case selected && so.capturing:
			vl := w.label("press a shortcut…", c.accentRGB, bg, false, valueCells)
			drawLabel(gtx, vl, image.Pt(valueX, ty), 0)
		default:
			v, strong := so.value(w, row)
			fg := c.dimRGB
			if strong {
				fg = c.textRGB
				if row.kind == kindBool {
					fg = c.accentRGB
				}
			}
			vl := w.label(v, fg, bg, false, valueCells)
			drawLabel(gtx, vl, image.Pt(valueX, ty), 0)
		}

		if i < len(so.tags) {
			area := clip.Rect(rr).Push(gtx.Ops)
			event.Op(gtx.Ops, &so.tags[i])
			pointer.CursorPointer.Add(gtx.Ops)
			area.Pop()
			for {
				ev, ok := gtx.Event(pointer.Filter{Target: &so.tags[i], Kinds: pointer.Press})
				if !ok {
					break
				}
				pe, ok := ev.(pointer.Event)
				if !ok || pe.Kind != pointer.Press {
					continue
				}
				so.editing, so.capturing = false, false
				so.sel = idx
				// A click on the value side changes it.
				if int(pe.Position.X)+rr.Min.X >= valueX-w.dp(4) {
					so.activate(w)
				}
			}
		}
	}

	// Status line: the selected row's hint, or the last message.
	msg := so.status
	if msg == "" && so.sel < len(so.rows) {
		msg = so.rows[so.sel].hint
	}
	if msg != "" {
		fillRect(gtx, image.Rect(x0, listBottom, r.Max.X, listBottom+1), c.panelBorder)
		sl := w.label(msg, c.dimRGB, c.panelRGB, false, cells-4)
		drawLabel(gtx, sl, image.Pt(x0+pad, listBottom+(statusH-m.CellH)/2), 0)
	}
}
