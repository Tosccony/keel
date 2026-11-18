package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A second Keel doesn't run: it asks the first to show its window. Once
// the first quits, a new one can run.
func TestOnlyOneKeel(t *testing.T) {
	home := t.TempDir()
	shown := make(chan bool, 4)
	release, first := oneKeel(home, func() { shown <- true })
	if !first {
		t.Fatal("the first Keel should run")
	}
	// A browser reaching the port mustn't pop the window up.
	port, _ := os.ReadFile(filepath.Join(home, "keel-window.port"))
	if c, err := net.Dial("tcp", "127.0.0.1:"+strings.TrimSpace(string(port))); err == nil {
		c.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
		c.Close()
	}
	if _, again := oneKeel(home, func() {}); again {
		t.Fatal("a second Keel ran")
	}
	select {
	case <-shown:
	case <-time.After(3 * time.Second):
		t.Fatal("the first Keel wasn't asked to show its window")
	}
	select {
	case <-shown:
		t.Fatal("something other than a second Keel showed the window")
	case <-time.After(300 * time.Millisecond):
	}
	release()
	next, ok := oneKeel(home, func() {})
	if !ok {
		t.Fatal("a new Keel couldn't run after the first quit")
	}
	next()
}
