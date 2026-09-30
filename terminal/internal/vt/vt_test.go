package vt

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func newTerm(cols, rows int) *Terminal {
	return New(Options{Cols: cols, Rows: rows, Scrollback: 1000, GraphemeClustering: true, Version: "0.1.0"})
}

// screen returns the visible rows with trailing blanks trimmed.
func screen(t *Terminal) []string {
	out := make([]string, t.rows)
	for y := 0; y < t.rows; y++ {
		out[y] = t.ScreenLine(y).String()
	}
	return out
}

func history(t *Terminal) []string {
	var out []string
	for i := 0; i < t.HistoryLen(); i++ {
		out = append(out, t.LineAt(i).String())
	}
	return out
}

func expectScreen(tb testing.TB, t *Terminal, want ...string) {
	tb.Helper()
	got := screen(t)
	for len(want) < len(got) {
		want = append(want, "")
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		tb.Fatalf("screen mismatch\n got: %q\nwant: %q", got, want)
	}
}

func expectCursor(tb testing.TB, t *Terminal, x, y int) {
	tb.Helper()
	if cx, cy := t.CursorPos(); cx != x || cy != y {
		tb.Fatalf("cursor at (%d,%d), want (%d,%d)", cx, cy, x, y)
	}
}

func TestPrintAndNewlines(t *testing.T) {
	term := newTerm(10, 3)
	term.WriteString("hello\r\nworld")
	expectScreen(t, term, "hello", "world")
	expectCursor(t, term, 5, 1)
}

func TestAutowrapAndPendingWrap(t *testing.T) {
	term := newTerm(5, 3)
	term.WriteString("abcde")
	// The cursor stays on the last column with a wrap pending.
	expectCursor(t, term, 4, 0)
	if !term.cur.pendingWrap {
		t.Fatal("expected pending wrap")
	}
	term.WriteString("f")
	expectScreen(t, term, "abcde", "f")
	if !term.ScreenLine(0).Wrapped {
		t.Fatal("line 0 should be marked wrapped")
	}
	// CR cancels the pending wrap.
	term = newTerm(5, 3)
	term.WriteString("abcde\rX")
	expectScreen(t, term, "Xbcde")
}

func TestAutowrapOff(t *testing.T) {
	term := newTerm(5, 2)
	term.WriteString("\x1b[?7labcdefg")
	expectScreen(t, term, "abcdg")
}

func TestCursorMovement(t *testing.T) {
	term := newTerm(20, 10)
	term.WriteString("\x1b[5;10H")
	expectCursor(t, term, 9, 4)
	term.WriteString("\x1b[2A")
	expectCursor(t, term, 9, 2)
	term.WriteString("\x1b[100B")
	expectCursor(t, term, 9, 9)
	term.WriteString("\x1b[3D\x1b[C")
	expectCursor(t, term, 7, 9)
	term.WriteString("\x1b[H")
	expectCursor(t, term, 0, 0)
	term.WriteString("\x1b[3G\x1b[4d")
	expectCursor(t, term, 2, 3)
	term.WriteString("\x1b[2E")
	expectCursor(t, term, 0, 5)
	term.WriteString("\x1b[5;5H\x1b[F")
	expectCursor(t, term, 0, 3)
}

func TestOriginMode(t *testing.T) {
	term := newTerm(20, 10)
	term.WriteString("\x1b[3;7r\x1b[?6h")
	expectCursor(t, term, 0, 2)
	term.WriteString("\x1b[10;1H") // clamps to the region
	expectCursor(t, term, 0, 6)
	term.WriteString("\x1b[6n")
	if got := string(term.TakeReplies()); got != "\x1b[5;1R" {
		t.Fatalf("CPR in origin mode = %q", got)
	}
}

func TestEraseWithBackgroundColor(t *testing.T) {
	term := newTerm(10, 3)
	term.WriteString("abcdefghij\x1b[1;4H\x1b[41m\x1b[K")
	expectScreen(t, term, "abc")
	c := term.ScreenLine(0).Cells[5]
	if i, ok := c.Bg.Index(); !ok || i != 1 {
		t.Fatalf("erased cell bg = %v, want red", c.Bg)
	}
	term.WriteString("\x1b[1K")
	expectScreen(t, term, "")
}

func TestEraseDisplayKeepsHistory(t *testing.T) {
	term := newTerm(10, 3)
	term.WriteString("one\r\ntwo\r\nthree")
	term.WriteString("\x1b[H\x1b[2J")
	expectScreen(t, term, "", "", "")
	if h := history(term); strings.Join(h, ",") != "one,two,three" {
		t.Fatalf("history after ED 2 = %q", h)
	}
	term.WriteString("\x1b[3J")
	if term.HistoryLen() != 0 {
		t.Fatal("ED 3 should clear history")
	}
}

func TestScrollbackAndLimit(t *testing.T) {
	term := New(Options{Cols: 10, Rows: 3, Scrollback: 5})
	for i := 0; i < 20; i++ {
		fmt.Fprintf(term, "line%d\r\n", i)
	}
	if term.HistoryLen() != 5 {
		t.Fatalf("history len = %d, want 5", term.HistoryLen())
	}
	expectScreen(t, term, "line18", "line19", "")
	if h := history(term); h[0] != "line13" {
		t.Fatalf("oldest history line = %q", h[0])
	}
	if term.FirstAbs() != 13 {
		t.Fatalf("FirstAbs = %d, want 13", term.FirstAbs())
	}
}

func TestScrollRegion(t *testing.T) {
	term := newTerm(10, 5)
	term.WriteString("1\r\n2\r\n3\r\n4\r\n5")
	term.WriteString("\x1b[2;4r\x1b[4;1H\n")
	expectScreen(t, term, "1", "3", "4", "", "5")
	if term.HistoryLen() != 0 {
		t.Fatal("scrolling a region that doesn't start at the top must not add history")
	}
	term.WriteString("\x1b[2;1H\x1bM")
	expectScreen(t, term, "1", "", "3", "4", "5")
}

func TestRegionAtTopFeedsHistory(t *testing.T) {
	term := newTerm(10, 4)
	term.WriteString("a\r\nb\r\nc\r\nstatus")
	term.WriteString("\x1b[1;3r\x1b[3;1H\nd")
	expectScreen(t, term, "b", "c", "d", "status")
	if h := history(term); len(h) != 1 || h[0] != "a" {
		t.Fatalf("history = %q", h)
	}
}

func TestInsertDeleteChars(t *testing.T) {
	term := newTerm(10, 2)
	term.WriteString("abcdef\x1b[1;3H\x1b[2@")
	expectScreen(t, term, "ab  cdef")
	term.WriteString("\x1b[3P")
	expectScreen(t, term, "abdef")
	term.WriteString("\x1b[2X")
	expectScreen(t, term, "ab  f")
}

func TestInsertDeleteLines(t *testing.T) {
	term := newTerm(5, 4)
	term.WriteString("1\r\n2\r\n3\r\n4\x1b[2;1H\x1b[L")
	expectScreen(t, term, "1", "", "2", "3")
	term.WriteString("\x1b[2M")
	expectScreen(t, term, "1", "3", "", "")
}

func TestInsertMode(t *testing.T) {
	term := newTerm(10, 1)
	term.WriteString("abc\x1b[1G\x1b[4hXY\x1b[4lZ")
	expectScreen(t, term, "XYZbc")
}

func TestTabs(t *testing.T) {
	term := newTerm(30, 1)
	term.WriteString("a\tb\tc")
	expectScreen(t, term, "a       b       c")
	term.WriteString("\x1b[3g\x1b[1G\x1b[5C\x1bH\x1b[1G\tX")
	expectCursor(t, term, 6, 0)
	term.WriteString("\x1b[Z")
	expectCursor(t, term, 5, 0)
}

func TestWideCharacters(t *testing.T) {
	term := newTerm(6, 2)
	term.WriteString("中文")
	expectScreen(t, term, "中文")
	expectCursor(t, term, 4, 0)
	l := term.ScreenLine(0)
	if !l.Cells[0].Wide() || !l.Cells[1].Spacer() {
		t.Fatal("wide char cells not marked")
	}
	// A wide char that doesn't fit wraps, leaving padding.
	term = newTerm(5, 2)
	term.WriteString("abcd中")
	expectScreen(t, term, "abcd", "中")
	// Overwriting half of a wide char blanks the other half.
	term = newTerm(6, 1)
	term.WriteString("中文\x1b[1;2Hx")
	expectScreen(t, term, " x文")
}

func TestGraphemeClusters(t *testing.T) {
	term := newTerm(10, 1)
	family := "👨\u200d👩\u200d👧"
	term.WriteString("a" + family + "b")
	l := term.ScreenLine(0)
	if got := l.Text(1); got != family {
		t.Fatalf("cluster = %q", got)
	}
	if l.Text(3) != "b" {
		t.Fatalf("after cluster = %q (cursor must advance by the cluster's width)", l.Text(3))
	}
	// Combining mark joins the previous cell.
	term = newTerm(10, 1)
	term.WriteString("e\u0301x")
	if l := term.ScreenLine(0); l.Text(0) != "e\u0301" || l.Text(1) != "x" {
		t.Fatalf("combining: %q %q", l.Text(0), l.Text(1))
	}
	// Flags are two regional indicators in one double-width cell.
	term = newTerm(10, 1)
	term.WriteString("🇯🇵🇺🇸")
	if l := term.ScreenLine(0); l.Text(0) != "🇯🇵" || l.Text(2) != "🇺🇸" {
		t.Fatalf("flags: %q %q", l.Text(0), l.Text(2))
	}
	// VS16 widens a text-default emoji in grapheme mode.
	term = newTerm(10, 1)
	term.WriteString("\u2764\ufe0fx")
	expectCursor(t, term, 3, 0)
	if !term.ScreenLine(0).Cells[0].Wide() {
		t.Fatal("heart + VS16 should be wide")
	}
}

func TestLegacyWidths(t *testing.T) {
	term := New(Options{Cols: 20, Rows: 1})
	term.WriteString("\u2764\ufe0fx")
	expectCursor(t, term, 2, 0) // VS16 is zero width without mode 2027
	term = New(Options{Cols: 20, Rows: 1})
	term.WriteString("👨\u200d👩")
	expectCursor(t, term, 4, 0) // each emoji takes its own two cells
	term.WriteString("\x1b[?2027$p")
	if got := string(term.TakeReplies()); got != "\x1b[?2027;2$y" {
		t.Fatalf("DECRQM 2027 = %q", got)
	}
}

func TestSGR(t *testing.T) {
	term := newTerm(20, 1)
	term.WriteString("\x1b[1;3;4:3;38;5;196;48:2::10:20:30;58;2;1;2;3mX\x1b[0mY")
	l := term.ScreenLine(0)
	c := l.Cells[0]
	if c.A&AttrBold == 0 || c.A&AttrItalic == 0 || c.A.Underline() != UnderlineCurly {
		t.Fatalf("attrs = %b", c.A)
	}
	if i, _ := c.Fg.Index(); i != 196 {
		t.Fatalf("fg = %v", c.Fg)
	}
	if rgb, _ := c.Bg.RGB(); rgb != (RGB{10, 20, 30}) {
		t.Fatalf("bg = %v", c.Bg)
	}
	if rgb, _ := l.UnderlineColor(0).RGB(); rgb != (RGB{1, 2, 3}) {
		t.Fatalf("underline color = %v", l.UnderlineColor(0))
	}
	if d := l.Cells[1]; d.A != 0 || !d.Fg.IsDefault() || !d.Bg.IsDefault() {
		t.Fatalf("after reset: %+v", d)
	}
	term.WriteString("\x1b[38;2;1;2;3;1mZ")
	if c := term.ScreenLine(0).Cells[2]; c.A&AttrBold == 0 {
		t.Fatal("params after a semicolon truecolor must still apply")
	}
	term.WriteString("\x1b[92;101mW")
	if c := term.ScreenLine(0).Cells[3]; c.Fg != Indexed(10) || c.Bg != Indexed(9) {
		t.Fatalf("bright colors: %v %v", c.Fg, c.Bg)
	}
}

func TestAltScreen(t *testing.T) {
	term := newTerm(10, 3)
	term.WriteString("shell$ \x1b[?1049h")
	if !term.AltScreen() {
		t.Fatal("expected alt screen")
	}
	expectScreen(t, term, "", "", "")
	term.WriteString("\x1b[Hvim stuff")
	term.WriteString("\x1b[?1049l")
	expectScreen(t, term, "shell$")
	expectCursor(t, term, 7, 0)
}

func TestSaveRestoreCursor(t *testing.T) {
	term := newTerm(10, 5)
	term.WriteString("\x1b[3;4H\x1b[31m\x1b7\x1b[H\x1b[0m\x1b8X")
	expectCursor(t, term, 4, 2)
	if c := term.ScreenLine(2).Cells[3]; c.Fg != Indexed(1) {
		t.Fatal("DECRC should restore the pen")
	}
}

func TestDeviceReports(t *testing.T) {
	term := newTerm(80, 24)
	cases := []struct{ in, want string }{
		{"\x1b[c", "\x1b[?65;1;4;6;22;28;52c"},
		{"\x1b[>c", "\x1b[>1;100;0c"},
		{"\x1b[>q", "\x1bP>|OpalTerminal 0.1.0\x1b\\"},
		{"\x1b[5n", "\x1b[0n"},
		{"\x1b[3;7H\x1b[6n", "\x1b[3;7R"},
		{"\x1b[?2004$p", "\x1b[?2004;2$y"},
		{"\x1b[?2004h\x1b[?2004$p", "\x1b[?2004;1$y"},
		{"\x1b[4$p", "\x1b[4;2$y"},
		{"\x1b[?9999$p", "\x1b[?9999;0$y"},
		{"\x1b[18t", "\x1b[8;24;80t"},
		{"\x1b[31;1m\x1bP$qm\x1b\\", "\x1bP1$r0;1;31m\x1b\\"},
		{"\x1bP$qr\x1b\\", "\x1bP1$r1;24r\x1b\\"},
		{"\x1b]10;?\x07", "\x1b]10;rgb:e6e6/e6e6/e6e6\x1b\\"},
		{"\x1b]4;1;?\x1b\\", "\x1b]4;1;rgb:cccc/6666/6666\x1b\\"},
		{"\x1bP+q524742\x1b\\", "\x1bP1+r524742=382f382f38\x1b\\"},
		{"\x1bP+q78797a\x1b\\", "\x1bP0+r78797a\x1b\\"},
		{"\x1b[?996n", "\x1b[?997;1n"},
		{"\x1b[21t", "\x1b]l\x1b\\"},
	}
	for _, c := range cases {
		term.WriteString(c.in)
		if got := string(term.TakeReplies()); got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKittyKeyboardStack(t *testing.T) {
	term := newTerm(10, 2)
	term.WriteString("\x1b[>1u\x1b[>3u\x1b[?u")
	if got := string(term.TakeReplies()); got != "\x1b[?3u" {
		t.Fatalf("query = %q", got)
	}
	term.WriteString("\x1b[<u\x1b[?u")
	if got := string(term.TakeReplies()); got != "\x1b[?1u" {
		t.Fatalf("after pop = %q", got)
	}
	term.WriteString("\x1b[=4;2u")
	if term.KittyKeyboardFlags() != 5 {
		t.Fatalf("flags = %d, want 5", term.KittyKeyboardFlags())
	}
	// The alt screen has its own stack.
	term.WriteString("\x1b[?1049h")
	if term.KittyKeyboardFlags() != 0 {
		t.Fatal("alt screen should start with no flags")
	}
}

func TestTitleAndCWD(t *testing.T) {
	term := newTerm(10, 2)
	term.WriteString("\x1b]2;hello\x07\x1b]7;file://box/home/me/src\x1b\\")
	evs := term.TakeEvents()
	if term.Title() != "hello" || term.CWD() != "/home/me/src" {
		t.Fatalf("title %q cwd %q", term.Title(), term.CWD())
	}
	var sawCWD bool
	for _, e := range evs {
		if c, ok := e.(EvCWD); ok && c.Host == "box" {
			sawCWD = true
		}
	}
	if !sawCWD {
		t.Fatalf("events: %#v", evs)
	}
	term.WriteString("\x1b]7;file:///C:/Users/me\x07")
	if term.CWD() != "C:/Users/me" {
		t.Fatalf("windows cwd = %q", term.CWD())
	}
	term.WriteString("\x1b[22t\x1b]2;inner\x07\x1b[23t")
	if term.Title() != "hello" {
		t.Fatalf("title stack pop = %q", term.Title())
	}
}

func TestHyperlinks(t *testing.T) {
	term := newTerm(20, 1)
	term.WriteString("\x1b]8;id=1;https://example.com\x1b\\link\x1b]8;;\x1b\\ plain")
	l := term.ScreenLine(0)
	if lk := l.Link(0); lk == nil || lk.URI != "https://example.com" || lk.ID != "1" {
		t.Fatalf("link = %+v", lk)
	}
	if l.Link(3) != l.Link(0) {
		t.Fatal("cells of one link should share it")
	}
	if l.Link(5) != nil {
		t.Fatal("link should have ended")
	}
}

func TestClipboardOSC52(t *testing.T) {
	term := newTerm(10, 1)
	term.WriteString("\x1b]52;c;aGVsbG8=\x07\x1b]52;c;?\x07")
	evs := term.TakeEvents()
	if w, ok := evs[0].(EvClipboardWrite); !ok || string(w.Data) != "hello" {
		t.Fatalf("write event %#v", evs[0])
	}
	if _, ok := evs[1].(EvClipboardRead); !ok {
		t.Fatalf("read event %#v", evs[1])
	}
}

func TestShellIntegrationMarks(t *testing.T) {
	now := time.Unix(1000, 0)
	nowFunc = func() time.Time { return now }
	defer func() { nowFunc = time.Now }()

	term := newTerm(20, 5)
	term.WriteString("\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07")
	now = now.Add(1500 * time.Millisecond)
	term.WriteString("file1\r\nfile2\r\n\x1b]133;D;2\x07\x1b]133;A\x07$ ")
	m := term.ScreenLine(0).Prompt
	if m == nil {
		t.Fatal("no prompt mark on line 0")
	}
	if m.Exit != 2 {
		t.Fatalf("exit = %d", m.Exit)
	}
	if d, ok := m.Duration(); !ok || d != 1500*time.Millisecond {
		t.Fatalf("duration = %v", d)
	}
	if term.ScreenLine(3).Prompt == nil {
		t.Fatal("second prompt not marked")
	}
	l := term.ScreenLine(0)
	if l.Cells[0].A.Semantic() != SemanticPrompt || l.Cells[2].A.Semantic() != SemanticInput {
		t.Fatal("prompt/input cells not tagged")
	}
	if term.ScreenLine(1).Cells[0].A.Semantic() != SemanticOutput {
		t.Fatal("output not tagged")
	}
	var finished bool
	for _, e := range term.TakeEvents() {
		if f, ok := e.(EvCommandFinished); ok && f.Mark == m {
			finished = true
		}
	}
	if !finished {
		t.Fatal("no EvCommandFinished")
	}
}

func TestNotificationsAndProgress(t *testing.T) {
	term := newTerm(10, 1)
	term.WriteString("\x1b]9;build done\x07\x1b]9;4;1;42\x07\x1b]777;notify;Title;Body\x07\x1b]99;;hi\x1b\\")
	evs := term.TakeEvents()
	if n, ok := evs[0].(EvNotify); !ok || n.Body != "build done" {
		t.Fatalf("osc 9 → %#v", evs[0])
	}
	if p, ok := evs[1].(EvProgress); !ok || p.State != ProgressNormal || p.Percent != 42 {
		t.Fatalf("progress → %#v", evs[1])
	}
	if n, ok := evs[2].(EvNotify); !ok || n.Title != "Title" || n.Body != "Body" {
		t.Fatalf("osc 777 → %#v", evs[2])
	}
	if n, ok := evs[3].(EvNotify); !ok || n.Title != "hi" {
		t.Fatalf("osc 99 → %#v", evs[3])
	}
}

func TestDECSpecialGraphics(t *testing.T) {
	term := newTerm(10, 1)
	term.WriteString("\x1b(0lqk\x1b(Bq")
	expectScreen(t, term, "┌─┐q")
	term.WriteString("\x1b)0\x0eq\x0fq")
	expectScreen(t, term, "┌─┐q─q")
}

func TestDECALNAndRects(t *testing.T) {
	term := newTerm(5, 3)
	term.WriteString("\x1b#8")
	expectScreen(t, term, "EEEEE", "EEEEE", "EEEEE")
	term.WriteString("\x1b[2;2;3;4$z")
	expectScreen(t, term, "EEEEE", "E   E", "E   E")
	term.WriteString("\x1b[88;1;1;1;5$x")
	expectScreen(t, term, "XXXXX", "E   E", "E   E")
	term.WriteString("\x1b[1;1;1;2;1;3;4;1$v")
	expectScreen(t, term, "XXXXX", "E   E", "E  XX")
}

func TestREP(t *testing.T) {
	term := newTerm(10, 1)
	term.WriteString("a\x1b[3b")
	expectScreen(t, term, "aaaa")
}

func TestReflowShrinkAndGrow(t *testing.T) {
	term := newTerm(10, 4)
	term.WriteString("0123456789abcdef\r\n$ ")
	expectScreen(t, term, "0123456789", "abcdef", "$")
	term.Resize(6, 4)
	expectScreen(t, term, "012345", "6789ab", "cdef", "$")
	expectCursor(t, term, 2, 3)
	term.Resize(20, 4)
	expectScreen(t, term, "0123456789abcdef", "$")
	expectCursor(t, term, 2, 1)
}

func TestReflowPushesIntoHistory(t *testing.T) {
	term := newTerm(10, 3)
	term.WriteString("aaaaaaaaaa\r\nbbbbbbbbbb\r\n$ ")
	term.Resize(5, 3)
	// 2 lines become 4 plus the prompt: two lines go to history.
	if h := history(term); strings.Join(h, ",") != "aaaaa,aaaaa" {
		t.Fatalf("history = %q", h)
	}
	expectScreen(t, term, "bbbbb", "bbbbb", "$")
	term.Resize(10, 3)
	expectScreen(t, term, "aaaaaaaaaa", "bbbbbbbbbb", "$")
	if term.HistoryLen() != 0 {
		t.Fatalf("history should be pulled back, have %d", term.HistoryLen())
	}
}

func TestReflowWideChars(t *testing.T) {
	term := newTerm(5, 3)
	term.WriteString("abcd中文x")
	expectScreen(t, term, "abcd", "中文x")
	term.Resize(10, 3)
	expectScreen(t, term, "abcd中文x")
	term.Resize(4, 3)
	expectScreen(t, term, "abcd", "中文", "x")
}

func TestShrinkRowsKeepsCursorLine(t *testing.T) {
	term := newTerm(10, 5)
	term.WriteString("1\r\n2\r\n3\r\n4\r\n5")
	term.Resize(10, 2)
	expectScreen(t, term, "4", "5")
	expectCursor(t, term, 1, 1)
	term.Resize(10, 5)
	expectScreen(t, term, "1", "2", "3", "4", "5")
}

func TestAltScreenCropsOnResize(t *testing.T) {
	term := newTerm(10, 3)
	term.WriteString("\x1b[?1049h0123456789")
	term.Resize(5, 3)
	expectScreen(t, term, "01234")
	term.WriteString("\x1b[?1049l")
	if term.AltScreen() {
		t.Fatal("still on alt screen")
	}
}

// Chunk boundaries must never matter: feeding a stream one byte at a time
// or in random pieces has to give the same screen as one big write.
func TestChunkingInvariance(t *testing.T) {
	stream := "plain \x1b[1;31mred\x1b[0m 中文 é\u0301 👨\u200d👩\u200d👧 " +
		"\x1b]2;title\x07\x1b]8;;http://x\x1b\\L\x1b]8;;\x1b\\ \x1b[2;5H\x1b[K" +
		"\x1b[38:2::1:2:3mrgb\x1b[m\x1bP$qm\x1b\\\r\n\x1b(0lqk\x1b(B\x1b[?1049h\x1b[?1049l end"
	ref := newTerm(20, 6)
	ref.WriteString(stream)
	want := strings.Join(screen(ref), "\n")
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 200; trial++ {
		term := newTerm(20, 6)
		b := []byte(stream)
		for len(b) > 0 {
			n := 1 + rng.Intn(min(len(b), 7))
			if trial == 0 {
				n = 1
			}
			term.Write(b[:n])
			b = b[n:]
		}
		if got := strings.Join(screen(term), "\n"); got != want {
			t.Fatalf("trial %d:\n got %q\nwant %q", trial, got, want)
		}
	}
}

func TestParserIgnoresGarbage(t *testing.T) {
	term := newTerm(20, 2)
	term.WriteString("\x1b[999999999999999999999999H\x1b[1;2;3;4;5;6;7;8;9;10;11;12;13;14;15;16;17;18;19;20;21;22;23;24;25;26;27;28;29;30;31;32;33;34;35m")
	term.WriteString("\x1b[?1;2$$$$p\xff\xfeok\x18\x1a")
	if !strings.Contains(term.ScreenLine(1).String(), "ok") {
		t.Fatalf("screen: %q", screen(term))
	}
}

func TestInvalidUTF8(t *testing.T) {
	term := newTerm(10, 1)
	term.Write([]byte{'a', 0xe4, 0xb8, 'b'})
	if got := term.ScreenLine(0).String(); got != "a\uFFFDb" {
		t.Fatalf("got %q", got)
	}
}

func TestReverseWrap(t *testing.T) {
	term := newTerm(5, 2)
	term.WriteString("\x1b[?45h\x1b[2;1H\bX")
	expectScreen(t, term, "    X")
}

func TestLeftRightMargins(t *testing.T) {
	term := newTerm(10, 3)
	term.WriteString("\x1b[?69h\x1b[3;6s")
	term.WriteString("\x1b[1;3Habcdefg")
	expectScreen(t, term, "  abcd", "  efg")
}

func TestSynchronizedOutput(t *testing.T) {
	term := newTerm(10, 1)
	term.WriteString("\x1b[?2026h")
	if on, _ := term.Synchronized(); !on {
		t.Fatal("sync should be on")
	}
	term.WriteString("\x1b[?2026l")
	if on, _ := term.Synchronized(); on {
		t.Fatal("sync should be off")
	}
}

func TestFullReset(t *testing.T) {
	term := newTerm(10, 2)
	term.WriteString("junk\x1b[31m\x1b[?1049h\x1bc")
	if term.AltScreen() {
		t.Fatal("RIS should leave the alt screen")
	}
	term.WriteString("x")
	expectScreen(t, term, "x")
	if !term.ScreenLine(0).Cells[0].Fg.IsDefault() {
		t.Fatal("RIS should reset the pen")
	}
}

func BenchmarkASCIIThroughput(b *testing.B) {
	term := New(Options{Cols: 200, Rows: 50, Scrollback: 10000})
	line := strings.Repeat("the quick brown fox jumps over the lazy dog ", 4) + "\r\n"
	data := []byte(strings.Repeat(line, 1000))
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		term.Write(data)
	}
}

func BenchmarkColoredThroughput(b *testing.B) {
	term := New(Options{Cols: 200, Rows: 50, Scrollback: 10000})
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&sb, "\x1b[38;5;%dmcolor %d\x1b[0m \x1b[1;32mok\x1b[0m 中文字符 %d\r\n", i%256, i, i)
	}
	data := []byte(sb.String())
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		term.Write(data)
	}
}
