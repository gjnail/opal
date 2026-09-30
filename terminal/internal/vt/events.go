package vt

// Event is something the UI should react to. Terminals queue events while
// parsing and the UI drains them with TakeEvents, so handlers never run
// under the terminal's lock.
type Event interface{ isEvent() }

// EvBell is BEL.
type EvBell struct{}

// EvTitle is a new window title (OSC 0/2).
type EvTitle struct{ Title string }

// EvCWD reports the shell's working directory (OSC 7, OSC 633;P;Cwd,
// OSC 1337;CurrentDir, ConEmu OSC 9;9).
type EvCWD struct{ Path, Host string }

// EvClipboardWrite asks to set a clipboard (OSC 52). Selection is 'c' for
// the clipboard or 'p' for the primary selection.
type EvClipboardWrite struct {
	Selection byte
	Data      []byte
}

// EvClipboardRead asks for clipboard contents (OSC 52 with "?"). Answer
// with Terminal.ClipboardReply if policy allows.
type EvClipboardRead struct{ Selection byte }

// EvNotify is a desktop notification request (OSC 9, OSC 777, OSC 99).
type EvNotify struct{ Title, Body string }

// EvProgress is a taskbar/tab progress report (ConEmu OSC 9;4).
type EvProgress struct {
	State   ProgressState
	Percent int
}

// ProgressState mirrors ConEmu/Windows Terminal progress states.
type ProgressState int

const (
	ProgressNone ProgressState = iota
	ProgressNormal
	ProgressError
	ProgressIndeterminate
	ProgressPaused
)

// EvUserVar is an iTerm2 user variable (OSC 1337;SetUserVar).
type EvUserVar struct{ Name, Value string }

// EvCommandFinished fires when shell integration reports that a command
// ended (OSC 133;D).
type EvCommandFinished struct{ Mark *PromptMark }

// EvPointerShape asks for a mouse pointer shape (OSC 22).
type EvPointerShape struct{ Name string }

// EvAttention asks the UI to get the user's attention.
type EvAttention struct{}

// EvColorsChanged means the program changed palette colors.
type EvColorsChanged struct{}

func (EvBell) isEvent()            {}
func (EvTitle) isEvent()           {}
func (EvCWD) isEvent()             {}
func (EvClipboardWrite) isEvent()  {}
func (EvClipboardRead) isEvent()   {}
func (EvNotify) isEvent()          {}
func (EvProgress) isEvent()        {}
func (EvUserVar) isEvent()         {}
func (EvCommandFinished) isEvent() {}
func (EvPointerShape) isEvent()    {}
func (EvAttention) isEvent()       {}
func (EvColorsChanged) isEvent()   {}
