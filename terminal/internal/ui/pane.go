package ui

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/op/paint"

	"opal/terminal/internal/pty"
	"opal/terminal/internal/render"
	"opal/terminal/internal/settings"
	"opal/terminal/internal/vt"
)

// Pane is one terminal: a shell process and the screen it draws on.
type Pane struct {
	win     *Window
	term    *vt.Terminal
	pty     pty.PTY
	profile settings.Profile
	id      int

	cols, rows int
	rect       image.Rectangle // last layout, window pixels
	grid       image.Point     // top-left of the cell grid, window pixels

	// scroll is how many lines the view is scrolled back from the bottom.
	// viewTop pins the view to an absolute line while output arrives.
	scroll  int
	viewTop int64
	gen     uint64

	sel    selection
	search *searchState

	cache   map[uint64]*rowImage
	retired []*rowImage
	frame   uint64

	exited   atomic.Bool
	exitCode atomic.Int32

	procMu   sync.Mutex
	procName string
	procPID  int

	cwd       string
	bellAt    time.Time
	progress  vt.EvProgress
	lastInput time.Time
	activity  bool // output arrived while the tab was in the background

	scrollAccum float32
	hover       hoverState
	mouseDown   bool
	pressedBtn  vt.MouseButton
	clicks      clickCounter
	broadcast   bool

	exitHandled    bool
	clipboardReply bool
	lastSeq        uint64
	images         map[uint64]*imgEntry
}

type rowImage struct {
	img  *image.RGBA
	op   paint.ImageOp
	used uint64
}

var paneIDs atomic.Int64

// newPane starts profile's program in a new terminal of the given size.
// replay, if set, is output from a previous session shown before the
// program starts.
func newPane(w *Window, prof settings.Profile, cwd string, cols, rows int, replay string) (*Pane, error) {
	cfg := w.app.cfg
	p := &Pane{
		win:     w,
		profile: prof,
		id:      int(paneIDs.Add(1)),
		cols:    cols,
		rows:    rows,
		cache:   map[uint64]*rowImage{},
		cwd:     cwd,
	}
	p.term = vt.New(vt.Options{
		Cols: cols, Rows: rows,
		Scrollback:         cfg.Scrollback,
		Palette:            w.pal,
		GraphemeClustering: cfg.Graphemes,
		Name:               "OpalTerminal",
		Version:            w.app.version,
	})
	m := w.rend.Metrics()
	p.term.SetCellSize(m.CellW, m.CellH)
	if replay != "" {
		p.term.WriteString(replay)
		p.term.WriteString("\x1b[m\x1b[2m[restored from the last session]\x1b[m\r\n")
		p.term.TakeReplies()
		p.term.TakeEvents()
	}

	path, err := resolveCommand(prof.Command)
	if err != nil {
		return nil, err
	}
	argv0 := filepath.Base(prof.Command)
	if prof.Login && runtime.GOOS != "windows" {
		argv0 = "-" + strings.TrimSuffix(argv0, filepath.Ext(argv0))
	}
	dir := expandHome(prof.Cwd)
	if dir == "" {
		dir = cwd
	}
	if dir == "" || !isDir(dir) {
		dir, _ = os.UserHomeDir()
	}
	p.cwd = dir
	proc, err := pty.Start(pty.Cmd{
		Path: path,
		Args: append([]string{argv0}, prof.Args...),
		Dir:  dir,
		Env:  childEnv(prof, w.app.version, p.id, replay != ""),
		Cols: cols, Rows: rows,
		Graphemes: cfg.Graphemes,
	})
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", prof.Command, err)
	}
	p.pty = proc
	go p.readLoop()
	go p.watchForeground()
	return p, nil
}

func resolveCommand(cmd string) (string, error) {
	if cmd == "" {
		return "", fmt.Errorf("profile has no command")
	}
	if filepath.IsAbs(cmd) {
		return cmd, nil
	}
	return exec.LookPath(expandHome(cmd))
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// childEnv is our environment minus variables that describe some other
// terminal, plus the ones that tell programs (and Opal's prompt) where
// they're running.
func childEnv(prof settings.Profile, version string, id int, restored bool) []string {
	drop := []string{"TERM_PROGRAM", "TERM_PROGRAM_VERSION", "WT_SESSION", "WT_PROFILE_ID", "TERM_SESSION_ID",
		"VTE_VERSION", "TERMINAL_EMULATOR", "TMUX", "TMUX_PANE", "STY", "COLUMNS", "LINES", "COLORTERM", "TERM",
		"OPAL_TERMINAL", "OPAL_TERMINAL_PANE",
		// Set by opal's shell hooks for the shell they run in; started from
		// such a shell (opal terminal), we'd otherwise hand them to a
		// different one.
		"OPAL_SHELL", "OPAL_SHELL_VERSION", "OPAL_BIN", "OPAL_GREETED", "OPAL_ADMIN", "OPAL_PSRL"}
	dropPrefix := []string{"KITTY_", "ALACRITTY_", "WEZTERM_", "KONSOLE_", "ITERM_", "VSCODE_", "GHOSTTY_"}
	var env []string
outer:
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		ku := strings.ToUpper(k)
		for _, d := range drop {
			if ku == d {
				continue outer
			}
		}
		for _, d := range dropPrefix {
			if strings.HasPrefix(ku, d) {
				continue outer
			}
		}
		env = append(env, kv)
	}
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=OpalTerminal",
		"TERM_PROGRAM_VERSION="+version,
		"OPAL_TERMINAL=1",
		fmt.Sprintf("TERM_SESSION_ID=opal-%d-%d", os.Getpid(), id),
	)
	if restored {
		// The shell is continuing an earlier session, so Opal's greeting
		// for a new terminal would be out of place.
		env = append(env, "OPAL_GREETING=0")
	}
	for k, v := range prof.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// readLoop copies the program's output into the terminal. With
// OPAL_TERMINAL_TRACE set to a file path, the raw output is also appended
// there, which is how escape-sequence bugs get diagnosed.
func (p *Pane) readLoop() {
	buf := make([]byte, 64<<10)
	var trace *os.File
	if path := os.Getenv("OPAL_TERMINAL_TRACE"); path != "" {
		trace, _ = os.OpenFile(fmt.Sprintf("%s.%d", path, p.id), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	}
	if trace != nil {
		defer trace.Close()
	}
	for {
		n, err := p.pty.Read(buf)
		if n > 0 && trace != nil {
			trace.Write(buf[:n])
		}
		if n > 0 {
			p.term.Write(buf[:n])
			if r := p.term.TakeReplies(); len(r) > 0 {
				p.pty.Write(r)
			}
			p.win.invalidate()
		}
		if err != nil {
			break
		}
	}
	code, _ := p.pty.Wait()
	p.exitCode.Store(int32(code))
	p.exited.Store(true)
	p.win.invalidate()
}

// watchForeground keeps track of what's running, for tab titles.
func (p *Pane) watchForeground() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		if p.exited.Load() {
			return
		}
		name, pid := p.pty.Foreground()
		p.procMu.Lock()
		changed := name != p.procName
		p.procName, p.procPID = name, pid
		p.procMu.Unlock()
		if changed {
			p.win.invalidate()
		}
	}
}

// send writes input to the program, clearing any selection-related view
// state the way typing does.
func (p *Pane) send(b []byte) {
	if len(b) == 0 || p.exited.Load() {
		return
	}
	p.lastInput = time.Now()
	p.scrollToBottom()
	p.pty.Write(b)
}

func (p *Pane) sendKey(ev vt.KeyEvent) {
	b := p.term.EncodeKey(ev)
	if p.broadcast {
		p.win.broadcast(p, b)
	}
	p.send(b)
}

func (p *Pane) paste(text string) {
	if text == "" {
		return
	}
	b := p.term.Paste(text)
	if p.broadcast {
		p.win.broadcast(p, b)
	}
	p.send(b)
}

func (p *Pane) scrollToBottom() {
	p.scroll = 0
}

// resize changes the grid size, telling the program.
func (p *Pane) resize(cols, rows int) {
	if cols == p.cols && rows == p.rows {
		return
	}
	p.cols, p.rows = cols, rows
	p.term.Resize(cols, rows)
	m := p.win.rend.Metrics()
	p.pty.Resize(cols, rows, cols*m.CellW, rows*m.CellH)
	p.flushCache()
}

func (p *Pane) flushCache() {
	for _, ri := range p.cache {
		p.retired = append(p.retired, ri)
	}
	clear(p.cache)
}

// title is what the tab shows for this pane.
func (p *Pane) title() string {
	p.term.Lock()
	t := p.term.Title()
	p.term.Unlock()
	// ConPTY sets the title to the executable's full path until the shell
	// sets its own; the profile's name says more.
	if t != "" && !(strings.HasSuffix(strings.ToLower(t), ".exe") && strings.ContainsAny(t, `\/`)) {
		return t
	}
	if runtime.GOOS == "windows" && p.profile.Name != "" {
		return p.profile.Name
	}
	p.procMu.Lock()
	name := p.procName
	p.procMu.Unlock()
	if name != "" {
		return name
	}
	return p.profile.Name
}

// currentDir is the best guess at the shell's working directory: what
// shell integration reported, else what the OS says.
func (p *Pane) currentDir() string {
	p.term.Lock()
	d := p.term.CWD()
	p.term.Unlock()
	if d != "" && isDir(d) {
		return d
	}
	p.procMu.Lock()
	pid := p.procPID
	p.procMu.Unlock()
	if pid == 0 {
		pid = p.pty.Pid()
	}
	if d := pty.ProcessCWD(pid); d != "" {
		return d
	}
	return p.cwd
}

func (p *Pane) close() {
	p.pty.Kill()
	p.pty.Close()
}

// viewTopRow returns the index (into history+screen) of the first line
// shown, keeping a scrolled-back view pinned as output arrives. The
// caller holds the terminal lock.
func (p *Pane) viewTopRow() int {
	hist := p.term.HistoryLen()
	if g := p.term.Generation(); g != p.gen {
		p.gen = g
		p.scroll = 0
		p.sel.clear()
	}
	if p.scroll > 0 {
		screenTop := p.term.ScreenTopAbs()
		p.scroll = int(screenTop - p.viewTop)
	}
	p.scroll = clampInt(p.scroll, 0, hist)
	if p.scroll > 0 {
		p.viewTop = p.term.ScreenTopAbs() - int64(p.scroll)
	}
	return hist - p.scroll
}

// scrollBy moves the view; positive is back into history.
func (p *Pane) scrollBy(lines int) {
	p.term.Lock()
	hist := p.term.HistoryLen()
	p.scroll = clampInt(p.scroll+lines, 0, hist)
	p.viewTop = p.term.ScreenTopAbs() - int64(p.scroll)
	p.term.Unlock()
}

// scrollToAbs makes an absolute row visible, a few lines from the top.
func (p *Pane) scrollToAbs(row int64) {
	p.term.Lock()
	top := p.term.ScreenTopAbs()
	hist := p.term.HistoryLen()
	want := int(top - row + 2)
	p.scroll = clampInt(want, 0, hist)
	p.viewTop = top - int64(p.scroll)
	p.term.Unlock()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// cursorShape maps the program's DECSCUSR choice and our config to a
// renderer cursor.
func (p *Pane) cursorShape(focused, blinkOn bool) render.CursorShape {
	if !p.term.CursorVisible() || p.scroll > 0 {
		return render.CursorNone
	}
	if !focused {
		return render.CursorHollow
	}
	style := p.term.CursorShape()
	blink := p.win.app.cfg.CursorBlink
	switch style {
	case vt.CursorBlinkBlock, vt.CursorBlinkBar, vt.CursorBlinkUnderline:
		blink = true
	case vt.CursorSteadyBlock, vt.CursorSteadyBar, vt.CursorSteadyUnderline:
		blink = false
	}
	if blink && !blinkOn {
		return render.CursorNone
	}
	switch style {
	case vt.CursorBlinkBar, vt.CursorSteadyBar:
		return render.CursorBar
	case vt.CursorBlinkUnderline, vt.CursorSteadyUnderline:
		return render.CursorUnderline
	case vt.CursorBlinkBlock, vt.CursorSteadyBlock:
		return render.CursorBlock
	}
	switch p.win.app.cfg.CursorStyle {
	case "bar":
		return render.CursorBar
	case "underline":
		return render.CursorUnderline
	}
	return render.CursorBlock
}

// cursorBlinks reports whether the cursor is currently blinking, so the
// window knows to schedule redraws.
func (p *Pane) cursorBlinks() bool {
	switch p.term.CursorShape() {
	case vt.CursorBlinkBlock, vt.CursorBlinkBar, vt.CursorBlinkUnderline:
		return true
	case vt.CursorSteadyBlock, vt.CursorSteadyBar, vt.CursorSteadyUnderline:
		return false
	}
	return p.win.app.cfg.CursorBlink
}
