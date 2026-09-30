//go:build !windows

package picker

import "os"

// openTTY opens the controlling terminal, bypassing whatever the shell did
// with stdin/stdout (it's capturing our stdout to get the choice).
func openTTY() (in, out *os.File, err error) {
	in, err = os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	out, err = os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, out, nil
}

func enableVT(*os.File) func() { return func() {} }
