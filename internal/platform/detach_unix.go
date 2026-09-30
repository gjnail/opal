//go:build !windows

package platform

import (
	"os/exec"
	"syscall"
)

// Detach makes cmd outlive us in its own session, so Ctrl+C or closing the
// terminal doesn't take it down.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
