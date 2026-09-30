package ui

import (
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gioui.org/app"

	"opal/internal/config"
	"opal/internal/tools"
	"opal/terminal/internal/settings"
	"opal/terminal/internal/vt"
)

type actionDef struct {
	id    string
	title string
}

// actionList is every command, in the order the command palette shows
// them before any filtering.
func (w *Window) actionList() []actionDef {
	list := []actionDef{
		{"new_tab", "New tab"},
		{"split_right", "Split pane right"},
		{"split_down", "Split pane down"},
		{"split_auto", "Split pane (longest side)"},
		{"find", "Find in scrollback"},
		{"quick_select", "Quick select (copy a URL, path or hash by label)"},
		{"copy", "Copy selection"},
		{"paste", "Paste"},
		{"select_all", "Select all"},
		{"select_last_output", "Select output of the last command"},
		{"copy_last_output", "Copy output of the last command"},
		{"prev_prompt", "Jump to previous prompt"},
		{"next_prompt", "Jump to next prompt"},
		{"toggle_zoom", "Zoom pane"},
		{"close_pane", "Close pane"},
		{"close_tab", "Close tab"},
		{"duplicate_tab", "Duplicate tab"},
		{"rename_tab", "Rename tab"},
		{"new_window", "New window"},
		{"next_tab", "Next tab"},
		{"prev_tab", "Previous tab"},
		{"focus_left", "Focus pane left"},
		{"focus_right", "Focus pane right"},
		{"focus_up", "Focus pane up"},
		{"focus_down", "Focus pane down"},
		{"resize_left", "Move divider left"},
		{"resize_right", "Move divider right"},
		{"resize_up", "Move divider up"},
		{"resize_down", "Move divider down"},
		{"font_bigger", "Increase font size"},
		{"font_smaller", "Decrease font size"},
		{"font_reset", "Reset font size"},
		{"scroll_top", "Scroll to top"},
		{"scroll_bottom", "Scroll to bottom"},
		{"scroll_page_up", "Scroll up a page"},
		{"scroll_page_down", "Scroll down a page"},
		{"clear_scrollback", "Clear scrollback"},
		{"reset_terminal", "Reset terminal"},
		{"restart_pane", "Restart shell"},
		{"toggle_broadcast", "Broadcast input to all panes in tab"},
		{"toggle_fullscreen", "Toggle full screen"},
		{"open_config", "Open settings file"},
		{"reload_config", "Reload settings"},
	}
	for _, p := range w.app.cfg.ProfilesWithDetected() {
		list = append(list, actionDef{"profile:" + p.Name, "New tab: " + p.Name})
	}
	return list
}

// actionApplies lets keys through to full-screen programs when an action
// only makes sense at a shell (scrolling, prompt jumps).
func (w *Window) actionApplies(action string) bool {
	switch action {
	case "prev_prompt", "next_prompt", "scroll_up", "scroll_down", "scroll_page_up", "scroll_page_down", "scroll_top", "scroll_bottom", "select_last_output":
		p := w.activePane()
		if p == nil {
			return false
		}
		p.term.Lock()
		alt := p.term.AltScreen()
		p.term.Unlock()
		return !alt
	case "copy":
		p := w.activePane()
		return p != nil && p.sel.active
	}
	return true
}

func (w *Window) runAction(id string) { w.runActionSized(id, w.lastSize) }

func (w *Window) runActionSized(id string, size image.Point) {
	cfg := w.app.cfg
	t := w.activeTab()
	p := w.activePane()
	if size == (image.Point{}) {
		size = w.lastSize
	}
	defer w.invalidate()

	if name, ok := strings.CutPrefix(id, "profile:"); ok {
		for _, prof := range cfg.ProfilesWithDetected() {
			if prof.Name == name {
				w.newTab(prof, w.dirForNew(), size)
				return
			}
		}
		return
	}
	if n, ok := strings.CutPrefix(id, "goto_tab_"); ok {
		i, _ := strconv.Atoi(n)
		if i == 9 || i > len(w.tabs) {
			i = len(w.tabs)
		}
		if i >= 1 {
			w.activateTab(w.tabs[i-1])
		}
		return
	}

	switch id {
	case "new_tab":
		prof := cfg.DefaultProfile()
		if p != nil && p.profile.Name != "error" {
			prof = p.profile
		}
		w.newTab(prof, w.dirForNew(), size)
	case "duplicate_tab":
		if p != nil {
			w.newTab(p.profile, p.currentDir(), size)
		}
	case "new_window":
		prof := cfg.DefaultProfile()
		w.app.openWindow(&prof, w.dirForNew())
	case "close_pane":
		if p != nil {
			w.closePane(p)
		}
	case "close_tab":
		if t != nil {
			w.closeTab(t)
		}
	case "next_tab":
		if len(w.tabs) > 0 {
			w.activateTab(w.tabs[(w.active+1)%len(w.tabs)])
		}
	case "prev_tab":
		if len(w.tabs) > 0 {
			w.activateTab(w.tabs[(w.active+len(w.tabs)-1)%len(w.tabs)])
		}
	case "split_right":
		w.splitPane(splitCols)
	case "split_down":
		w.splitPane(splitRows)
	case "split_auto":
		if p != nil && p.rect.Dx() >= p.rect.Dy()*2 {
			w.splitPane(splitCols)
		} else {
			w.splitPane(splitRows)
		}
	case "focus_left", "focus_right", "focus_up", "focus_down":
		if t != nil && p != nil {
			dx, dy := dirOf(id)
			if q := t.neighbor(p, dx, dy); q != nil {
				w.focusPane(q)
			}
		}
	case "resize_left", "resize_right", "resize_up", "resize_down":
		if t != nil && p != nil {
			dx, dy := dirOf(id)
			t.resize(p, dx, dy, 0.04)
		}
	case "toggle_zoom":
		if t != nil && len(t.panes()) > 1 {
			if t.zoom != nil {
				t.zoom = nil
			} else {
				t.zoom = p
			}
		}
	case "copy":
		if p != nil && p.sel.active {
			w.copySelection(p)
		}
	case "paste":
		if p != nil {
			w.requestPaste(p)
		}
	case "select_all":
		if p != nil {
			p.term.Lock()
			cols, _ := p.term.Size()
			first := vt.Pos{Row: p.term.FirstAbs()}
			p.sel = selection{active: true, mode: selChar, anchor: first, anchorEnd: first, head: vt.Pos{Row: p.term.LastAbs(), Col: cols - 1}}
			p.term.Unlock()
		}
	case "select_last_output", "copy_last_output":
		if p != nil {
			w.selectLastOutput(p, id == "copy_last_output")
		}
	case "prev_prompt":
		w.jumpPrompt(-1)
	case "next_prompt":
		w.jumpPrompt(1)
	case "find":
		if p != nil {
			w.openSearch(p)
		}
	case "quick_select":
		if p != nil {
			w.openQuickSelect(p)
		}
	case "command_palette":
		w.openPalette("")
	case "rename_tab":
		if t != nil {
			w.openRename(t)
		}
	case "font_bigger":
		w.fontPt = min(w.fontPt+1, 72)
	case "font_smaller":
		w.fontPt = max(w.fontPt-1, 5)
	case "font_reset":
		w.fontPt = cfg.FontSize
	case "scroll_up":
		if p != nil {
			p.scrollBy(1)
		}
	case "scroll_down":
		if p != nil {
			p.scrollBy(-1)
		}
	case "scroll_page_up":
		if p != nil {
			p.scrollBy(max(1, p.rows-1))
		}
	case "scroll_page_down":
		if p != nil {
			p.scrollBy(-max(1, p.rows-1))
		}
	case "scroll_top":
		if p != nil {
			p.scrollBy(1 << 30)
		}
	case "scroll_bottom":
		if p != nil {
			p.scrollToBottom()
		}
	case "clear_scrollback":
		if p != nil {
			p.term.ClearScrollback()
			p.scrollToBottom()
			p.sel.clear()
		}
	case "reset_terminal":
		if p != nil {
			p.term.WriteString("\x1bc")
			p.send([]byte{0x0c}) // ask the shell to redraw its prompt
		}
	case "restart_pane":
		if p != nil {
			w.restartPane(p)
		}
	case "toggle_broadcast":
		if t != nil && p != nil {
			on := !p.broadcast
			for _, q := range t.panes() {
				q.broadcast = on
			}
			if on {
				w.addToast("Broadcasting input to %d panes", len(t.panes()))
			}
		}
	case "toggle_fullscreen":
		if w.mode == app.Fullscreen {
			w.gw.Option(app.Windowed.Option())
		} else {
			w.gw.Option(app.Fullscreen.Option())
		}
	case "open_config":
		w.openConfig()
	case "reload_config":
		w.app.reload()
	}
}

func dirOf(id string) (int, int) {
	switch {
	case strings.HasSuffix(id, "left"):
		return -1, 0
	case strings.HasSuffix(id, "right"):
		return 1, 0
	case strings.HasSuffix(id, "up"):
		return 0, -1
	}
	return 0, 1
}

// dirForNew is where a new tab or window starts: the active shell's
// directory.
func (w *Window) dirForNew() string {
	if p := w.activePane(); p != nil && p.profile.Name != "error" {
		return p.currentDir()
	}
	return ""
}

// jumpPrompt scrolls to the previous or next shell prompt.
func (w *Window) jumpPrompt(dir int) {
	p := w.activePane()
	if p == nil {
		return
	}
	p.term.Lock()
	rows := p.term.PromptRows()
	top := p.viewTopRow()
	anchor := p.term.FirstAbs() + int64(top) + 2
	p.term.Unlock()
	target := int64(-1)
	if dir < 0 {
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i] < anchor {
				target = rows[i]
				break
			}
		}
	} else {
		for _, r := range rows {
			if r > anchor {
				target = r
				break
			}
		}
	}
	if target < 0 {
		if dir > 0 {
			p.scrollToBottom()
		}
		return
	}
	p.scrollToAbs(target)
}

// selectLastOutput selects the output of the most recent finished command.
func (w *Window) selectLastOutput(p *Pane, copyIt bool) {
	p.term.Lock()
	rows := p.term.PromptRows()
	var from, to vt.Pos
	ok := false
	for i := len(rows) - 1; i >= 0 && !ok; i-- {
		from, to, ok = p.term.CommandOutput(rows[i])
	}
	if ok {
		p.sel = selection{active: true, mode: selChar, anchor: from, anchorEnd: from, head: to}
	}
	p.term.Unlock()
	if !ok {
		w.addToast("No command output found (shell integration needed)")
		return
	}
	p.scrollToAbs(from.Row - 1)
	if copyIt {
		w.copySelection(p)
	}
}

// openConfig opens config.toml, creating it from the CLI's template if
// it doesn't exist yet.
func (w *Window) openConfig() {
	path := config.Path()
	if _, err := os.Stat(path); err != nil {
		if _, _, err := config.WriteDefault(); err != nil {
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte("[terminal]\n"), 0o644)
		}
	}
	if err := tools.Open(path); err != nil {
		w.addToast("Couldn't open %s: %v", path, err)
	}
}

func openURL(u string) {
	go tools.Open(u)
}

// profileByName finds a profile, for the command line.
func profileByName(cfg *settings.Config, name string) (settings.Profile, bool) {
	for _, p := range cfg.ProfilesWithDetected() {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return settings.Profile{}, false
}
