//go:build !windows && !linux && !darwin

package pty

func processName(pid int) string { return "" }

// ProcessCWD is unknown on this platform.
func ProcessCWD(pid int) string { return "" }
