package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

// On macOS Keel starts at sign-in through a LaunchAgent in the user's
// Library, which needs no admin rights.

func launchAgent() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "io.github.tosccony.keel.plist")
}

// StartsAtSignIn reports whether Keel opens when the user signs in.
func StartsAtSignIn() bool {
	_, err := os.Stat(launchAgent())
	return err == nil
}

// SetStartAtSignIn turns opening at sign-in on or off.
func SetStartAtSignIn(on bool) error {
	if !on {
		if err := os.Remove(launchAgent()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := keelProgram()
	if err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>io.github.tosccony.keel</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, xmlEscape(exe), HiddenFlag)
	if err := os.MkdirAll(filepath.Dir(launchAgent()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(launchAgent(), []byte(plist), 0o644)
}

func xmlEscape(s string) string {
	out := ""
	for _, r := range s {
		switch r {
		case '&':
			out += "&amp;"
		case '<':
			out += "&lt;"
		case '>':
			out += "&gt;"
		default:
			out += string(r)
		}
	}
	return out
}

func launchAgentOrEntry() string { return launchAgent() }
