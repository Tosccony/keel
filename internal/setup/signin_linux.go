package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// On Linux Keel starts at sign-in through an XDG autostart entry, which
// GNOME, KDE, Xfce and the other desktops read.

func autostartEntry() string {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, _ := os.UserHomeDir()
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(config, "autostart", "keel.desktop")
}

// StartsAtSignIn reports whether Keel opens when the user signs in.
func StartsAtSignIn() bool {
	_, err := os.Stat(autostartEntry())
	return err == nil
}

// SetStartAtSignIn turns opening at sign-in on or off.
func SetStartAtSignIn(on bool) error {
	if !on {
		if err := os.Remove(autostartEntry()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := keelProgram()
	if err != nil {
		return err
	}
	if strings.ContainsAny(exe, " \t") {
		exe = `"` + exe + `"` // the Exec key quotes paths with spaces
	}
	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Keel\nComment=Local web stack\nExec=%s %s\nIcon=keel\nTerminal=false\nX-GNOME-Autostart-enabled=true\n",
		exe, HiddenFlag)
	if err := os.MkdirAll(filepath.Dir(autostartEntry()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(autostartEntry(), []byte(entry), 0o644)
}

func launchAgentOrEntry() string { return autostartEntry() }
