package setup

import "os"

// HiddenFlag is the command-line flag Keel starts with at sign-in: no
// window, just the tray icon and the parts set to start with Keel.
const HiddenFlag = "--hidden"

// keelProgram is the program to start at sign-in: this one.
func keelProgram() (string, error) { return os.Executable() }
