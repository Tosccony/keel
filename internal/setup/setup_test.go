package setup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// layout makes an empty mariadbd at dir/rel/bin and returns that bin folder.
func layout(t *testing.T, dir, rel string) string {
	t.Helper()
	bin := filepath.Join(dir, rel, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, exe("mariadbd")), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestFindBundledInEachInstallersLayout(t *testing.T) {
	for _, tt := range []struct{ name, exeDir, mariadb string }{
		{"Windows and the Linux .tar.gz", "Keel", "Keel/mariadb"},
		{"macOS app", "Keel.app/Contents/MacOS", "Keel.app/Contents/Resources/mariadb"},
		{"Linux .deb and .rpm", "usr/bin", "usr/lib/keel/mariadb"},
	} {
		root := t.TempDir()
		want := layout(t, root, tt.mariadb)
		if got := findBundled(filepath.Join(root, tt.exeDir)); got != want {
			t.Errorf("%s: found %q, want %q", tt.name, got, want)
		}
	}
	if got := findBundled(t.TempDir()); got != "" {
		t.Errorf("with no MariaDB beside Keel, found %q", got)
	}
}

func TestDevelopmentBuildUsesTheLab(t *testing.T) {
	// The test binary has no MariaDB beside it, like a development build.
	if d := Defaults(); d.Bundled || d.Port != LabPort || !strings.HasSuffix(d.Folder, filepath.Join("personal", "keel-lab")) {
		t.Errorf("Defaults() = %+v, want the lab", d)
	}
}

func TestAppDataDirIsLocalAndPerUser(t *testing.T) {
	dir := appDataDir()
	switch runtime.GOOS {
	case "darwin":
		if !strings.HasSuffix(dir, filepath.Join("Library", "Application Support")) {
			t.Errorf("macOS: %s", dir)
		}
	case "windows":
		if strings.Contains(strings.ToLower(dir), "roaming") {
			t.Errorf("Windows must not use the roaming folder: %s", dir)
		}
	default:
		t.Setenv("XDG_DATA_HOME", "/xdg")
		if got := appDataDir(); got != "/xdg" {
			t.Errorf("Linux with XDG_DATA_HOME: %s", got)
		}
	}
}

// Prepare on a brand-new folder creates the data folder, the way an
// installed Keel's first start does. It needs mariadb-install-db.
func TestPrepareSetsUpOnFirstStart(t *testing.T) {
	bin := Lab().BinDir
	if _, err := os.Stat(filepath.Join(bin, "mariadb-install-db")); err != nil {
		t.Skip("needs MariaDB installed: ", err)
	}
	o := Options{BinDir: bin, Folder: filepath.Join(t.TempDir(), "mariadb"), Port: freePort(t)}
	s := o.Server()
	if s.IsSetUp() {
		t.Fatal("a new folder shouldn't count as set up")
	}
	if clean, why := s.LastShutdown(); !clean {
		t.Errorf("before the first start there's nothing to recover, but: %s", why)
	}
	note, err := o.Prepare(context.Background(), s)
	if err != nil || note != "" {
		t.Fatalf("Prepare: note %q, err %v", note, err)
	}
	if !s.IsSetUp() {
		t.Fatal("Prepare didn't create the data folder")
	}
	if clean, why := s.LastShutdown(); !clean {
		t.Errorf("a new data folder counts as cleanly stopped, but: %s", why)
	}
	if _, err := o.Prepare(context.Background(), s); err != nil {
		t.Errorf("a second Prepare should do nothing: %v", err)
	}
}
