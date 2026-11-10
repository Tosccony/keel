package setup

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Part names, used as keys in the settings file for ports and autostart.
const (
	PartMariaDB = "mariadb"
	PartWeb     = "web"     // Apache, HTTP
	PartHTTPS   = "https"   // Apache, HTTPS
	PartPHP     = "php"     // PHP-FPM, behind Apache on macOS and Linux
	PartMail    = "mail"    // the mail catcher's SMTP port
	PartMailWeb = "mailweb" // the mail catcher's inbox page
	PartFTP     = "ftp"
	PartTomcat  = "tomcat"
)

// DefaultPorts are where each part starts. MovePort moves on from them if
// another program holds one.
var DefaultPorts = map[string]int{
	PartMariaDB: DefaultPort,
	PartWeb:     8080,
	PartHTTPS:   8443,
	PartPHP:     9000,
	PartMail:    1025,
	PartMailWeb: 8025,
	PartFTP:     2121,
	PartTomcat:  8081,
}

// Settings are the choices Keel keeps between runs, in keel-settings.json
// in Keel's folder: ports it moved to, the websites folder, which parts
// start with Keel, and the FTP login. They're safe for concurrent use, and
// every part of Keel shares one copy per file, so two parts saving at once
// can't undo each other.
type Settings struct {
	path string
	mu   sync.Mutex
	data settingsFile
}

type settingsFile struct {
	MariaDBPort int            `json:"mariadbPort,omitempty"` // older Keels; read into Ports
	Ports       map[string]int `json:"ports,omitempty"`
	Sites       string         `json:"sites,omitempty"`
	Autostart   []string       `json:"autostart,omitempty"`
	FTPUser     string         `json:"ftpUser,omitempty"`
	FTPPassword string         `json:"ftpPassword,omitempty"`
}

var (
	openMu   sync.Mutex
	openFile = map[string]*Settings{}
)

// OpenSettings returns the settings kept in path. A missing or unreadable
// file gives empty settings; the next save writes a good one.
func OpenSettings(path string) *Settings {
	openMu.Lock()
	defer openMu.Unlock()
	if s, ok := openFile[path]; ok {
		return s
	}
	s := &Settings{path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &s.data) // a broken file leaves the defaults
	}
	if s.data.Ports == nil {
		s.data.Ports = map[string]int{}
	}
	if s.data.MariaDBPort != 0 && s.data.Ports[PartMariaDB] == 0 {
		s.data.Ports[PartMariaDB] = s.data.MariaDBPort
	}
	s.data.MariaDBPort = 0
	openFile[path] = s
	return s
}

// forgetSettings drops the shared copy, so the next OpenSettings rereads
// the file. Tests use it.
func forgetSettings(path string) {
	openMu.Lock()
	delete(openFile, path)
	openMu.Unlock()
}

// Port returns the port saved for a part, or fallback if none is.
func (s *Settings) Port(part string, fallback int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.data.Ports[part]; p != 0 {
		return p
	}
	return fallback
}

// SetPort saves a part's port.
func (s *Settings) SetPort(part string, port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Ports[part] = port
	return s.save()
}

// takenByOthers reports, for MovePort, whether a port belongs to another
// part of Keel (saved, or that part's default).
func (s *Settings) takenByOthers(part string) func(int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := map[int]bool{}
	for p, port := range DefaultPorts {
		if p != part {
			taken[port] = true
		}
	}
	for p, port := range s.data.Ports {
		if p != part {
			taken[port] = true
		}
	}
	return func(port int) bool { return taken[port] }
}

// Sites returns the websites folder (XAMPP's htdocs), or "" before the
// user has chosen one.
func (s *Settings) Sites() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Sites
}

// SetSites saves the websites folder.
func (s *Settings) SetSites(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Sites = dir
	return s.save()
}

// Autostart reports whether a part starts when Keel opens.
func (s *Settings) Autostart(part string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.data.Autostart, part)
}

// SetAutostart saves whether a part starts when Keel opens.
func (s *Settings) SetAutostart(part string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Autostart = slices.DeleteFunc(s.data.Autostart, func(p string) bool { return p == part })
	if on {
		s.data.Autostart = append(s.data.Autostart, part)
		slices.Sort(s.data.Autostart)
	}
	return s.save()
}

// FTPLogin returns the FTP server's username and password, making up a
// password the first time.
func (s *Settings) FTPLogin() (user, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.FTPUser == "" {
		s.data.FTPUser = "keel"
	}
	if s.data.FTPPassword == "" {
		s.data.FTPPassword = newPassword()
		s.save() // if this fails, the login lasts until Keel closes
	}
	return s.data.FTPUser, s.data.FTPPassword
}

// SetFTPLogin saves the FTP server's username and password.
func (s *Settings) SetFTPLogin(user, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.FTPUser, s.data.FTPPassword = user, password
	return s.save()
}

// save writes the file whole or not at all. Callers hold mu.
func (s *Settings) save() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil { // holds the FTP password
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// newPassword makes a password that's easy to read aloud in a classroom:
// three groups of four lowercase letters and digits, without look-alikes.
func newPassword() string {
	const chars = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 12)
	rand.Read(b)
	out := make([]byte, 0, 14)
	for i, c := range b {
		if i > 0 && i%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, chars[int(c)%len(chars)])
	}
	return string(out)
}
