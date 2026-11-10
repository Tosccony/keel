package mariadb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersions(t *testing.T) {
	if got := versionNumber("11.8.9-MariaDB-log"); got != "11.8.9" {
		t.Errorf("versionNumber = %q", got)
	}
	for _, c := range []struct {
		a, b string
		newer bool
	}{
		{"11.8.10-MariaDB", "11.8.9-MariaDB", true},
		{"12.0.1-MariaDB", "11.8.9-MariaDB", true},
		{"11.8.9-MariaDB", "11.8.9-MariaDB", false},
		{"11.8.9-MariaDB", "11.8.10-MariaDB", false},
		{"11.8.9-MariaDB", "", true},
	} {
		if got := newerVersion(c.a, c.b); got != c.newer {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// A data folder made by an older MariaDB (as after an update of Keel) gets
// mariadb-upgrade on the next start, even with a root password; a current
// one doesn't.
func TestUpgradeAfterAMariaDBUpdate(t *testing.T) {
	s := startTestServer(t)
	if _, err := os.Stat(filepath.Join(s.BinDir, exe("mariadb-upgrade"))); err != nil {
		t.Skip("no mariadb-upgrade beside mariadbd")
	}
	info := filepath.Join(s.DataDir, upgradeInfoName)
	current, err := os.ReadFile(info)
	if err != nil {
		t.Fatalf("a new data folder has no %s: %v", upgradeInfoName, err)
	}
	setRootPassword(t, s, "class-password")
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	var msgs []string
	s.Progress = func(m string) { msgs = append(msgs, m) }
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if log := strings.Join(msgs, "\n"); strings.Contains(log, "updated") {
		t.Errorf("an up-to-date data folder was upgraded:\n%s", log)
	}
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(info, []byte("10.11.2-MariaDB"), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs = nil
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	log := strings.Join(msgs, "\n")
	if !strings.Contains(log, "made for 10.11.2") || !strings.Contains(log, "System tables updated.") {
		t.Errorf("no upgrade after a MariaDB update:\n%s", log)
	}
	if after, _ := os.ReadFile(info); versionNumber(strings.TrimSpace(string(after))) != versionNumber(strings.TrimSpace(string(current))) {
		t.Errorf("%s says %q after the upgrade, want %q", upgradeInfoName, after, current)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(s.DataDir), ".keel-upgrade-*")); len(leftovers) > 0 {
		t.Errorf("settings file left behind: %v", leftovers)
	}
}
