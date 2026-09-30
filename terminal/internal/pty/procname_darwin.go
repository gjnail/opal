package pty

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func processName(pid int) string {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return ""
	}
	name := kp.Proc.P_comm[:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return string(name)
}

// ProcessCWD returns a process's working directory. macOS only exposes
// this through libproc, which needs cgo; shell integration (OSC 7) covers
// the common case instead.
func ProcessCWD(pid int) string { return "" }
