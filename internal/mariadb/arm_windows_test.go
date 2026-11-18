package mariadb

import "golang.org/x/sys/windows"

// emulatedOnARM reports whether these tests run in Windows on ARM's x64
// emulator. It's the check setup.needsNoTLS makes for Keel itself.
func emulatedOnARM() bool {
	var process, native uint16
	return windows.IsWow64Process2(windows.CurrentProcess(), &process, &native) == nil && native == 0xAA64
}
