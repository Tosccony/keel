// Package ports finds free TCP ports for Keel's servers, and the program
// holding a port that's taken.
//
// XAMPP only warns when a port is taken, and leaves the user to find the
// culprit. Keel names it and moves to the next free port instead.
package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Span is how many ports Next tries, counting the first.
const Span = 100

// ErrNoFreePort means every port Next tried was taken.
var ErrNoFreePort = errors.New("no free port")

// Answers reports whether anything accepts connections on the port at
// 127.0.0.1 or ::1.
//
// Connecting catches what test-binding misses: on macOS and Windows,
// binding 127.0.0.1:3306 succeeds even while another server holds *:3306,
// and the result would be two servers behind one port.
func Answers(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
		if err == nil {
			c.Close()
			return true
		}
	}
	return false
}

// Free reports whether a Keel server could listen on 127.0.0.1 at port:
// nothing answers there, and the port can be bound.
func Free(port int) bool {
	if port < 1 || port > 65535 || Answers(port) {
		return false
	}
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// Next returns the first free port from start on, trying Span ports and
// passing over any that skip rules out (such as ports another Keel server
// is set to use). skip may be nil.
func Next(start int, skip func(int) bool) (int, error) {
	last := min(start+Span-1, 65535)
	for p := start; p <= last; p++ {
		if skip != nil && skip(p) {
			continue
		}
		if Free(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("%w from %d to %d", ErrNoFreePort, start, last)
}

// Holder is a program listening on a port.
type Holder struct {
	PID  int32
	Name string // "mysqld", for example; empty if the system won't say
	Exe  string // full path; empty if the system won't say
}

// String describes the holder for a person, like "mysqld (PID 412)", and
// adds "from XAMPP" when its path says so.
func (h Holder) String() string {
	name := h.Name
	if name == "" {
		name = "a program"
	}
	s := fmt.Sprintf("%s (PID %d)", name, h.PID)
	if strings.Contains(strings.ToLower(h.Exe), "xampp") {
		s += " from XAMPP"
	}
	return s
}

// Who returns the program listening on port, if the system will say.
//
// Windows reports every program. macOS and Linux only report the current
// user's programs unless Keel runs as an administrator, so a server
// started by another user (or as a system service) isn't found.
func Who(ctx context.Context, port int) (Holder, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conns, err := gnet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return Holder{}, false
	}
	for _, c := range conns {
		if c.Status != "LISTEN" || int(c.Laddr.Port) != port || c.Pid == 0 {
			continue
		}
		h := Holder{PID: c.Pid}
		if p, err := process.NewProcessWithContext(ctx, c.Pid); err == nil {
			h.Name, _ = p.NameWithContext(ctx)
			h.Exe, _ = p.ExeWithContext(ctx)
		}
		return h, true
	}
	return Holder{}, false
}
