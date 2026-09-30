//go:build windows

package picker

import (
	"os"

	"golang.org/x/sys/windows"
)

// openTTY opens the console itself, bypassing whatever the shell did with
// stdin/stdout (it's capturing our stdout to get the choice).
func openTTY() (in, out *os.File, err error) {
	in, err = os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	out, err = os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, out, nil
}

// enableVT turns on escape-sequence processing for the console output.
func enableVT(out *os.File) func() {
	h := windows.Handle(out.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return func() {}
	}
	windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
	return func() { windows.SetConsoleMode(h, mode) }
}
