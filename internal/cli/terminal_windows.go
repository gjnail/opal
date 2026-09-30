//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// startDetached keeps the terminal from holding our console (a build made
// without -H windowsgui would otherwise share it) or dying with its process
// group. Unlike platform.Detach it leaves the window shown.
func startDetached(cmd *exec.Cmd) {
	const detachedProcess, newProcessGroup = 0x00000008, 0x00000200
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | newProcessGroup}
}
