//go:build darwin

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestClearQuarantine(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Keel.app", "Contents")
	exe := filepath.Join(app, "MacOS", "keel")
	server := filepath.Join(app, "Resources", "mariadb", "bin", "mariadbd")
	php := filepath.Join(app, "Resources", "php", "bin", "php")
	for _, f := range []string{exe, server, php} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mark := func(path string) {
		t.Helper()
		if err := unix.Setxattr(path, quarantine, []byte("0083;00000000;Safari;"), 0); err != nil {
			t.Skipf("can't set the quarantine mark here: %v", err)
		}
	}

	mark(php)
	if n := clearQuarantine(exe); n != 0 {
		t.Errorf("cleared %d files although the MariaDB server isn't marked", n)
	}

	mark(server)
	if n := clearQuarantine(exe); n < 2 {
		t.Errorf("cleared %d files, want at least the 2 marked ones", n)
	}
	for _, f := range []string{server, php} {
		if quarantined(f) {
			t.Errorf("%s is still marked", f)
		}
	}

	// Outside an app bundle (a development build): nothing is touched.
	loose := filepath.Join(t.TempDir(), "keel")
	os.WriteFile(loose, nil, 0o755)
	if n := clearQuarantine(loose); n != 0 {
		t.Errorf("cleared %d files outside an app bundle", n)
	}
}
