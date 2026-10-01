//go:build !darwin

package ui

// defaultLocale is only needed on macOS; elsewhere the desktop session or
// the system sets the locale.
func defaultLocale() string { return "" }
