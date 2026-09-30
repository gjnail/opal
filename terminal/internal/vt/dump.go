package vt

import (
	"fmt"
	"strings"
)

// Dump renders up to maxLines of history and screen as escape sequences
// that reproduce them: colors and attributes, soft wraps, and the shell
// integration marks (so exit-status gutters come back). The line the
// cursor is on (usually a prompt waiting for input) is left out, because a
// restored session starts a new shell that draws its own prompt.
func (t *Terminal) Dump(maxLines int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	end := t.buf.count() - t.rows + t.cur.y
	// A prompt still waiting for input can span lines (Opal's is two):
	// drop all of it, not just the cursor's line.
	for i := end; i >= 0 && i >= end-8; i-- {
		if m := t.buf.line(i).Prompt; m != nil {
			if m.Executed.IsZero() {
				end = i
			}
			break
		}
	}
	start := max(0, end-maxLines)
	// Don't start in the middle of a wrapped line.
	for start > 0 && t.buf.line(start-1).Wrapped {
		start--
	}
	// Leading blank lines carry nothing.
	for start < end && t.buf.line(start).isBlank() {
		start++
	}
	var b strings.Builder
	var cur pen
	var open *PromptMark
	finish := func() {
		if open != nil && !open.Finished.IsZero() {
			fmt.Fprintf(&b, "\x1b]133;D;%d\x07", max(open.Exit, 0))
		}
		open = nil
	}
	for i := start; i < end; i++ {
		l := t.buf.line(i)
		if l.Prompt != nil {
			finish()
			b.WriteString("\x1b]133;A\x07")
			open = l.Prompt
		}
		n := l.contentEnd()
		for x := 0; x < n; x++ {
			c := l.Cells[x]
			if c.A&(attrSpacer|attrPadding) != 0 {
				continue
			}
			p := pen{fg: c.Fg, bg: c.Bg, attrs: c.A & penAttrs, ulColor: l.UnderlineColor(x)}
			if p != cur {
				b.WriteString("\x1b[" + p.sgrString() + "m")
				cur = p
			}
			b.WriteString(l.Text(x))
		}
		if l.Wrapped && i < end-1 {
			continue // the next line continues this one
		}
		if cur != (pen{}) {
			b.WriteString("\x1b[m")
			cur = pen{}
		}
		b.WriteString("\r\n")
		if l.Prompt != nil && !l.Prompt.Executed.IsZero() {
			b.WriteString("\x1b]133;C\x07")
		}
	}
	finish()
	return b.String()
}
