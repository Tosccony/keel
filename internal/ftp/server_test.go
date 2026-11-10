package ftp

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{Root: t.TempDir(), Port: freePort(t), User: "keel", Password: "abcd-efgh-jkmn"}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

// client is just enough of an FTP client for the tests: commands, and
// passive-mode transfers, the way FileZilla does them.
type client struct {
	t    *testing.T
	text *textproto.Conn
}

func dial(t *testing.T, s *Server) *client {
	t.Helper()
	conn, err := textproto.Dial("tcp", "127.0.0.1:"+strconv.Itoa(s.Port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &client{t: t, text: conn}
	c.expect(220)
	return c
}

// cmd sends a command and returns the reply code and text.
func (c *client) cmd(format string, args ...any) (int, string) {
	c.t.Helper()
	id, err := c.text.Cmd(format, args...)
	if err != nil {
		c.t.Fatal(err)
	}
	c.text.StartResponse(id)
	defer c.text.EndResponse(id)
	code, msg, err := c.text.ReadResponse(0)
	if err != nil && code == 0 {
		c.t.Fatal(err)
	}
	return code, msg
}

func (c *client) expect(code int) string {
	c.t.Helper()
	got, msg, err := c.text.ReadResponse(code)
	if err != nil {
		c.t.Fatalf("want %d, got %d %s (%v)", code, got, msg, err)
	}
	return msg
}

func (c *client) login(user, pass string) int {
	c.t.Helper()
	if code, _ := c.cmd("USER %s", user); code != 331 {
		c.t.Fatalf("USER: %d", code)
	}
	code, _ := c.cmd("PASS %s", pass)
	return code
}

// transfer runs a passive-mode data command: send writes upload, else the
// download comes back.
func (c *client) transfer(command string, upload []byte) []byte {
	c.t.Helper()
	code, msg := c.cmd("EPSV")
	if code != 229 {
		c.t.Fatalf("EPSV: %d %s", code, msg)
	}
	open, close := strings.Index(msg, "(|||"), strings.Index(msg, "|)")
	port := msg[open+4 : close]
	data, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 5*time.Second)
	if err != nil {
		c.t.Fatal(err)
	}
	id, _ := c.text.Cmd("%s", command)
	c.text.StartResponse(id)
	if code, msg, err := c.text.ReadResponse(150); err != nil {
		c.text.EndResponse(id)
		data.Close()
		c.t.Fatalf("%s: %d %s", command, code, msg)
	}
	var down []byte
	if upload != nil {
		data.Write(upload)
		data.Close()
	} else {
		down, _ = io.ReadAll(bufio.NewReader(data))
		data.Close()
	}
	if code, msg, err := c.text.ReadResponse(226); err != nil {
		c.text.EndResponse(id)
		c.t.Fatalf("%s finished with %d %s", command, code, msg)
	}
	c.text.EndResponse(id)
	return down
}

func TestUploadListDownload(t *testing.T) {
	s := startServer(t)
	c := dial(t, s)
	if code := c.login("keel", "abcd-efgh-jkmn"); code != 230 {
		t.Fatalf("login: %d", code)
	}
	c.cmd("TYPE I")
	if code, _ := c.cmd("MKD shop"); code != 257 {
		t.Fatalf("MKD: %d", code)
	}
	c.cmd("CWD shop")
	c.transfer("STOR index.php", []byte("<?php echo 'hello';\n"))

	if b, err := os.ReadFile(filepath.Join(s.Root, "shop", "index.php")); err != nil || string(b) != "<?php echo 'hello';\n" {
		t.Errorf("uploaded file on disk: %q (%v)", b, err)
	}
	if list := c.transfer("LIST", nil); !strings.Contains(string(list), "index.php") {
		t.Errorf("LIST: %q", list)
	}
	if got := c.transfer("RETR index.php", nil); string(got) != "<?php echo 'hello';\n" {
		t.Errorf("RETR: %q", got)
	}
	if code, _ := c.cmd("RNFR index.php"); code != 350 {
		t.Fatalf("RNFR: %d", code)
	}
	if code, _ := c.cmd("RNTO home.php"); code != 250 {
		t.Fatalf("RNTO: %d", code)
	}
	if code, _ := c.cmd("DELE home.php"); code != 250 {
		t.Errorf("DELE: %d", code)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "shop", "home.php")); !os.IsNotExist(err) {
		t.Errorf("deleted file still there: %v", err)
	}
}

func TestWrongLoginIsRefused(t *testing.T) {
	s := startServer(t)
	for _, login := range [][2]string{{"keel", "wrong"}, {"root", "abcd-efgh-jkmn"}, {"keel", ""}} {
		c := dial(t, s)
		if code := c.login(login[0], login[1]); code == 230 {
			t.Errorf("%s/%q logged in", login[0], login[1])
		}
	}
}

// The login reaches the websites folder and nothing above it.
func TestCantLeaveTheWebsitesFolder(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	os.WriteFile(secret, []byte("private"), 0o644)
	s := &Server{Root: filepath.Join(outside, "htdocs"), Port: freePort(t), User: "keel", Password: "pw-pw-pw"}
	os.Mkdir(s.Root, 0o755)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	c := dial(t, s)
	c.login("keel", "pw-pw-pw")
	c.cmd("TYPE I")
	c.cmd("CWD ../..")
	_, pwd := c.cmd("PWD")
	if !strings.Contains(pwd, `"/"`) {
		t.Errorf("after CWD ../.., PWD = %s", pwd)
	}
	for _, path := range []string{"../secret.txt", "/../secret.txt", "../../" + secret} {
		code, _ := c.cmd("SIZE %s", path)
		if code == 213 {
			t.Errorf("SIZE %s worked: the file outside the folder is reachable", path)
		}
	}
	c.transfer("STOR ../escape.txt", []byte("x"))
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); err == nil {
		t.Error("an upload landed outside the websites folder")
	}
}

func TestStartStopAndFailures(t *testing.T) {
	s := startServer(t)
	if !s.Running() {
		t.Fatal("not running")
	}
	s.Stop()
	if s.Running() {
		t.Error("running after Stop")
	}
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(s.Port), time.Second); err == nil {
		t.Error("the port still answers after Stop")
	}
	if err := s.Start(); err != nil {
		t.Errorf("restart: %v", err)
	}
	s.Stop()

	missing := &Server{Root: filepath.Join(t.TempDir(), "nope"), Port: freePort(t), User: "u", Password: "p"}
	if err := missing.Start(); err == nil || missing.Running() {
		t.Error("Start without the websites folder succeeded")
	}
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	taken := &Server{Root: t.TempDir(), Port: busy.Addr().(*net.TCPAddr).Port, User: "u", Password: "p"}
	if err := taken.Start(); err == nil || taken.Running() {
		t.Error("Start on a taken port succeeded")
	}
	nologin := &Server{Root: t.TempDir(), Port: freePort(t)}
	if err := nologin.Start(); err == nil {
		t.Error("Start without a login succeeded")
	}
}

var _ = fmt.Sprint
