//go:build !windows && !darwin

package ui

func (w *Window) nativeWindow(e any) {}

// requestAttention has no portable equivalent yet outside Windows and
// macOS; the tab's status dot still shows what happened.
func (w *Window) requestAttention() {}

// beep has no portable sound; the visual bell still shows.
func (w *Window) beep() {}
