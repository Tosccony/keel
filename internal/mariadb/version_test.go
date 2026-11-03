package mariadb

import "testing"

func TestParseVersion(t *testing.T) {
	tests := map[string]string{
		"mariadbd  Ver 13.0.2-MariaDB for osx10.23 on arm64 (Homebrew)\n":    "13.0.2",
		`C:\mariadb\bin\mariadbd.exe  Ver 11.4.5-MariaDB for Win64 on AMD64`: "11.4.5",
		"mariadbd  Ver 11.8.3-MariaDB-0ubuntu1 for debian-linux-gnu":         "11.8.3",
	}
	for out, want := range tests {
		got, err := parseVersion(out)
		if err != nil || got != want {
			t.Errorf("parseVersion(%q) = %q, %v; want %q", out, got, err, want)
		}
	}
	if _, err := parseVersion("something else"); err == nil {
		t.Error("parseVersion accepted output with no version")
	}
}

func TestVersion(t *testing.T) {
	s := &Server{BinDir: binDir(t)}
	v, err := s.Version()
	if err != nil || v == "" {
		t.Fatalf("Version() = %q, %v", v, err)
	}
}
