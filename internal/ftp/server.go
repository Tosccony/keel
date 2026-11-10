// Package ftp is Keel's FTP server, in place of XAMPP's FileZilla Server.
// It serves the websites folder to one login, so students can practise
// uploading a site with an FTP client such as FileZilla.
//
// It listens on 127.0.0.1 only, and the login can't reach outside the
// websites folder.
package ftp

import (
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

// Server serves Root over FTP on Port to one user.
type Server struct {
	Root     string // the websites folder
	Port     int
	User     string
	Password string

	mu      sync.Mutex
	srv     *ftpserver.FtpServer
	done    chan struct{}
	running bool
}

// Start listens and serves in the background. It fails if the port can't
// be used or the folder doesn't exist.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	if info, err := os.Stat(s.Root); err != nil || !info.IsDir() {
		return fmt.Errorf("ftp: the websites folder %s isn't there", s.Root)
	}
	if s.User == "" || s.Password == "" {
		return errors.New("ftp: no login set")
	}
	srv := ftpserver.NewFtpServer(&driver{s: s, user: s.User, password: s.Password, root: s.Root, port: s.Port})
	if err := srv.Listen(); err != nil {
		return fmt.Errorf("ftp: port %d: %w", s.Port, err)
	}
	s.srv, s.done, s.running = srv, make(chan struct{}), true
	go func(done chan struct{}) {
		srv.Serve()
		close(done)
	}(s.done)
	return nil
}

// Stop closes the port and the open connections.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	srv, done := s.srv, s.done
	s.running = false
	s.mu.Unlock()
	err := srv.Stop()
	<-done
	return err
}

// Running reports whether the server is listening.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// driver connects ftpserverlib to Keel's settings. It keeps its own copy
// of the login and folder, so changing the Server's fields while it runs
// takes effect at the next start.
type driver struct {
	s              *Server
	user, password string
	root           string
	port           int
}

func (d *driver) GetSettings() (*ftpserver.Settings, error) {
	return &ftpserver.Settings{
		ListenAddr:  net.JoinHostPort("127.0.0.1", strconv.Itoa(d.port)),
		PublicHost:  "127.0.0.1", // passive connections come back to this computer
		Banner:      "Keel FTP server",
		IdleTimeout: 900,
	}, nil
}

func (d *driver) ClientConnected(ftpserver.ClientContext) (string, error) {
	return "Keel FTP server: log in with the username and password shown in Keel", nil
}

func (d *driver) ClientDisconnected(ftpserver.ClientContext) {}

func (d *driver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(d.user)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(d.password)) == 1
	if !userOK || !passOK {
		return nil, errors.New("wrong username or password")
	}
	// BasePathFs refuses any path that resolves outside the folder.
	return afero.NewBasePathFs(afero.NewOsFs(), d.root), nil
}

// GetTLSConfig: no FTPS. The server only takes connections from this
// computer, where there's nothing to eavesdrop on.
func (d *driver) GetTLSConfig() (*tls.Config, error) {
	return nil, errors.New("FTPS isn't offered: the server only takes connections from this computer")
}
