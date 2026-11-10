package ports

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// listen opens a listener on addr ("127.0.0.1:0", ":0", ...) and returns
// its port.
func listen(t *testing.T, addr string) int {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("can't listen on %s: %v", addr, err)
	}
	t.Cleanup(func() { l.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

func TestFreeAndAnswers(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !Answers(port) || Free(port) {
		t.Errorf("with a listener open: Answers=%v Free=%v, want true/false", Answers(port), Free(port))
	}
	l.Close()
	if Answers(port) || !Free(port) {
		t.Errorf("after closing it: Answers=%v Free=%v, want false/true", Answers(port), Free(port))
	}
}

// A server on every address (*:port) must count as taken, even though
// binding 127.0.0.1:port alone might succeed on macOS and Windows.
func TestAllAddressesListenerCountsAsTaken(t *testing.T) {
	port := listen(t, ":0")
	if Free(port) {
		t.Errorf("Free(%d) = true while *:%d is taken", port, port)
	}
}

func TestIPv6OnlyListenerCountsAsTaken(t *testing.T) {
	port := listen(t, "[::1]:0")
	if Free(port) {
		t.Errorf("Free(%d) = true while [::1]:%d is taken", port, port)
	}
}

func TestFreeRejectsImpossiblePorts(t *testing.T) {
	for _, p := range []int{-1, 0, 65536} {
		if Free(p) {
			t.Errorf("Free(%d) = true", p)
		}
	}
}

func TestNextPassesOverTakenPorts(t *testing.T) {
	taken := listen(t, "127.0.0.1:0")
	got, err := Next(taken, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got <= taken || !Free(got) {
		t.Errorf("Next(%d) = %d, want a free port after it", taken, got)
	}

	// skip rules ports out even when they're free.
	got, err = Next(taken+1, func(p int) bool { return p < taken+4 })
	if err != nil {
		t.Fatal(err)
	}
	if got < taken+4 {
		t.Errorf("Next with the first three ruled out = %d, want ≥ %d", got, taken+4)
	}
}

func TestNextGivesUp(t *testing.T) {
	_, err := Next(20000, func(int) bool { return true })
	if !errors.Is(err, ErrNoFreePort) || !strings.Contains(err.Error(), "20000 to 20099") {
		t.Errorf("err = %v, want ErrNoFreePort naming the range", err)
	}
	// Near the top it stops at 65535 instead of running past it.
	if _, err := Next(65500, func(int) bool { return true }); err == nil || !strings.Contains(err.Error(), "to 65535") {
		t.Errorf("err = %v, want the range to end at 65535", err)
	}
}

func TestWhoFindsTheListener(t *testing.T) {
	port := listen(t, "127.0.0.1:0")
	began := time.Now()
	h, ok := Who(context.Background(), port)
	if !ok {
		t.Fatalf("Who(%d) found nothing; this test process is listening there", port)
	}
	if int(h.PID) != os.Getpid() || h.Name == "" {
		t.Errorf("Who(%d) = %+v, want PID %d with a name", port, h, os.Getpid())
	}
	t.Logf("Who took %v and found %s", time.Since(began).Round(time.Millisecond), h)
}

func TestWhoOnFreePort(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if h, ok := Who(context.Background(), port); ok {
		t.Errorf("Who(%d) on a free port = %+v", port, h)
	}
}

func TestHolderString(t *testing.T) {
	for _, tt := range []struct {
		h    Holder
		want string
	}{
		{Holder{PID: 412, Name: "mysqld", Exe: `C:\xampp\mysql\bin\mysqld.exe`}, "mysqld (PID 412) from XAMPP"},
		{Holder{PID: 412, Name: "mysqld", Exe: "/Applications/XAMPP/xamppfiles/sbin/mysqld"}, "mysqld (PID 412) from XAMPP"},
		{Holder{PID: 77, Name: "httpd", Exe: "/usr/sbin/httpd"}, "httpd (PID 77)"},
		{Holder{PID: 9}, "a program (PID 9)"},
	} {
		if got := tt.h.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.h, got, tt.want)
		}
	}
}

func TestListeningFindsThisProcess(t *testing.T) {
	port := listen(t, "127.0.0.1:0")
	list, err := Listening(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, l := range list {
		if i > 0 && list[i-1].Port > l.Port {
			t.Errorf("not sorted: %d before %d", list[i-1].Port, l.Port)
		}
		if l.Port == port && int(l.PID) == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Errorf("port %d (this test) isn't listed among %d listeners", port, len(list))
	}
}
