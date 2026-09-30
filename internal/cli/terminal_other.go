//go:build !windows

package cli

import (
	"os/exec"

	"opal/internal/platform"
)

// startDetached puts the terminal in its own session, so closing the shell
// that ran opal terminal doesn't close it too.
func startDetached(cmd *exec.Cmd) {
	platform.Detach(cmd)
}
