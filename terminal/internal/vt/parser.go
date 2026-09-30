package vt

import "unicode/utf8"

// The parser is Paul Williams' DEC-compatible state machine
// (vt100.net/emu/dec_ansi_parser), extended with UTF-8, colon sub-parameters
// and generous string limits for OSC/DCS/APC payloads.

type pstate uint8

const (
	stGround pstate = iota
	stEscape
	stEscapeInter
	stCSIEntry
	stCSIParam
	stCSIInter
	stCSIIgnore
	stDCSEntry
	stDCSParam
	stDCSInter
	stDCSPass
	stDCSIgnore
	stOSC
	stSOSPMAPC
)

const (
	maxParams     = 32
	maxParamValue = 65535
	maxOSC        = 64 << 20 // inline images travel in OSC 1337
	maxAPC        = 8 << 20
)

// params holds CSI/DCS parameters. A value of -1 means the parameter was
// omitted. sub[i] is true when param i followed a ':' rather than a ';'.
type params struct {
	v   [maxParams]int32
	sub [maxParams]bool
	n   int
}

// get returns param i, or def if it's missing or zero-defaulted.
func (p *params) get(i int, def int) int {
	if i >= p.n || p.v[i] < 0 {
		return def
	}
	return int(p.v[i])
}

// getNZ is get, but 0 also means def (most cursor movement works this way).
func (p *params) getNZ(i int, def int) int {
	v := p.get(i, def)
	if v == 0 {
		return def
	}
	return v
}

type handler interface {
	print(r rune)
	printASCII(b []byte)
	execute(b byte)
	escDispatch(inter []byte, final byte)
	csiDispatch(p *params, private byte, inter []byte, final byte)
	oscDispatch(data []byte)
	dcsHook(p *params, private byte, inter []byte, final byte)
	dcsPut(b []byte)
	dcsUnhook()
	apcDispatch(data []byte)
}

type parser struct {
	h     handler
	state pstate

	p      params
	cur    int32
	hasCur bool
	// pendingSep: a separator was seen, so another (possibly empty)
	// parameter follows.
	pendingSep bool
	private    byte
	inter      [2]byte
	ninter     int
	ignore     bool // too many params or intermediates: swallow the sequence

	str     []byte
	strKind byte // ']' OSC, '_' APC, '^' PM, 'X' SOS
	strEsc  bool // saw ESC inside a string; ST may follow
	strMax  int

	u8    [utf8.UTFMax]byte
	u8n   int
	u8len int
}

func newParser(h handler) *parser { return &parser{h: h} }

func (ps *parser) clear() {
	ps.p.n = 0
	ps.cur = 0
	ps.hasCur = false
	ps.pendingSep = false
	ps.private = 0
	ps.ninter = 0
	ps.ignore = false
}

func (ps *parser) collect(b byte) {
	if ps.ninter < len(ps.inter) {
		ps.inter[ps.ninter] = b
		ps.ninter++
	} else {
		ps.ignore = true
	}
}

func (ps *parser) paramDigit(b byte) {
	if !ps.hasCur {
		ps.cur, ps.hasCur = 0, true
	}
	if ps.cur < maxParamValue {
		ps.cur = ps.cur*10 + int32(b-'0')
		if ps.cur > maxParamValue {
			ps.cur = maxParamValue
		}
	}
}

// paramSep finishes the current parameter; colon says the next one is a
// sub-parameter.
func (ps *parser) paramSep(colon bool) {
	ps.finishParam()
	if ps.p.n < maxParams {
		ps.p.sub[ps.p.n] = colon
	}
	// A trailing separator means one more (empty) parameter follows.
	ps.hasCur = false
	ps.pendingSep = true
}

func (ps *parser) finishParam() {
	if ps.p.n >= maxParams {
		ps.ignore = true
		return
	}
	if ps.hasCur {
		ps.p.v[ps.p.n] = ps.cur
	} else {
		ps.p.v[ps.p.n] = -1
	}
	if !ps.pendingSep {
		ps.p.sub[ps.p.n] = false
	}
	ps.p.n++
	ps.hasCur = false
	ps.pendingSep = false
}

// endParams closes the parameter list before dispatch.
func (ps *parser) endParams() {
	if ps.hasCur || ps.pendingSep {
		ps.finishParam()
	}
}

func (ps *parser) interSlice() []byte { return ps.inter[:ps.ninter] }

func (ps *parser) startString(kind byte) {
	ps.str = ps.str[:0]
	ps.strKind = kind
	ps.strEsc = false
	switch kind {
	case ']':
		ps.strMax = maxOSC
		ps.state = stOSC
	case '_':
		ps.strMax = maxAPC
		ps.state = stSOSPMAPC
	default:
		ps.strMax = 0 // SOS and PM are swallowed
		ps.state = stSOSPMAPC
	}
}

func (ps *parser) appendString(b []byte) {
	if len(ps.str)+len(b) <= ps.strMax {
		ps.str = append(ps.str, b...)
	}
}

func (ps *parser) finishString() {
	switch ps.strKind {
	case ']':
		ps.h.oscDispatch(ps.str)
	case '_':
		ps.h.apcDispatch(ps.str)
	}
	if cap(ps.str) > 1<<20 {
		ps.str = nil // don't pin a big image buffer forever
	}
	ps.state = stGround
}

// advance feeds bytes through the state machine.
func (ps *parser) advance(data []byte) {
	for i := 0; i < len(data); {
		b := data[i]
		switch ps.state {
		case stGround:
			if ps.u8len > 0 {
				i += ps.continueUTF8(data[i:])
				continue
			}
			if b >= 0x20 && b < 0x7f {
				j := i + 1
				for j < len(data) && data[j] >= 0x20 && data[j] < 0x7f {
					j++
				}
				ps.h.printASCII(data[i:j])
				i = j
				continue
			}
			if b >= 0x80 {
				i += ps.startUTF8(data[i:])
				continue
			}
			ps.anywhere(b)
			i++

		case stOSC, stSOSPMAPC:
			i += ps.stringBytes(data[i:])

		case stDCSPass:
			i += ps.dcsBytes(data[i:])

		default:
			ps.step(b)
			i++
		}
	}
}

// anywhere handles C0 controls and DEL in the non-string states.
func (ps *parser) anywhere(b byte) {
	switch {
	case b == 0x1b:
		ps.clear()
		ps.state = stEscape
	case b == 0x18 || b == 0x1a:
		ps.state = stGround
		if b == 0x1a {
			ps.h.print('�') // SUB shows as a replacement glyph, like xterm's
		}
	case b < 0x20:
		ps.h.execute(b)
	}
	// 0x7f (DEL) is ignored everywhere.
}

func (ps *parser) step(b byte) {
	if b < 0x20 || b == 0x7f {
		ps.anywhere(b)
		return
	}
	if b >= 0x80 {
		// Stray UTF-8 inside an escape sequence: abandon the sequence.
		ps.state = stGround
		return
	}
	switch ps.state {
	case stEscape:
		switch {
		case b <= 0x2f:
			ps.collect(b)
			ps.state = stEscapeInter
		case b == '[':
			ps.clear()
			ps.state = stCSIEntry
		case b == ']':
			ps.startString(']')
		case b == 'P':
			ps.clear()
			ps.state = stDCSEntry
		case b == 'X' || b == '^' || b == '_':
			ps.startString(b)
		default:
			ps.state = stGround
			ps.h.escDispatch(nil, b)
		}

	case stEscapeInter:
		if b <= 0x2f {
			ps.collect(b)
			return
		}
		ps.state = stGround
		if !ps.ignore {
			ps.h.escDispatch(ps.interSlice(), b)
		}

	case stCSIEntry, stCSIParam:
		switch {
		case b >= '0' && b <= '9':
			ps.paramDigit(b)
			ps.state = stCSIParam
		case b == ';' || b == ':':
			ps.paramSep(b == ':')
			ps.state = stCSIParam
		case b >= 0x3c && b <= 0x3f:
			if ps.state == stCSIEntry {
				ps.private = b
				ps.state = stCSIParam
			} else {
				ps.state = stCSIIgnore
			}
		case b <= 0x2f:
			ps.endParams()
			ps.collect(b)
			ps.state = stCSIInter
		default:
			ps.endParams()
			ps.state = stGround
			if !ps.ignore {
				ps.h.csiDispatch(&ps.p, ps.private, ps.interSlice(), b)
			}
		}

	case stCSIInter:
		switch {
		case b <= 0x2f:
			ps.collect(b)
		case b <= 0x3f:
			ps.state = stCSIIgnore
		default:
			ps.state = stGround
			if !ps.ignore {
				ps.h.csiDispatch(&ps.p, ps.private, ps.interSlice(), b)
			}
		}

	case stCSIIgnore:
		if b >= 0x40 {
			ps.state = stGround
		}

	case stDCSEntry, stDCSParam:
		switch {
		case b >= '0' && b <= '9':
			ps.paramDigit(b)
			ps.state = stDCSParam
		case b == ';' || b == ':':
			ps.paramSep(b == ':')
			ps.state = stDCSParam
		case b >= 0x3c && b <= 0x3f:
			if ps.state == stDCSEntry {
				ps.private = b
				ps.state = stDCSParam
			} else {
				ps.state = stDCSIgnore
			}
		case b <= 0x2f:
			ps.endParams()
			ps.collect(b)
			ps.state = stDCSInter
		default:
			ps.endParams()
			ps.hookDCS(b)
		}

	case stDCSInter:
		switch {
		case b <= 0x2f:
			ps.collect(b)
		case b <= 0x3f:
			ps.state = stDCSIgnore
		default:
			ps.hookDCS(b)
		}

	case stDCSIgnore:
		// Swallowed until ST, which arrives as ESC \ and resets through
		// the escape state.
	}
}

func (ps *parser) hookDCS(final byte) {
	if ps.ignore {
		ps.state = stDCSIgnore
		return
	}
	ps.state = stDCSPass
	ps.strEsc = false
	ps.h.dcsHook(&ps.p, ps.private, ps.interSlice(), final)
}

// dcsBytes passes DCS payload through until ST. It returns bytes consumed.
func (ps *parser) dcsBytes(data []byte) int {
	for i, b := range data {
		if ps.strEsc {
			ps.strEsc = false
			ps.h.dcsUnhook()
			ps.state = stGround
			if b == '\\' {
				return i + 1
			}
			// Not ST: the ESC starts a new sequence.
			ps.clear()
			ps.state = stEscape
			return i
		}
		switch b {
		case 0x1b:
			if i > 0 {
				ps.h.dcsPut(data[:i])
			}
			ps.strEsc = true
			return i + 1
		case 0x18, 0x1a:
			if i > 0 {
				ps.h.dcsPut(data[:i])
			}
			ps.h.dcsUnhook()
			ps.state = stGround
			return i + 1
		}
	}
	ps.h.dcsPut(data)
	return len(data)
}

// stringBytes collects an OSC/APC/PM/SOS string until BEL or ST.
func (ps *parser) stringBytes(data []byte) int {
	for i, b := range data {
		if ps.strEsc {
			ps.strEsc = false
			ps.finishString()
			if b == '\\' {
				return i + 1
			}
			ps.clear()
			ps.state = stEscape
			return i
		}
		switch {
		case b == 0x1b:
			ps.appendString(data[:i])
			ps.strEsc = true
			return i + 1
		case b == 0x07 && ps.strKind == ']':
			ps.appendString(data[:i])
			ps.finishString()
			return i + 1
		case b == 0x18 || b == 0x1a:
			ps.state = stGround
			return i + 1
		case b < 0x20 && b != 0x07:
			// Other C0 bytes inside a string are dropped (xterm behavior);
			// flush what came before and skip this byte.
			ps.appendString(data[:i])
			return i + 1
		}
	}
	ps.appendString(data)
	return len(data)
}

func (ps *parser) startUTF8(data []byte) int {
	b := data[0]
	var n int
	switch {
	case b&0xe0 == 0xc0:
		n = 2
	case b&0xf0 == 0xe0:
		n = 3
	case b&0xf8 == 0xf0:
		n = 4
	default:
		ps.h.print(utf8.RuneError)
		return 1
	}
	// Fast path: the whole sequence is in this buffer.
	if len(data) >= n {
		r, size := utf8.DecodeRune(data[:n])
		if r == utf8.RuneError && size <= 1 {
			// One replacement for the maximal ill-formed subpart: the lead
			// byte plus the continuation bytes that follow it.
			k := 1
			for k < n && data[k]&0xc0 == 0x80 {
				k++
			}
			ps.h.print(utf8.RuneError)
			return k
		}
		ps.h.print(r)
		return size
	}
	ps.u8[0] = b
	ps.u8n = 1
	ps.u8len = n
	return 1
}

func (ps *parser) continueUTF8(data []byte) int {
	b := data[0]
	if b&0xc0 != 0x80 {
		// Truncated sequence: emit a replacement and reprocess this byte.
		ps.u8len = 0
		ps.h.print(utf8.RuneError)
		return 0
	}
	ps.u8[ps.u8n] = b
	ps.u8n++
	if ps.u8n == ps.u8len {
		r, _ := utf8.DecodeRune(ps.u8[:ps.u8n])
		ps.u8len = 0
		ps.h.print(r)
	}
	return 1
}
