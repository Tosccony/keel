// Package mail is Keel's mail catcher. It takes the place of XAMPP's
// Mercury mail server: PHP sends email to it over SMTP, it keeps every
// message as a file, and an inbox page shows them. Nothing ever leaves the
// computer, so students can test sign-up and password-reset emails safely.
//
// Both the SMTP port and the inbox page listen on 127.0.0.1 only.
package mail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultKeep is how many messages the catcher keeps; older ones go.
const DefaultKeep = 500

// MaxSize is the biggest message accepted, attachments included.
const MaxSize = 25 << 20

// Catcher receives mail on SMTPPort and shows it on WebPort.
type Catcher struct {
	Dir      string // where messages are kept, one .eml file each
	SMTPPort int
	WebPort  int
	Keep     int // most messages kept; 0 means DefaultKeep

	mu      sync.Mutex
	smtp    net.Listener
	webL    net.Listener // closed by Stop itself: Shutdown misses it if Serve hasn't begun yet
	web     *http.Server
	conns   sync.WaitGroup
	running bool
}

// Start listens on both ports. It fails, changing nothing, if either port
// can't be used.
func (c *Catcher) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	smtpL, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.SMTPPort)))
	if err != nil {
		return fmt.Errorf("mail: SMTP port %d: %w", c.SMTPPort, err)
	}
	webL, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.WebPort)))
	if err != nil {
		smtpL.Close()
		return fmt.Errorf("mail: inbox page port %d: %w", c.WebPort, err)
	}
	c.smtp, c.webL = smtpL, webL
	c.web = &http.Server{Handler: c.handler(), ReadHeaderTimeout: 10 * time.Second}
	c.running = true
	go c.serveSMTP(smtpL)
	go c.web.Serve(webL)
	return nil
}

// Stop closes both ports and waits for messages being received to finish
// or ctx to end.
func (c *Catcher) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	smtpL, webL, web := c.smtp, c.webL, c.web
	c.mu.Unlock()

	smtpL.Close()
	err := web.Shutdown(ctx)
	webL.Close()
	done := make(chan struct{})
	go func() { c.conns.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return err
}

// Running reports whether the catcher is listening.
func (c *Catcher) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// InboxURL is the inbox page's address.
func (c *Catcher) InboxURL() string { return fmt.Sprintf("http://localhost:%d/", c.WebPort) }

// save stores a message the SMTP side received and drops the oldest ones
// beyond Keep. Files are written under a temporary name and renamed, so the
// inbox never shows half a message.
func (c *Catcher) save(raw []byte) (string, error) {
	id := newID()
	tmp := filepath.Join(c.Dir, "."+id+".tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, filepath.Join(c.Dir, id+".eml")); err != nil {
		os.Remove(tmp)
		return "", err
	}
	c.prune()
	return id, nil
}

func (c *Catcher) prune() {
	keep := c.Keep
	if keep <= 0 {
		keep = DefaultKeep
	}
	ids, err := c.ids()
	if err != nil || len(ids) <= keep {
		return
	}
	for _, id := range ids[keep:] {
		os.Remove(filepath.Join(c.Dir, id+".eml"))
	}
}

// ids lists the stored messages, newest first. Names start with the time
// received, so they sort by age.
func (c *Catcher) ids() ([]string, error) {
	entries, err := os.ReadDir(c.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".eml") && !e.IsDir() {
			ids = append(ids, strings.TrimSuffix(name, ".eml"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}

// path returns a message's file, refusing anything that isn't an ID this
// package made, so a request can't reach outside the folder.
func (c *Catcher) path(id string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("mail: no message %q", id)
	}
	return filepath.Join(c.Dir, id+".eml"), nil
}

// Delete removes one message.
func (c *Catcher) Delete(id string) error {
	p, err := c.path(id)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// DeleteAll empties the inbox.
func (c *Catcher) DeleteAll() error {
	ids, err := c.ids()
	if err != nil {
		return err
	}
	for _, id := range ids {
		os.Remove(filepath.Join(c.Dir, id+".eml"))
	}
	return nil
}

// newID is the time received (sortable) plus random hex.
func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%020d-%s", time.Now().UnixNano(), hex.EncodeToString(b))
}

func validID(id string) bool {
	stamp, random, ok := strings.Cut(id, "-")
	if !ok || len(stamp) != 20 || len(random) != 8 {
		return false
	}
	for _, r := range stamp {
		if r < '0' || r > '9' {
			return false
		}
	}
	_, err := hex.DecodeString(random)
	return err == nil
}
