package vt

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const maxClipboard = 16 << 20

func (t *Terminal) oscDispatch(data []byte) {
	t.lastValid = false
	cmd, rest, _ := strings.Cut(string(data), ";")
	switch cmd {
	case "0":
		t.iconTitle = sanitizeTitle(rest)
		t.setTitle(rest)
	case "1":
		t.iconTitle = sanitizeTitle(rest)
	case "2":
		t.setTitle(rest)
	case "4":
		t.oscPalette(rest)
	case "104":
		t.oscResetPalette(rest)
	case "5", "105":
		// Special colors (bold, underline, ...): accepted, ignored.
	case "10", "11", "12", "17", "19":
		n, _ := strconv.Atoi(cmd)
		t.oscDynamicColors(n, rest)
	case "110", "111", "112", "117", "119":
		n, _ := strconv.Atoi(cmd)
		t.resetDynamicColor(n - 100)
	case "7":
		t.oscCWD(rest)
	case "8":
		t.oscHyperlink(rest)
	case "9":
		t.osc9(rest)
	case "22":
		t.emit(EvPointerShape{Name: rest})
	case "52":
		t.osc52(rest)
	case "99":
		t.osc99(rest)
	case "133":
		t.osc133(rest)
	case "633":
		t.osc633(rest)
	case "777":
		parts := strings.SplitN(rest, ";", 3)
		if len(parts) >= 2 && parts[0] == "notify" {
			body := ""
			if len(parts) == 3 {
				body = parts[2]
			}
			t.emit(EvNotify{Title: sanitizeTitle(parts[1]), Body: sanitizeText(body)})
		}
	case "1337":
		t.osc1337(rest)
	}
}

func sanitizeTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if len(s) > 1024 {
		s = s[:1024]
	}
	return s
}

func sanitizeText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if len(s) > 4096 {
		s = s[:4096]
	}
	return s
}

func (t *Terminal) setTitle(s string) {
	s = sanitizeTitle(s)
	if s == t.title {
		return
	}
	t.title = s
	t.emit(EvTitle{s})
}

// oscPalette handles OSC 4 ; index ; spec [; index ; spec ...].
func (t *Terminal) oscPalette(rest string) {
	parts := strings.Split(rest, ";")
	for i := 0; i+1 < len(parts); i += 2 {
		idx, err := strconv.Atoi(parts[i])
		if err != nil || idx < 0 || idx > 255 {
			continue
		}
		if parts[i+1] == "?" {
			t.reply(fmt.Sprintf("\x1b]4;%d;%s\x1b\\", idx, t.pal.ANSI[idx].xtermSpec()))
			continue
		}
		if c, ok := ParseColorSpec(parts[i+1]); ok {
			t.pal.ANSI[idx] = c
			t.seq++
			t.emit(EvColorsChanged{})
		}
	}
}

func (t *Terminal) oscResetPalette(rest string) {
	if rest == "" {
		t.pal.ANSI = t.basePal.ANSI
	} else {
		for _, s := range strings.Split(rest, ";") {
			if idx, err := strconv.Atoi(s); err == nil && idx >= 0 && idx <= 255 {
				t.pal.ANSI[idx] = t.basePal.ANSI[idx]
			}
		}
	}
	t.seq++
	t.emit(EvColorsChanged{})
}

// dynamicColor returns a pointer to the palette slot for OSC 10-19.
func (t *Terminal) dynamicColor(p *Palette, n int) *RGB {
	switch n {
	case 10:
		return &p.Foreground
	case 11:
		return &p.Background
	case 12:
		return &p.Cursor
	case 17:
		return &p.SelectionBG
	case 19:
		return &p.SelectionFG
	}
	return nil
}

// oscDynamicColors handles OSC 10-19. Several specs in one sequence set
// consecutive slots (OSC 10;fg;bg sets 10 and 11).
func (t *Terminal) oscDynamicColors(n int, rest string) {
	for _, spec := range strings.Split(rest, ";") {
		slot := t.dynamicColor(&t.pal, n)
		if slot != nil {
			if spec == "?" {
				t.reply(fmt.Sprintf("\x1b]%d;%s\x1b\\", n, slot.xtermSpec()))
			} else if c, ok := ParseColorSpec(spec); ok {
				*slot = c
				if n == 19 {
					t.pal.HasSelectionFG = true
				}
				t.seq++
				t.emit(EvColorsChanged{})
			}
		}
		n++
		if n == 13 {
			n = 17 // after cursor come the highlight colors
		}
	}
}

func (t *Terminal) resetDynamicColor(n int) {
	if slot := t.dynamicColor(&t.pal, n); slot != nil {
		*slot = *t.dynamicColor(&t.basePal, n)
		if n == 19 {
			t.pal.HasSelectionFG = t.basePal.HasSelectionFG
		}
		t.seq++
		t.emit(EvColorsChanged{})
	}
}

// oscCWD handles OSC 7 ; file://host/path.
func (t *Terminal) oscCWD(rest string) {
	u, err := url.Parse(rest)
	if err != nil || u.Path == "" {
		return
	}
	t.setCWD(u.Path, u.Host)
}

func (t *Terminal) setCWD(path, host string) {
	// file:///C:/Users/me arrives with a leading slash on Windows.
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	t.cwd = path
	t.emit(EvCWD{Path: path, Host: host})
}

// oscHyperlink handles OSC 8 ; params ; uri.
func (t *Terminal) oscHyperlink(rest string) {
	params, uri, ok := strings.Cut(rest, ";")
	if !ok || uri == "" {
		t.link = nil
		return
	}
	if len(uri) > 8192 {
		return
	}
	id := ""
	for _, kv := range strings.Split(params, ":") {
		if v, ok := strings.CutPrefix(kv, "id="); ok {
			id = v
		}
	}
	if t.link != nil && t.link.ID == id && t.link.URI == uri {
		return
	}
	t.link = &Hyperlink{ID: id, URI: uri}
}

// osc9 is iTerm2's notification, or one of ConEmu's commands.
func (t *Terminal) osc9(rest string) {
	sub, arg, _ := strings.Cut(rest, ";")
	if n, err := strconv.Atoi(sub); err == nil && n >= 1 && n <= 12 {
		switch n {
		case 4:
			st, pct, _ := strings.Cut(arg, ";")
			state, _ := strconv.Atoi(st)
			p, _ := strconv.Atoi(pct)
			t.emit(EvProgress{State: ProgressState(clamp(state, 0, 4)), Percent: clamp(p, 0, 100)})
		case 9:
			t.setCWD(strings.Trim(arg, `"`), "")
		case 12:
			t.semanticPromptStart()
		}
		return
	}
	t.emit(EvNotify{Body: sanitizeText(rest)})
}

// osc52 handles clipboard access.
func (t *Terminal) osc52(rest string) {
	sel, data, ok := strings.Cut(rest, ";")
	if !ok {
		return
	}
	target := byte('c')
	if strings.ContainsRune(sel, 'p') && !strings.ContainsRune(sel, 'c') {
		target = 'p'
	}
	if data == "?" {
		t.emit(EvClipboardRead{Selection: target})
		return
	}
	if len(data) > maxClipboard*4/3+4 {
		return
	}
	dec, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		dec, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(data, "="))
		if err != nil {
			return
		}
	}
	t.emit(EvClipboardWrite{Selection: target, Data: dec})
}

// ClipboardReply answers an EvClipboardRead.
func (t *Terminal) ClipboardReply(sel byte, text string) []byte {
	return []byte(fmt.Sprintf("\x1b]52;%c;%s\x1b\\", sel, base64.StdEncoding.EncodeToString([]byte(text))))
}

// osc99 is kitty's desktop notification protocol (the simple subset:
// title and body, optionally base64, possibly split over chunks).
func (t *Terminal) osc99(rest string) {
	meta, payload, _ := strings.Cut(rest, ";")
	id, done, part, b64 := "", true, "title", false
	for _, kv := range strings.Split(meta, ":") {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "i":
			id = v
		case "d":
			done = v != "0"
		case "p":
			part = v
		case "e":
			b64 = v == "1"
		}
	}
	if b64 {
		if dec, err := base64.StdEncoding.DecodeString(payload); err == nil {
			payload = string(dec)
		}
	}
	if t.kittyNotes == nil {
		t.kittyNotes = map[string]*EvNotify{}
	}
	n := t.kittyNotes[id]
	if n == nil {
		n = &EvNotify{}
		if len(t.kittyNotes) > 32 {
			clear(t.kittyNotes)
		}
		t.kittyNotes[id] = n
	}
	switch part {
	case "title":
		n.Title += payload
	case "body":
		n.Body += payload
	}
	if done {
		delete(t.kittyNotes, id)
		t.emit(EvNotify{Title: sanitizeTitle(n.Title), Body: sanitizeText(n.Body)})
	}
}

// Shell integration: OSC 133 (FinalTerm) and OSC 633 (VS Code).

func (t *Terminal) semanticPromptStart() {
	if m := t.lastPrompt; m != nil && m.Finished.IsZero() {
		if !m.Executed.IsZero() {
			// A new prompt without D: the command ended, status unknown.
			m.Finished = nowFunc()
		} else {
			m.Aborted = true
		}
	}
	m := &PromptMark{Started: nowFunc(), Exit: -1}
	// The mark goes on the line where the prompt's first character lands,
	// not where the cursor is now. Windows' ConPTY re-renders output and
	// can pass OSC 133 through before the cursor move that preceded it.
	t.pendingMark = m
	t.lastPrompt = m
	t.semantic = SemanticPrompt
	t.seq++
}

// placePendingMark attaches a waiting prompt mark to the cursor's line.
func (t *Terminal) placePendingMark() {
	l := t.line(t.cur.y)
	l.Prompt = t.pendingMark
	t.pendingMark = nil
}

func (t *Terminal) osc133(rest string) {
	kind, args, _ := strings.Cut(rest, ";")
	switch kind {
	case "A":
		t.semanticPromptStart()
	case "P":
		t.semantic = SemanticPrompt
	case "B":
		t.semantic = SemanticInput
	case "C":
		t.semantic = SemanticOutput
		if t.lastPrompt != nil {
			t.lastPrompt.Executed = nowFunc()
		}
	case "D":
		t.commandFinished(args)
	}
}

func (t *Terminal) commandFinished(args string) {
	t.semantic = SemanticOutput
	m := t.lastPrompt
	if m == nil || !m.Finished.IsZero() {
		return
	}
	code, _, _ := strings.Cut(args, ";")
	exit, err := strconv.Atoi(code)
	if err != nil {
		exit = 0
		if code != "" {
			exit = -1
		}
	}
	if m.Executed.IsZero() {
		m.Aborted = true
		return
	}
	m.Finished = nowFunc()
	m.Exit = exit
	t.seq++
	t.emit(EvCommandFinished{Mark: m})
}

func (t *Terminal) osc633(rest string) {
	kind, args, _ := strings.Cut(rest, ";")
	switch kind {
	case "A", "B", "C":
		t.osc133(kind)
	case "D":
		t.commandFinished(args)
	case "E":
		cmd, _, _ := strings.Cut(args, ";")
		if t.lastPrompt != nil {
			t.lastPrompt.Command = unescape633(cmd)
		}
	case "P":
		k, v, _ := strings.Cut(args, "=")
		if k == "Cwd" {
			t.setCWD(unescape633(v), "")
		}
	}
}

// unescape633 undoes VS Code's escaping: \\ and \xAB.
func unescape633(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == '\\' {
				b.WriteByte('\\')
				i++
				continue
			}
			if s[i+1] == 'x' && i+3 < len(s) {
				if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 3
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// osc1337 handles iTerm2's proprietary sequences.
func (t *Terminal) osc1337(rest string) {
	key, val, _ := strings.Cut(rest, "=")
	switch key {
	case "SetUserVar":
		name, b64, ok := strings.Cut(val, "=")
		if !ok {
			return
		}
		v, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return
		}
		t.emit(EvUserVar{Name: name, Value: string(v)})
	case "CurrentDir":
		t.setCWD(val, "")
	case "ClearScrollback":
		t.clearHistory()
	case "RequestAttention":
		if val != "no" {
			t.emit(EvAttention{})
		}
	case "CursorShape":
		switch val {
		case "0":
			t.cursorStyle = CursorSteadyBlock
		case "1":
			t.cursorStyle = CursorSteadyBar
		case "2":
			t.cursorStyle = CursorSteadyUnderline
		}
		t.seq++
	case "ReportCellSize":
		t.reply(fmt.Sprintf("\x1b]1337;ReportCellSize=%d;%d;1\x1b\\", t.cellH, t.cellW))
	case "Copy":
		_, b64, _ := strings.Cut(val, ":")
		if d, err := base64.StdEncoding.DecodeString(b64); err == nil && len(d) <= maxClipboard {
			t.emit(EvClipboardWrite{Selection: 'c', Data: d})
		}
	case "File":
		t.itermImage(val)
	case "MultipartFile":
		t.itermMultipartStart(val)
	case "FilePart":
		t.itermMultipartPart(val)
	case "FileEnd":
		t.itermMultipartEnd()
	}
}
