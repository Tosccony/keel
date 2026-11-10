// Package extras pins the parts Keel downloads when they're first turned
// on: Tomcat and a Java runtime for it, and Perl on Windows. They're in Go
// rather than a script's .env file because Keel itself downloads them, on
// the student's computer. Change a version and its checksum together; the
// checksums come from each project's release.
package extras

import "runtime"

// Tomcat is Apache Tomcat 10.1 (Java 11 and later). Checksum: the .sha512
// file published beside the download.
var Tomcat = Download{
	Version: "10.1.61",
	URLs: []string{
		"https://dlcdn.apache.org/tomcat/tomcat-10/v10.1.61/bin/apache-tomcat-10.1.61.zip",
		"https://archive.apache.org/dist/tomcat/tomcat-10/v10.1.61/bin/apache-tomcat-10.1.61.zip",
	},
	Checksum: "sha512:018fca94e41e75ac74891a55894bd94f662bc838ad92a443937c169c0b1dc5617b69ceffb4e1c9f0648c4d9e5b1da7cb5f06c3ff6daab7e305fffc955f607ed4",
	SizeMB:   13,
}

// Java is Eclipse Temurin's Java 21 runtime, for computers without Java.
// Checksums: Adoptium's API (api.adoptium.net/v3/assets/latest/21/hotspot).
func Java() (Download, bool) {
	base := "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.13%2B9/"
	d := Download{Version: "21.0.13"}
	switch runtime.GOOS + "-" + runtime.GOARCH {
	case "darwin-arm64":
		d.URLs = []string{base + "OpenJDK21U-jre_aarch64_mac_hotspot_21.0.13_9.tar.gz"}
		d.Checksum, d.SizeMB = "sha256:225bab0a09491ef06835488cd942f5d2befa9a0410a2484f0c8bbcf7506ec09a", 45
	case "darwin-amd64":
		d.URLs = []string{base + "OpenJDK21U-jre_x64_mac_hotspot_21.0.13_9.tar.gz"}
		d.Checksum, d.SizeMB = "sha256:c909674b1c10c261b9a3fb7e9c0efebd303e33f88a4fe84be7135b5e2d711f89", 39
	case "linux-amd64":
		d.URLs = []string{base + "OpenJDK21U-jre_x64_linux_hotspot_21.0.13_9.tar.gz"}
		d.Checksum, d.SizeMB = "sha256:a535a1df9c6284d9618872984e026ae0352d1f7e9e0605425c0e6114f47351dd", 49
	case "windows-amd64", "windows-arm64": // Windows on ARM runs the x64 build
		d.URLs = []string{base + "OpenJDK21U-jre_x64_windows_hotspot_21.0.13_9.zip"}
		d.Checksum, d.SizeMB = "sha256:2332c0b4cff6bed722da3567c91d5e47568bcba863fa4945d0a8d6fc76158628", 46
	default:
		return Download{}, false
	}
	return d, true
}

// Perl is Strawberry Perl for Windows (macOS and Linux have Perl). The
// portable edition is big because it carries a C compiler for building
// Perl modules. Checksum: the release's published digest. Its zip has no
// top folder: perl.exe ends up in perl/bin.
var Perl = Download{
	Version: "5.42.3.1",
	URLs: []string{
		"https://github.com/StrawberryPerl/Perl-Dist-Strawberry/releases/download/SP_54231_64bit/strawberry-perl-5.42.3.1-64bit-portable.zip",
	},
	Checksum: "sha256:6a081a811781c30aca51dbc036afd93092af91e3297901f02c17043795a10690",
	SizeMB:   290,
}

// Download is one pinned download.
type Download struct {
	Version  string
	URLs     []string
	Checksum string // "sha256:..." or "sha512:..."
	SizeMB   int    // roughly, for telling the user
}
