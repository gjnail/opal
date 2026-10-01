//go:build darwin && !cgo

package pty

// ProcessCWD needs libproc, and so cgo; without it only shell integration
// (OSC 7) tells us where a shell is.
func ProcessCWD(pid int) string { return "" }
