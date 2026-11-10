//go:build !windows

package tomcat

import (
	"os"
	"path/filepath"
	"testing"
)

// JavaVersion reads both the old "1.8.0" style and the new "21.0.13" one.
func TestJavaVersion(t *testing.T) {
	for out, want := range map[string]int{
		`openjdk version "21.0.13" 2025-10-21 LTS`: 21,
		`java version "1.8.0_401"`:                 8,
		`openjdk version "23.0.2" 2025-01-21`:      23,
		`openjdk version "11" 2018-09-25`:          11,
		`something else`:                           0,
	} {
		fake := filepath.Join(t.TempDir(), "java")
		os.WriteFile(fake, []byte("#!/bin/sh\necho '"+out+"' >&2\n"), 0o755)
		if got := JavaVersion(fake); got != want {
			t.Errorf("JavaVersion(%q) = %d, want %d", out, got, want)
		}
	}
	if JavaVersion(filepath.Join(t.TempDir(), "missing")) != 0 {
		t.Error("a missing java should give 0")
	}
}
