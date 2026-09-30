//go:build !(linux || darwin || freebsd || netbsd || dragonfly || windows)

package platform

// TermWidth is unknown here; callers fall back to a sensible default.
func TermWidth() int { return 0 }

func ttyID() string { return "" }
