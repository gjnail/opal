//go:build !windows && !darwin

package notify

import (
	"os/exec"

	"github.com/godbus/dbus/v5"
)

// SetProcessAppID is only meaningful on Windows.
func SetProcessAppID() {}

// send talks to the freedesktop notification service over D-Bus, and
// falls back to notify-send.
func send(title, body string) {
	if conn, err := dbus.SessionBus(); err == nil {
		obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
		call := obj.Call("org.freedesktop.Notifications.Notify", 0,
			AppName, uint32(0), "utilities-terminal", title, body,
			[]string{}, map[string]dbus.Variant{}, int32(-1))
		if call.Err == nil {
			return
		}
	}
	if p, err := exec.LookPath("notify-send"); err == nil {
		exec.Command(p, "--app-name", AppName, title, body).Run()
	}
}
