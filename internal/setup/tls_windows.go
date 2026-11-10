package setup

import "golang.org/x/sys/windows"

const imageFileMachineARM64 = 0xAA64

// needsNoTLS reports whether MariaDB must run without TLS: on Windows on
// ARM the x64 MariaDB runs in Microsoft's emulator, and its TLS library
// aborts the server there (see mariadb.Server.NoTLS).
func needsNoTLS() bool {
	var process, native uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &process, &native); err != nil {
		return false
	}
	return native == imageFileMachineARM64
}
