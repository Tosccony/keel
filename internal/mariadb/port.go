package mariadb

import (
	"net"
	"strconv"
	"time"
)

// portInUse reports whether anything accepts connections on the port at
// 127.0.0.1 or ::1.
//
// It connects instead of test-binding. On macOS and Windows, binding
// 127.0.0.1:3306 succeeds even while another server holds *:3306, and the
// result would be two servers behind one port.
//
// This doesn't see a program that only listens on a network address (not
// loopback). Smart port selection will replace it with the OS's listener
// table, which also reports which program holds the port.
func portInUse(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
		if err == nil {
			c.Close()
			return true
		}
	}
	return false
}
