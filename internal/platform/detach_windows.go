//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// Detach makes cmd outlive us without holding the console or the pipe the
// shell is reading our output from.
func Detach(cmd *exec.Cmd) {
	const detachedProcess, newProcessGroup = 0x00000008, 0x00000200
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | newProcessGroup, HideWindow: true}
}
