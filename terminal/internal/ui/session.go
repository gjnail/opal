package ui

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"time"

	"opal/internal/platform"
	"opal/terminal/internal/settings"
)

// A session is the windows, tabs and panes that were open when Opal
// Terminal last quit, with each pane's directory and recent output, so the
// next launch picks up where you left off.

const sessionScrollback = 2000 // lines of output kept per pane

type sessionFile struct {
	Version int             `json:"version"`
	Saved   time.Time       `json:"saved"`
	Windows []sessionWindow `json:"windows"`
}

type sessionWindow struct {
	Active int          `json:"active"`
	Tabs   []sessionTab `json:"tabs"`
}

type sessionTab struct {
	Title string       `json:"title,omitempty"`
	Focus int          `json:"focus"` // focused pane, in depth-first order
	Root  *sessionNode `json:"root"`
}

type sessionNode struct {
	Split string       `json:"split,omitempty"` // "cols" or "rows"; empty for a pane
	Ratio float64      `json:"ratio,omitempty"`
	A     *sessionNode `json:"a,omitempty"`
	B     *sessionNode `json:"b,omitempty"`
	Pane  *sessionPane `json:"pane,omitempty"`
}

type sessionPane struct {
	Profile    settings.Profile `json:"profile"`
	Cwd        string           `json:"cwd,omitempty"`
	Scrollback string           `json:"scrollback,omitempty"`
}

func sessionPath() string {
	return filepath.Join(platform.DataDir(), "terminal-session.json")
}

func loadSession() *sessionFile {
	b, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil
	}
	var s sessionFile
	if json.Unmarshal(b, &s) != nil || s.Version != 1 || len(s.Windows) == 0 {
		return nil
	}
	return &s
}

func saveSession(windows []sessionWindow) error {
	s := sessionFile{Version: 1, Saved: time.Now(), Windows: windows}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	path := sessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Write then rename, so a crash mid-write can't leave half a file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeSession() { os.Remove(sessionPath()) }

// snapshot captures the window for the session file.
func (w *Window) snapshot() *sessionWindow {
	sw := &sessionWindow{Active: w.active}
	for _, t := range w.tabs {
		var order []*Pane
		root := snapshotNode(t.root, &order)
		if root == nil {
			continue
		}
		st := sessionTab{Title: t.title, Root: root}
		for i, p := range order {
			if p == t.focus {
				st.Focus = i
			}
		}
		sw.Tabs = append(sw.Tabs, st)
	}
	if sw.Active >= len(sw.Tabs) {
		sw.Active = max(0, len(sw.Tabs)-1)
	}
	return sw
}

// snapshotNode records a subtree. Panes that never started (error panes)
// are left out, and a split with one side missing becomes the other side.
func snapshotNode(n *node, order *[]*Pane) *sessionNode {
	if n == nil {
		return nil
	}
	if n.dir == splitLeaf {
		p := n.pane
		if p == nil || p.profile.Name == "error" {
			return nil
		}
		*order = append(*order, p)
		return &sessionNode{Pane: &sessionPane{
			Profile:    p.profile,
			Cwd:        p.currentDir(),
			Scrollback: p.term.Dump(sessionScrollback),
		}}
	}
	a, b := snapshotNode(n.a, order), snapshotNode(n.b, order)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	split := "cols"
	if n.dir == splitRows {
		split = "rows"
	}
	return &sessionNode{Split: split, Ratio: n.ratio, A: a, B: b}
}

// restoreTabs recreates a saved window's tabs inside content.
func (w *Window) restoreTabs(sw *sessionWindow, content image.Rectangle) {
	for _, st := range sw.Tabs {
		var order []*Pane
		root := w.buildNode(st.Root, content, &order)
		if root == nil || len(order) == 0 {
			continue
		}
		t := &Tab{root: root, focus: order[0], title: st.Title}
		if st.Focus >= 0 && st.Focus < len(order) {
			t.focus = order[st.Focus]
		}
		w.tabs = append(w.tabs, t)
	}
	if len(w.tabs) > 0 {
		w.active = clampInt(sw.Active, 0, len(w.tabs)-1)
	}
}

func (w *Window) buildNode(sn *sessionNode, r image.Rectangle, order *[]*Pane) *node {
	if sn == nil {
		return nil
	}
	if sn.Pane != nil {
		sp := sn.Pane
		prof := sp.Profile
		if prof.Command == "" {
			prof = w.app.cfg.DefaultProfile()
		}
		p := w.startPaneReplay(prof, sp.Cwd, r, sp.Scrollback)
		*order = append(*order, p)
		return &node{pane: p}
	}
	ratio := sn.Ratio
	if ratio <= 0.05 || ratio >= 0.95 {
		ratio = 0.5
	}
	ra, rb := r, r
	dir := splitCols
	if sn.Split == "rows" {
		dir = splitRows
		h := int(float64(r.Dy()) * ratio)
		ra.Max.Y, rb.Min.Y = r.Min.Y+h, r.Min.Y+h
	} else {
		wd := int(float64(r.Dx()) * ratio)
		ra.Max.X, rb.Min.X = r.Min.X+wd, r.Min.X+wd
	}
	a := w.buildNode(sn.A, ra, order)
	b := w.buildNode(sn.B, rb, order)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	n := &node{dir: dir, a: a, b: b, ratio: ratio}
	a.parent, b.parent = n, n
	return n
}
