package vt

import (
	"regexp"
	"testing"
)

func TestTextExtraction(t *testing.T) {
	term := newTerm(10, 4)
	term.WriteString("0123456789abc\r\nsecond   \r\nthird")
	top := term.ScreenTopAbs()
	// The first logical line wrapped: copying it must not add a newline.
	got := term.Text(Pos{top, 0}, Pos{top + 2, 9}, false)
	if got != "0123456789abc\nsecond" {
		t.Fatalf("text = %q", got)
	}
	if got := term.Text(Pos{top + 2, 2}, Pos{top + 3, 3}, true); got != "co\nir" {
		t.Fatalf("block text = %q", got)
	}
}

func TestWordAndLogicalLine(t *testing.T) {
	term := newTerm(20, 3)
	term.WriteString("git commit -m ok ===")
	row := term.ScreenTopAbs()
	a, b := term.WordBounds(Pos{row, 5}, "-_./")
	if a.Col != 4 || b.Col != 9 {
		t.Fatalf("word = %d..%d", a.Col, b.Col)
	}
	a, b = term.WordBounds(Pos{row, 18}, "")
	if a.Col != 17 || b.Col != 19 {
		t.Fatalf("run of = : %d..%d", a.Col, b.Col)
	}
	term = newTerm(5, 4)
	term.WriteString("abcdefghij\r\nx")
	top := term.ScreenTopAbs()
	f, l := term.LogicalLine(top + 1)
	if f != top || l != top+1 {
		t.Fatalf("logical line = %d..%d", f-top, l-top)
	}
}

func TestSearch(t *testing.T) {
	term := newTerm(8, 4)
	term.WriteString("hello wo\r\nrld and hello\r\n")
	ms := term.Search(regexp.MustCompile(`(?i)hello`), 0)
	if len(ms) != 2 {
		t.Fatalf("matches = %+v", ms)
	}
	// "world" spans the wrap between rows 0 and... it doesn't: row 0 isn't
	// wrapped (explicit CRLF). Check a match that does span a wrap.
	term = newTerm(5, 3)
	term.WriteString("abcdefgh")
	ms = term.Search(regexp.MustCompile(`def`), 0)
	if len(ms) != 1 || ms[0].Start.Col != 3 || ms[0].End.Col != 0 {
		t.Fatalf("wrapped match = %+v", ms)
	}
}

func TestURLDetection(t *testing.T) {
	term := newTerm(40, 2)
	term.WriteString("see (https://example.com/a_(b)) now.")
	row := term.ScreenTopAbs()
	u, a, b, ok := term.URLAt(Pos{row, 10})
	if !ok || u != "https://example.com/a_(b)" || a.Col != 5 || b.Col != 29 {
		t.Fatalf("url %q %d..%d ok=%v", u, a.Col, b.Col, ok)
	}
	if _, _, _, ok := term.URLAt(Pos{row, 1}); ok {
		t.Fatal("no URL at col 1")
	}
	term.WriteString("\r\n\x1b]8;;https://opal.dev\x1b\\docs\x1b]8;;\x1b\\")
	if u, _, _, ok := term.URLAt(Pos{row + 1, 2}); !ok || u != "https://opal.dev" {
		t.Fatalf("osc 8 link %q", u)
	}
}

func TestPromptNavigation(t *testing.T) {
	term := newTerm(20, 10)
	term.WriteString("\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07a\r\nb\r\n\x1b]133;D;0\x07\x1b]133;A\x07$ ")
	rows := term.PromptRows()
	if len(rows) != 2 {
		t.Fatalf("prompts = %v", rows)
	}
	from, to, ok := term.CommandOutput(rows[0])
	if !ok || term.Text(from, to, false) != "a\nb" {
		t.Fatalf("output = %q ok=%v", term.Text(from, to, false), ok)
	}
}
