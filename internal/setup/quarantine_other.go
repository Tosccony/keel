//go:build !darwin

package setup

// ClearQuarantine is for macOS; other systems have no such mark.
func ClearQuarantine() {}
