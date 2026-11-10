package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/Tosccony/keel/internal/tomcat"
)

// Folders for the parts Keel downloads when they're first turned on.
func (o Options) TomcatHome() string { return filepath.Join(o.home(), "tomcat") }
func (o Options) TomcatBase() string { return filepath.Join(o.home(), "tomcat-base") }
func (o Options) JavaHome() string   { return filepath.Join(o.home(), "java") }
func (o Options) PerlHome() string   { return filepath.Join(o.home(), "perl") }

// Java finds a Java runtime new enough for Tomcat 10.1 (Java 11): the one
// JAVA_HOME names, then the one on the PATH, then the one Keel downloaded.
// It returns "" when there's none.
func (o Options) Java() string {
	var candidates []string
	if home := os.Getenv("JAVA_HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, "bin", exe("java")))
	}
	if p, err := exec.LookPath("java"); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, o.KeelJava())
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil && tomcat.JavaVersion(c) >= 11 {
			return c
		}
	}
	return ""
}

// KeelJava is where the Java runtime Keel downloads keeps its java program.
// Temurin's macOS build is laid out as a bundle.
func (o Options) KeelJava() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(o.JavaHome(), "Contents", "Home", "bin", "java")
	}
	return filepath.Join(o.JavaHome(), "bin", exe("java"))
}
