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
}

// Options come from the command line.
type Options struct {
	Version string
	Profile *settings.Profile
	Dir     string
}

// Run opens the first window and runs until the last one closes.
func Run(cfg *settings.Config, o Options) {
	a := &App{cfg: cfg, version: o.Version, windows: map[*Window]bool{}}
	a.openWindow(o.Profile, o.Dir)
	app.Main()
}

func (a *App) openWindow(prof *settings.Profile, dir string) {
	w := newWindow(a, prof, dir)
	a.mu.Lock()
	a.windows[w] = true
	a.mu.Unlock()
	go func() {
		w.run()
		a.mu.Lock()
		delete(a.windows, w)
		n := len(a.windows)
		a.mu.Unlock()
		if n == 0 {
			os.Exit(0)
		}
	}()
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
