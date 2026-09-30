package ui

import (
	"image"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"opal/internal/history"
	"opal/internal/jump"
	"opal/terminal/internal/vt"
)

func TestChords(t *testing.T) {
	cases := []struct {
		mods key.Modifiers
		name key.Name
		want string
	}{
		{key.ModCtrl | key.ModShift, "T", "ctrl+shift+t"},
		{key.ModAlt | key.ModShift, "+", "alt+shift+plus"},
		{key.ModCtrl, key.NameUpArrow, "ctrl+up"},
		{key.ModShift | key.ModCtrl, key.NameTab, "ctrl+shift+tab"},
		{key.ModCommand, ",", "super+comma"},
	}
	for _, c := range cases {
		if got := chord(c.mods, c.name); got != c.want {
			t.Errorf("chord(%v, %q) = %q, want %q", c.mods, c.name, got, c.want)
		}
	}
	for in, want := range map[string]string{
		"Ctrl+Shift+T":   "ctrl+shift+t",
		"shift+ctrl+t":   "ctrl+shift+t",
		"cmd+=":          "super+plus",
		"ctrl++":         "ctrl+plus",
		"Alt+Shift+Plus": "alt+shift+plus",
		"ctrl+esc":       "ctrl+escape",
	} {
		if got := normalizeChord(in); got != want {
			t.Errorf("normalizeChord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeymapOverrides(t *testing.T) {
	km := keymap(map[string]string{"Ctrl+Shift+T": "none", "ctrl+alt+k": "clear_scrollback"})
	if _, ok := km["ctrl+shift+t"]; ok {
		t.Error("binding to none should remove the default")
	}
	if km["ctrl+alt+k"] != "clear_scrollback" {
		t.Error("user binding missing")
	}
}

func TestToVTKey(t *testing.T) {
	// Plain letters come through EditEvent, not key events.
	if _, ok := toVTKey(key.Event{Name: "A"}, false); ok {
		t.Error("plain A should be left to the text path")
	}
	ev, ok := toVTKey(key.Event{Name: "C", Modifiers: key.ModCtrl}, false)
	if !ok || ev.Key != vt.KeyChar || ev.Rune != 'c' || ev.Mods != vt.ModCtrl {
		t.Errorf("ctrl+c = %+v ok=%v", ev, ok)
	}
	ev, ok = toVTKey(key.Event{Name: key.NameUpArrow, Modifiers: key.ModShift}, false)
	if !ok || ev.Key != vt.KeyUp || ev.Mods != vt.ModShift {
		t.Errorf("shift+up = %+v", ev)
	}
	// With kitty's report-all-keys flag, plain text is a key event too.
	if _, ok := toVTKey(key.Event{Name: "A"}, true); !ok {
		t.Error("kitty flag 8 should report plain keys")
	}
}

func TestSplitTree(t *testing.T) {
	a, b, c := &Pane{id: 1}, &Pane{id: 2}, &Pane{id: 3}
	tab := newTab(a)
	tab.split(a, b, splitCols)
	tab.split(b, c, splitRows)
	if got := len(tab.panes()); got != 3 {
		t.Fatalf("panes = %d", got)
	}
	tab.layout(image.Rect(0, 0, 1000, 600), 2)
	if a.rect.Dx() != 499 || b.rect.Min.X != 501 {
		t.Fatalf("columns: a=%v b=%v", a.rect, b.rect)
	}
	if b.rect.Max.Y != 299 || c.rect.Min.Y != 301 {
		t.Fatalf("rows: b=%v c=%v", b.rect, c.rect)
	}
	if tab.neighbor(a, 1, 0) == nil || tab.neighbor(b, 0, 1) != c || tab.neighbor(c, 0, -1) != b {
		t.Fatal("directional focus is wrong")
	}
	tab.resize(a, 1, 0, 0.1)
	tab.layout(image.Rect(0, 0, 1000, 600), 2)
	if a.rect.Dx() <= 499 {
		t.Fatal("resize right should grow the left pane")
	}
	if tab.remove(b) {
		t.Fatal("tab isn't empty yet")
	}
	tab.layout(image.Rect(0, 0, 1000, 600), 2)
	if c.rect.Dy() != 600 {
		t.Fatalf("c should take b's space, got %v", c.rect)
	}
	tab.remove(c)
	if !tab.remove(a) {
		t.Fatal("removing the last pane should empty the tab")
	}
}

func TestFuzzyScore(t *testing.T) {
	if fuzzyScore("spr", "Split pane right") <= fuzzyScore("spr", "Scroll up a page") {
		t.Error("word-start matches should rank higher")
	}
	if fuzzyScore("xyz", "Split pane right") >= 0 {
		t.Error("non-matching query should fail")
	}
	if fuzzyScore("", "anything") != 0 {
		t.Error("empty query matches everything")
	}
}

func TestSelectionColumns(t *testing.T) {
	var s selection
	term := vt.New(vt.Options{Cols: 20, Rows: 5})
	term.WriteString("hello world")
	row := term.ScreenTopAbs()
	s.start(term, vt.Pos{Row: row, Col: 7}, selWord, "")
	if from, to := s.columns(row, 20); from != 6 || to != 11 {
		t.Fatalf("word selection = %d..%d", from, to)
	}
	s.start(term, vt.Pos{Row: row, Col: 2}, selChar, "")
	s.extend(term, vt.Pos{Row: row + 1, Col: 3}, "")
	if from, to := s.columns(row, 20); from != 2 || to != 20 {
		t.Fatalf("first row of a two-row selection = %d..%d", from, to)
	}
	if from, to := s.columns(row+1, 20); from != 0 || to != 4 {
		t.Fatalf("second row = %d..%d", from, to)
	}
	// Dragging backwards past the anchor keeps the anchor's cell selected.
	s.start(term, vt.Pos{Row: row, Col: 5}, selChar, "")
	s.extend(term, vt.Pos{Row: row, Col: 1}, "")
	if from, to := s.columns(row, 20); from != 1 || to != 6 {
		t.Fatalf("backwards = %d..%d", from, to)
	}
}

func TestQuickSelectLabels(t *testing.T) {
	for _, n := range []int{1, 5, 26, 27, 60, 300} {
		ls := labels(n)
		if len(ls) != n {
			t.Fatalf("labels(%d) gave %d", n, len(ls))
		}
		seen := map[string]bool{}
		for _, a := range ls {
			if seen[a] {
				t.Fatalf("labels(%d): duplicate %q", n, a)
			}
			seen[a] = true
			for _, b := range ls {
				if a != b && strings.HasPrefix(b, a) {
					t.Fatalf("labels(%d): %q is a prefix of %q", n, a, b)
				}
			}
		}
	}
	if labels(3)[0] != "a" {
		t.Error("home row first")
	}
}

func TestQuickPatterns(t *testing.T) {
	text := "see https://example.com/x and C:\\Users\\me\\src or ~/src/opal/main.go, commit 3f2a9c1 at 10.0.0.1:8080"
	got := quickPatterns.FindAllString(text, -1)
	want := []string{"https://example.com/x", `C:\Users\me\src`, "~/src/opal/main.go", "3f2a9c1", "10.0.0.1:8080"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestPaletteHistoryAndDirs(t *testing.T) {
	data := t.TempDir()
	t.Setenv("OPAL_DATA_DIR", data)
	now := time.Now().Unix()
	for i, c := range []string{"git status", "go test ./...", "git status", "docker ps"} {
		if err := history.Append(history.Entry{When: now - int64(100-i), Status: 0, DurMS: 5, Shell: "pwsh", Cmd: c}); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	if err := jump.Add(dir); err != nil {
		t.Fatal(err)
	}
	po := &paletteOverlay{loaded: map[byte]bool{}}
	po.edit.set("!git")
	po.filter()
	if len(po.items) != 1 || po.items[0].title != "git status" || po.items[0].run == nil {
		t.Fatalf("history items = %+v", po.items)
	}
	po.edit.set("!")
	po.filter()
	if len(po.items) != 3 || po.items[0].title != "docker ps" {
		t.Fatalf("all history (newest first, unique) = %+v", po.items)
	}
	po.edit.set("@" + filepath.Base(dir))
	po.filter()
	if len(po.items) != 1 {
		t.Fatalf("dir items = %+v", po.items)
	}
}

// recordPTY captures what the terminal sends to the program.
type recordPTY struct {
	deadPTY
	sent []byte
}

func (r *recordPTY) Write(b []byte) (int, error) {
	r.sent = append(r.sent, b...)
	return len(b), nil
}

func TestClickToMove(t *testing.T) {
	term := vt.New(vt.Options{Cols: 40, Rows: 3})
	// Prompt "$ ", then the typed command "echo 中文 hi" with the cursor
	// at its end.
	term.WriteString("\x1b]133;A\x07$ \x1b]133;B\x07echo 中文 hi")
	rec := &recordPTY{}
	p := &Pane{term: term, pty: rec}
	// Click on the "e" of "echo": the cursor is after "hi", 10 characters
	// (11 cells, one wide pair) to the right.
	p.clickToMove(image.Pt(2, 0))
	if got, want := string(rec.sent), strings.Repeat("\x1b[D", 10); got != want {
		t.Fatalf("sent %q, want %q", got, want)
	}
	// Clicks in the prompt itself go to the start of the input.
	rec.sent = nil
	p.clickToMove(image.Pt(0, 0))
	if strings.Count(string(rec.sent), "\x1b[D") != 10 {
		t.Fatalf("click on prompt sent %q", rec.sent)
	}
	// Other rows do nothing.
	rec.sent = nil
	p.clickToMove(image.Pt(2, 1))
	if len(rec.sent) != 0 {
		t.Fatal("click off the prompt row must not move the cursor")
	}
}

func TestLineEdit(t *testing.T) {
	var e lineEdit
	e.insert("git commit")
	e.key(key.Event{Name: key.NameDeleteBackward, Modifiers: key.ModCtrl})
	if e.String() != "git " {
		t.Fatalf("ctrl+backspace = %q", e.String())
	}
	e.key(key.Event{Name: key.NameHome})
	e.insert("> ")
	if e.String() != "> git " || e.cur != 2 {
		t.Fatalf("home+insert = %q cur=%d", e.String(), e.cur)
	}
}
