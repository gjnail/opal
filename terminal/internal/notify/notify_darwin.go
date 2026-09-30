package notify

import "os/exec"

// SetProcessAppID is only meaningful on Windows.
func SetProcessAppID() {}

// send uses AppleScript. The title and body are passed as arguments, so
// quotes in them can't change the script.
func send(title, body string) {
	exec.Command("osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		title, body).Run()
}
