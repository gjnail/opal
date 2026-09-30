// Package vt is Opal Terminal's emulation engine: a byte-stream parser and
// the screen model it drives. It knows nothing about windows, fonts or
// processes, so every behavior here can be tested headlessly.
package vt

import (
	"sync"
	"time"
)

// Options configure a new Terminal.
type Options struct {
	Cols, Rows int
	// Scrollback is how many lines of history the primary screen keeps.
	Scrollback int
	Palette    Palette
	// GraphemeClustering starts with mode 2027 on: grapheme clusters (emoji
	// ZWJ sequences, flags, combining marks) occupy one cell group, with
	// their width taken from the whole cluster.
	GraphemeClustering bool
	// Name and Version answer XTVERSION and friends.
	Name, Version string
}

// Terminal is a virtual terminal. All methods are safe for concurrent use;
// renderers take Lock/Unlock around reads of the screen.
type Terminal struct {
	mu sync.Mutex

	cols, rows int
	primary    *buffer
	alt        *buffer
	buf        *buffer // the active one

	cur   cursor
	modes modes
	tabs  []bool
	// Scroll region, inclusive rows, and left/right margins (DECSLRM).
	top, bottom int
	left, right int

	parser *parser

	basePal Palette
	pal     Palette

	title      string
	iconTitle  string
	titleStack []string
	cwd        string

	cursorStyle CursorStyle

	// Keyboard protocol state.
	modifyOtherKeys int

	semantic   int
	lastPrompt *PromptMark

	link *Hyperlink

	// The last printed cell, for grapheme joining and REP.
	lastX, lastY int
	lastValid    bool
	lastRune     rune

	replies []byte
	events  []Event

	scrollbackMax int
	seq           uint64
	generation    uint64 // bumped when absolute line numbers are invalidated (reflow, clear)

	cellW, cellH int // pixels, for size reports and images
	dark         bool

	syncStart time.Time

	dcs    dcsHandler
	images *imageStore

	savedModes map[int]bool
	sgrStack   []pen
	colorStack []Palette
	kittyNotes map[string]*EvNotify

	name, version string

	freeLines []*Line
}

// CursorStyle is the DECSCUSR cursor shape.
type CursorStyle int

const (
	CursorDefault CursorStyle = iota
	CursorBlinkBlock
	CursorSteadyBlock
	CursorBlinkUnderline
	CursorSteadyUnderline
	CursorBlinkBar
	CursorSteadyBar
)

// New creates a terminal.
func New(o Options) *Terminal {
	if o.Cols < 1 {
		o.Cols = 80
	}
	if o.Rows < 1 {
		o.Rows = 24
	}
	if o.Scrollback < 0 {
		o.Scrollback = 0
	}
	if o.Palette == (Palette{}) {
		o.Palette = DefaultPalette()
	}
	if o.Name == "" {
		o.Name = "OpalTerminal"
	}
	t := &Terminal{
		cols:          o.Cols,
		rows:          o.Rows,
		basePal:       o.Palette,
		pal:           o.Palette,
		scrollbackMax: o.Scrollback,
		name:          o.Name,
		version:       o.Version,
		cellW:         8,
		cellH:         16,
		dark:          o.Palette.Background.Luma() < 0.5,
	}
	t.parser = newParser(t)
	t.images = newImageStore()
	t.primary = newBuffer(t.cols, t.rows, true)
	t.alt = newBuffer(t.cols, t.rows, false)
	t.buf = t.primary
	t.resetState()
	if o.GraphemeClustering {
		t.modes.set(modeGraphemeCluster, true)
	}
	return t
}

// resetState is RIS minus the buffers themselves.
func (t *Terminal) resetState() {
	t.cur = cursor{}
	t.cur.resetCharsets()
	t.modes = defaultModes()
	t.top, t.bottom = 0, t.rows-1
	t.left, t.right = 0, t.cols-1
	t.resetTabs()
	t.cursorStyle = CursorDefault
	t.modifyOtherKeys = 0
	t.primary.kitty = nil
	t.alt.kitty = nil
	t.primary.saved = savedCursor{}
	t.alt.saved = savedCursor{}
	t.link = nil
	t.lastValid = false
	t.semantic = SemanticOutput
	t.pal = t.basePal
	t.titleStack = nil
}

func (t *Terminal) resetTabs() {
	t.tabs = make([]bool, t.cols)
	for i := 8; i < t.cols; i += 8 {
		t.tabs[i] = true
	}
}

// Lock and Unlock guard reads of screen state by renderers.
func (t *Terminal) Lock()   { t.mu.Lock() }
func (t *Terminal) Unlock() { t.mu.Unlock() }

// Write feeds output from the child process into the terminal.
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.parser.advance(p)
	t.seq++
	t.mu.Unlock()
	return len(p), nil
}

// WriteString is Write for strings; handy in tests.
func (t *Terminal) WriteString(s string) { t.Write([]byte(s)) }

// TakeReplies returns bytes the terminal wants sent back to the child
// (device reports, color queries, ...), clearing the queue.
func (t *Terminal) TakeReplies() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.replies
	t.replies = nil
	return r
}

// TakeEvents returns and clears pending events for the UI.
func (t *Terminal) TakeEvents() []Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.events
	t.events = nil
	return e
}

func (t *Terminal) reply(s string) { t.replies = append(t.replies, s...) }

func (t *Terminal) emit(e Event) { t.events = append(t.events, e) }

// Seq changes whenever the screen might have changed.
func (t *Terminal) Seq() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seq
}

// SetCellSize tells the terminal the pixel size of a cell, for pixel
// reports (XTWINOPS 14/16, in-band resize) and image placement.
func (t *Terminal) SetCellSize(w, h int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if w > 0 && h > 0 {
		t.cellW, t.cellH = w, h
	}
}

// SetDarkMode records whether the UI is dark, for color scheme queries
// (CSI ? 996 n), and notifies subscribed programs (mode 2031).
func (t *Terminal) SetDarkMode(dark bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dark == dark {
		return
	}
	t.dark = dark
	if t.modes.get(modeColorSchemeUpdates) {
		t.reportColorScheme()
	}
}

func (t *Terminal) reportColorScheme() {
	if t.dark {
		t.reply("\x1b[?997;1n")
	} else {
		t.reply("\x1b[?997;2n")
	}
}

// SetPalette replaces the base palette (theme change). Colors set by the
// running program with OSC 4/10/11 are dropped.
func (t *Terminal) SetPalette(p Palette) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.basePal = p
	t.pal = p
	t.seq++
}

// Palette returns the current colors, including program overrides.
func (t *Terminal) Palette() Palette {
	return t.pal
}

// Size returns the grid size.
func (t *Terminal) Size() (cols, rows int) { return t.cols, t.rows }

// Title returns the window title set by the program.
func (t *Terminal) Title() string { return t.title }

// CWD returns the working directory reported by shell integration.
func (t *Terminal) CWD() string { return t.cwd }

// AltScreen reports whether the alternate screen is active.
func (t *Terminal) AltScreen() bool { return t.buf == t.alt }

// CursorPos returns the cursor position on screen.
func (t *Terminal) CursorPos() (x, y int) { return t.cur.x, t.cur.y }

// CursorVisible reports DECTCEM.
func (t *Terminal) CursorVisible() bool { return t.modes.get(modeCursorVisible) }

// CursorShape returns the DECSCUSR style.
func (t *Terminal) CursorShape() CursorStyle { return t.cursorStyle }

// CursorBlink reports whether the cursor should blink (mode 12 or an odd
// DECSCUSR style).
func (t *Terminal) CursorBlink() bool {
	switch t.cursorStyle {
	case CursorBlinkBlock, CursorBlinkUnderline, CursorBlinkBar:
		return true
	case CursorSteadyBlock, CursorSteadyUnderline, CursorSteadyBar:
		return false
	}
	return t.modes.get(modeCursorBlink)
}

// ReverseVideo reports DECSCNM.
func (t *Terminal) ReverseVideo() bool { return t.modes.get(modeReverseVideo) }

// Synchronized reports whether the program asked us to hold rendering
// (mode 2026). Renderers should give up waiting after a short timeout.
func (t *Terminal) Synchronized() (bool, time.Time) {
	return t.modes.get(modeSyncOutput), t.syncStart
}

// Generation changes when absolute line numbers stop meaning what they
// meant (reflow, scrollback cleared). Selections anchored to them should
// be dropped.
func (t *Terminal) Generation() uint64 { return t.generation }

// Screen line y (0 = top of the visible screen).
func (t *Terminal) ScreenLine(y int) *Line { return t.buf.screenLine(y, t.rows) }

// HistoryLen is the number of scrollback lines above the screen.
func (t *Terminal) HistoryLen() int { return t.buf.count() - t.rows }

// LineAt returns a line by index into history+screen, where 0 is the
// oldest retained line and HistoryLen() is the top of the screen.
func (t *Terminal) LineAt(i int) *Line {
	if i < 0 || i >= t.buf.count() {
		return nil
	}
	return t.buf.line(i)
}

// FirstAbs is the absolute number of the oldest retained line. Absolute
// numbers stay attached to content while output scrolls, until
// Generation changes.
func (t *Terminal) FirstAbs() int64 { return t.buf.evicted }

func (t *Terminal) newLine(fill Cell) *Line {
	if n := len(t.freeLines); n > 0 {
		l := t.freeLines[n-1]
		t.freeLines = t.freeLines[:n-1]
		l.reset(t.cols, fill)
		return l
	}
	return newLine(t.cols, fill)
}

func (t *Terminal) recycle(l *Line) {
	if len(t.freeLines) < 256 {
		t.freeLines = append(t.freeLines, l)
	}
}

// blank is the cell erase operations leave behind: the current background
// (xterm's "background color erase") and nothing else.
func (t *Terminal) blank() Cell {
	return Cell{Bg: t.cur.pen.bg}
}
