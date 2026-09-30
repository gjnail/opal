// Package notify shows desktop notifications: toasts on Windows,
// Notification Center on macOS, and the freedesktop notification service
// on Linux.
package notify

import (
	"sync"
	"time"
	"unicode"
)

// AppName is how notifications identify the sender.
const AppName = "Opal Terminal"

var (
	mu   sync.Mutex
	last time.Time
)

// minInterval keeps a program that prints notifications in a loop from
// flooding the desktop.
const minInterval = time.Second

// Send shows a notification in the background. It reports false when the
// notification was dropped because another one was just shown.
func Send(title, body string) bool {
	mu.Lock()
	if time.Since(last) < minInterval {
		mu.Unlock()
		return false
	}
	last = time.Now()
	mu.Unlock()
	title, body = clean(title, 120), clean(body, 400)
	if title == "" {
		title = AppName
	}
	go send(title, body)
	return true
}

// clean drops control characters and caps the length.
func clean(s string, n int) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' {
			continue
		}
		out = append(out, r)
		if len(out) >= n {
			out = append(out, '…')
			break
		}
	}
	return string(out)
}
