package setup

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// On Windows Keel starts at sign-in through the current user's Run key,
// which needs no admin rights.

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// StartsAtSignIn reports whether Keel opens when the user signs in.
func StartsAtSignIn() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("Keel")
	return err == nil
}

// SetStartAtSignIn turns opening at sign-in on or off.
func SetStartAtSignIn(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("Keel"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := keelProgram()
	if err != nil {
		return err
	}
	return k.SetStringValue("Keel", `"`+exe+`" `+HiddenFlag)
}
