// Package ui is Opal Terminal's window layer, built on Gio: windows, tabs,
// split panes, input, and the chrome around the terminal grid.
package ui

import (
	"os"
	"sync"

	"gioui.org/app"

	"opal/terminal/internal/settings"
)

// App owns the windows.
type App struct {
	mu      sync.Mutex
	cfg     *settings.Config
	version string
	windows map[*Window]bool
	// settingsOnStart opens the settings page in the first window.
	settingsOnStart bool
}

// Options come from the command line.
type Options struct {
	Version string
	Profile *settings.Profile
	Dir     string
	// Settings opens the settings page in the first window.
	Settings bool
}

// Run opens the first window (or the windows saved from the last session)
// and runs until the last one closes.
func Run(cfg *settings.Config, o Options) {
	a := &App{cfg: cfg, version: o.Version, windows: map[*Window]bool{}, settingsOnStart: o.Settings}
	restored := false
	// A profile or directory on the command line asks for something
	// specific, so it doesn't bring back the old session.
	if cfg.Restore && o.Profile == nil && o.Dir == "" {
		if s := loadSession(); s != nil {
			for i := range s.Windows {
				if len(s.Windows[i].Tabs) > 0 {
					a.openWindowFrom(&s.Windows[i])
					restored = true
				}
			}
		}
	}
	if !restored {
		a.openWindow(o.Profile, o.Dir)
	}
	app.Main()
}

func (a *App) openWindow(prof *settings.Profile, dir string) {
	a.start(newWindow(a, prof, dir))
}

func (a *App) openWindowFrom(sw *sessionWindow) {
	w := newWindow(a, nil, "")
	w.restore = sw
	a.start(w)
}

func (a *App) start(w *Window) {
	a.mu.Lock()
	a.windows[w] = true
	w.openSettingsOnStart = a.settingsOnStart
	a.settingsOnStart = false
	a.mu.Unlock()
	go func() {
		snap := w.run()
		a.windowClosed(w, snap)
	}()
}

// windowClosed exits when the last window goes. Its tabs become the
// session to restore next time; closing its last tab instead means there
// is nothing to restore.
func (a *App) windowClosed(w *Window, snap *sessionWindow) {
	a.mu.Lock()
	delete(a.windows, w)
	last := len(a.windows) == 0
	restore := a.cfg.Restore
	a.mu.Unlock()
	if !last {
		return
	}
	if restore && snap != nil && len(snap.Tabs) > 0 {
		saveSession([]sessionWindow{*snap})
	} else {
		removeSession()
	}
	os.Exit(0)
}

// reload re-reads config.toml and applies what can change live: colors,
// keys, fonts.
func (a *App) reload() {
	cfg := settings.Load()
	a.mu.Lock()
	a.cfg = cfg
	ws := make([]*Window, 0, len(a.windows))
	for w := range a.windows {
		ws = append(ws, w)
	}
	a.mu.Unlock()
	for _, w := range ws {
		w.applyConfig(cfg)
	}
}

// applyConfig picks up new settings on the window's next frame.
func (w *Window) applyConfig(cfg *settings.Config) {
	w.keys = keymap(cfg.Keys)
	w.pal = cfg.Palette()
	w.chrome = newChrome(w.pal, cfg.Accent(), cfg.Gradient())
	w.fontPt = cfg.FontSize
	w.fonts = nil // rebuilt with the new family and size
	for _, t := range w.tabs {
		for _, p := range t.panes() {
			p.term.SetPalette(w.pal)
			p.flushCache()
		}
	}
	for _, msg := range cfg.Warnings {
		w.addToast("%s", msg)
	}
	w.invalidate()
}
