//go:build !windows

package tools

import "errors"

func winCopy(string) error      { return errors.New("win32 clipboard is Windows-only") }
func winPaste() (string, error) { return "", errors.New("win32 clipboard is Windows-only") }
