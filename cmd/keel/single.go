package main

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tosccony/keel/internal/mariadb"
)

// showRequest is what a second Keel sends the first. A browser's request
// to the same port starts with "GET", so it can't pop the window up.
const showRequest = "keel: show the window"

// oneKeel lets one Keel window run per Keel folder (home). The first takes
// keel-window.lock there and listens on 127.0.0.1 for later ones, whose
// port it writes to keel-window.port. A later Keel asks the first to show
// its window, and gets first == false so it can exit. If the lock can't
// be checked at all, Keel runs: two windows beat none.
func oneKeel(home string, show func()) (release func(), first bool) {
	os.MkdirAll(home, 0o755)
	portFile := filepath.Join(home, "keel-window.port")
	unlock, taken, err := mariadb.LockPath(filepath.Join(home, "keel-window.lock"))
	if taken {
		if b, err := os.ReadFile(portFile); err == nil {
			if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strings.TrimSpace(string(b)), time.Second); err == nil {
				c.Write([]byte(showRequest + "\n"))
				c.Close()
			}
		}
		return func() {}, false
	}
	if err != nil {
		return func() {}, true
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return unlock, true
	}
	os.WriteFile(portFile, []byte(strconv.Itoa(l.Addr().(*net.TCPAddr).Port)), 0o644)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, _ := bufio.NewReader(c).ReadString('\n')
			c.Close()
			if strings.TrimSpace(line) == showRequest {
				show()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { l.Close(); unlock() }) }, true
}
