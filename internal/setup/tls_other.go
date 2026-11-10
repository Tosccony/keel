//go:build !windows

package setup

// needsNoTLS is for Windows on ARM; MariaDB runs natively everywhere else.
func needsNoTLS() bool { return false }
