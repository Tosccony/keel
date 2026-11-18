//go:build !windows

package setup

// registerPerl does nothing outside Windows: Apache runs scripts through
// their "#!/usr/bin/perl" line.
func registerPerl(string) error { return nil }

// forgetPerl does nothing outside Windows: registerPerl wrote nothing.
func forgetPerl(string) {}
