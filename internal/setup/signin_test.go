//go:build !windows

package setup

import (
	"os"
	"strings"
	"testing"
)

// Uses a temporary home folder, so the real sign-in items are untouched.
func TestStartAtSignIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	if StartsAtSignIn() {
		t.Fatal("a new home folder shouldn't start Keel at sign-in")
	}
	if err := SetStartAtSignIn(true); err != nil {
		t.Fatal(err)
	}
	if !StartsAtSignIn() {
		t.Fatal("not on after turning it on")
	}
	var path string
	if p := launchAgentOrEntry(); p != "" {
		path = p
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), HiddenFlag) || !strings.HasPrefix(path, home) {
		t.Errorf("%s: %v\n%s", path, err, b)
	}
	if err := SetStartAtSignIn(false); err != nil || StartsAtSignIn() {
		t.Errorf("turning it off: %v, still on %v", err, StartsAtSignIn())
	}
	if err := SetStartAtSignIn(false); err != nil {
		t.Errorf("turning it off twice: %v", err)
	}
}
