//go:build !windows

package ui

func (w *Window) nativeWindow(e any) {}

// requestAttention has no portable equivalent yet outside Windows; the
// tab's status dot still shows what happened.
func (w *Window) requestAttention() {}
