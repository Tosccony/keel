package lab

import (
	"context"
	"flag"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func testOptions(t *testing.T, port int) Options {
	t.Helper()
	return Options{Folder: t.TempDir(), Port: port}
}

func takePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestChoosePortKeepsAFreePort(t *testing.T) {
	port := freePort(t)
	o := testOptions(t, port)
	s := o.Server()
	note, err := o.ChoosePort(context.Background(), s)
	if err != nil || note != "" || s.Port != port {
		t.Errorf("free port: note %q, err %v, port %d; want no change", note, err, s.Port)
	}
	if _, err := os.Stat(o.settingsFile()); !os.IsNotExist(err) {
		t.Errorf("nothing moved, so no settings file should be written (stat err = %v)", err)
	}
}

func TestChoosePortMovesAndRemembers(t *testing.T) {
	taken := takePort(t)
	o := testOptions(t, taken)
	s := o.Server()
	note, err := o.ChoosePort(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Port == taken {
		t.Fatalf("still on taken port %d", taken)
	}
	for _, want := range []string{strconv.Itoa(taken), strconv.Itoa(s.Port), "PID " + strconv.Itoa(os.Getpid())} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q doesn't mention %q", note, want)
		}
	}

	// The next run starts on the new port, even though the preferred one
	// is free again.
	again := Options{Folder: o.Folder, Port: taken}
	if got := again.Server().Port; got != s.Port {
		t.Errorf("next run uses port %d, want the remembered %d", got, s.Port)
	}

	// -port still overrides the remembered port.
	fs := flag.NewFlagSet("keel", flag.ContinueOnError)
	explicit := Options{Folder: o.Folder, Port: 3307}
	explicit.AddFlags(fs)
	if err := fs.Parse([]string{"-port", "4406"}); err != nil {
		t.Fatal(err)
	}
	if got := explicit.Server().Port; got != 4406 {
		t.Errorf("with -port 4406, port = %d", got)
	}
}

func TestPortFlagRejectsNonsense(t *testing.T) {
	for _, v := range []string{"abc", "0", "70000"} {
		fs := flag.NewFlagSet("keel", flag.ContinueOnError)
		fs.SetOutput(new(strings.Builder))
		o := Options{Port: 3307}
		o.AddFlags(fs)
		if err := fs.Parse([]string{"-port", v}); err == nil {
			t.Errorf("-port %s was accepted", v)
		}
	}
}

func TestChoosePortStillMovesWhenSettingsCantBeSaved(t *testing.T) {
	taken := takePort(t)
	o := testOptions(t, taken)
	// A folder where the settings file should be makes saving fail.
	if err := os.Mkdir(o.settingsFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	s := o.Server()
	note, err := o.ChoosePort(context.Background(), s)
	if err != nil || s.Port == taken || !strings.Contains(note, "couldn't save") {
		t.Errorf("note %q, err %v, port %d; want a move with a warning", note, err, s.Port)
	}
}

func TestBrokenSettingsFileIsIgnored(t *testing.T) {
	o := testOptions(t, 3307)
	if err := os.WriteFile(filepath.Join(o.Folder, "keel-settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := o.Server().Port; got != 3307 {
		t.Errorf("with a broken settings file, port = %d, want the default 3307", got)
	}
}
