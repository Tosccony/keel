package setup

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestSettingsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keel-settings.json")
	s := OpenSettings(path)
	if s.Port(PartWeb, 8080) != 8080 || s.Sites() != "" || s.Autostart(PartWeb) {
		t.Fatal("new settings should be empty")
	}
	s.SetPort(PartWeb, 8090)
	s.SetSites("/home/student/Documents/Keel/htdocs")
	s.SetAutostart(PartWeb, true)
	s.SetAutostart(PartMariaDB, true)
	s.SetAutostart(PartMariaDB, false)
	user, pass := s.FTPLogin()

	forgetSettings(path) // as if Keel restarted
	again := OpenSettings(path)
	if again.Port(PartWeb, 8080) != 8090 || again.Sites() != "/home/student/Documents/Keel/htdocs" ||
		!again.Autostart(PartWeb) || again.Autostart(PartMariaDB) {
		t.Errorf("after a restart: %+v", again.data)
	}
	if u, p := again.FTPLogin(); u != user || p != pass || u != "keel" {
		t.Errorf("FTP login changed across a restart: %s/%s, was %s/%s", u, p, user, pass)
	}
	if !regexp.MustCompile(`^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$`).MatchString(pass) {
		t.Errorf("password %q isn't three groups of four", pass)
	}
	// Windows has no file modes (Go reports 0666); its per-user folders keep the file private there.
	if info, err := os.Stat(path); runtime.GOOS != "windows" && (err != nil || info.Mode().Perm() != 0o600) {
		t.Errorf("the settings file holds a password, so only its owner should read it: %v %v", info.Mode(), err)
	}
}

func TestOlderSettingsFileIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keel-settings.json")
	os.WriteFile(path, []byte(`{"mariadbPort": 3310}`), 0o644)
	s := OpenSettings(path)
	if got := s.Port(PartMariaDB, 3306); got != 3310 {
		t.Errorf("port from an older file = %d, want 3310", got)
	}
	s.SetAutostart(PartWeb, true) // any save rewrites the file in the new form
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "mariadbPort") || !strings.Contains(string(b), `"mariadb": 3310`) {
		t.Errorf("rewritten file:\n%s", b)
	}
}

func TestSettingsSavedAtOnceKeepEveryChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keel-settings.json")
	s := OpenSettings(path)
	var wg sync.WaitGroup
	for i, part := range []string{PartWeb, PartHTTPS, PartMail, PartMailWeb, PartFTP, PartTomcat} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.SetPort(part, 20000+i)
			s.SetAutostart(part, true)
		}()
	}
	wg.Wait()
	forgetSettings(path)
	again := OpenSettings(path)
	for i, part := range []string{PartWeb, PartHTTPS, PartMail, PartMailWeb, PartFTP, PartTomcat} {
		if again.Port(part, 0) != 20000+i || !again.Autostart(part) {
			t.Errorf("%s lost its change: port %d, autostart %v", part, again.Port(part, 0), again.Autostart(part))
		}
	}
}

// A part that has to move must not land on a port another part of Keel
// is set to use.
func TestMovePortSkipsOtherPartsPorts(t *testing.T) {
	taken := takePort(t)
	o := Options{Home: t.TempDir()}
	st := o.Settings()
	st.SetPort(PartTomcat, taken+1) // Tomcat is set to use the next port
	port, note, err := o.MovePort(context.Background(), PartWeb, "the web server", taken)
	if err != nil {
		t.Fatal(err)
	}
	if port == taken || port == taken+1 {
		t.Errorf("moved to %d, which is taken or Tomcat's", port)
	}
	if !strings.Contains(note, "the web server will use port") || st.Port(PartWeb, 0) != port {
		t.Errorf("note %q, saved %d", note, st.Port(PartWeb, 0))
	}
	if same, note, _ := o.MovePort(context.Background(), PartWeb, "the web server", port); same != port || note != "" {
		t.Errorf("a free port moved: %d, %q", same, note)
	}
}

func TestQuitOnCloseAndPerlSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keel-settings.json")
	s := OpenSettings(path)
	if s.QuitOnClose() || s.Perl() {
		t.Fatal("both should be off by default")
	}
	s.SetQuitOnClose(true)
	s.SetPerl(true)
	forgetSettings(path)
	again := OpenSettings(path)
	if !again.QuitOnClose() || !again.Perl() {
		t.Error("the settings didn't survive a restart")
	}
}

// A portable Keel's websites folder sits beside its data folder. It's saved
// relative to Keel's own folder, so it's still found after the drive gets
// another letter; a folder elsewhere stays as it was given.
func TestSitesInsideKeelsFolderMoveWithIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "E", "Keel")
	path := filepath.Join(root, "data", "keel-settings.json")
	sites := filepath.Join(root, "htdocs")
	if err := OpenSettings(path).SetSites(sites); err != nil {
		t.Fatal(err)
	}
	forgetSettings(path)
	if got := OpenSettings(path).data.Sites; got != "htdocs" {
		t.Errorf("saved %q, want it relative to Keel's folder", got)
	}

	moved := filepath.Join(filepath.Dir(filepath.Dir(root)), "F", "Keel") // the drive's new letter
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	movedPath := filepath.Join(moved, "data", "keel-settings.json")
	if got, want := OpenSettings(movedPath).Sites(), filepath.Join(moved, "htdocs"); got != want {
		t.Errorf("after the move: %q, want %q", got, want)
	}

	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := OpenSettings(movedPath).SetSites(outside); err != nil {
		t.Fatal(err)
	}
	forgetSettings(movedPath)
	if got := OpenSettings(movedPath); got.data.Sites != outside || got.Sites() != outside {
		t.Errorf("a folder outside Keel's: saved %q, read %q, want %q", got.data.Sites, got.Sites(), outside)
	}
}
