// Package proc starts Keel's helper servers (Apache, PHP-FPM, Tomcat) as
// processes of their own, finds them again after Keel restarts, and stops
// them.
//
// Like MariaDB, they keep running when Keel's window closes: each gets its
// own process group, and writes to a log file instead of a pipe, so
// nothing depends on Keel staying alive. Unlike MariaDB, they hold no data,
// so Stop may force them once asking hasn't worked.
package proc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Process describes one server process.
type Process struct {
	Path    string   // the program
	Args    []string // its arguments
	Env     []string // extra environment variables, KEY=value
	Dir     string   // working folder ("" for Keel's)
	LogFile string   // where its output goes
	PIDFile string   // where its PID is kept; Keel writes it if the server doesn't

	// Marker is text in the process's command line that only Keel's copy
	// has, such as its config file's path. A PID from PIDFile is only
	// believed if the process's command line contains it: after a crash,
	// the number may belong to another program.
	Marker string

	cmd    *exec.Cmd
	exited chan struct{}
	err    error
}

// ErrNotRunning means there's no process to stop.
var ErrNotRunning = errors.New("not running")

// Start launches the process and records its PID.
func (p *Process) Start() error {
	logf, err := os.OpenFile(p.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logf.Close() // the process keeps its own handle

	// exec.Command, not CommandContext, which kills on cancel.
	cmd := exec.Command(p.Path, p.Args...)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), p.Env...)
	cmd.Stdout, cmd.Stderr = logf, logf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", p.Path, err)
	}
	p.cmd, p.exited = cmd, make(chan struct{})
	go func() {
		p.err = cmd.Wait()
		close(p.exited)
	}()
	if p.PIDFile != "" {
		os.WriteFile(p.PIDFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)
	}
	return nil
}

// Exited is closed when a process this Process started exits; nil if it
// didn't start one.
func (p *Process) Exited() <-chan struct{} { return p.exited }

// ExitErr is how the process exited; only valid once Exited is closed.
func (p *Process) ExitErr() error { return p.err }

// PID returns the running process's ID, or 0. It finds a process started
// by an earlier Keel through PIDFile.
func (p *Process) PID() int {
	if p.cmd != nil {
		select {
		case <-p.exited:
		default:
			return p.cmd.Process.Pid
		}
	}
	b, err := os.ReadFile(p.PIDFile)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if p.isOurs(pid) {
		return pid
	}
	return 0
}

func (p *Process) isOurs(pid int) bool {
	if pid <= 0 {
		return false
	}
	pr, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	cmdline, err := pr.Cmdline()
	return err == nil && strings.Contains(cmdline, p.Marker)
}

// Stop asks the process to stop, waits up to timeout, then forces it.
func (p *Process) Stop(ctx context.Context, timeout time.Duration) error {
	pid := p.PID()
	if pid == 0 {
		return ErrNotRunning
	}
	if err := ask(pid); err != nil && !p.wait(ctx, pid, 0) {
		return fmt.Errorf("ask PID %d to stop: %w", pid, err)
	}
	if p.wait(ctx, pid, timeout) {
		os.Remove(p.PIDFile)
		return nil
	}
	if err := force(pid); err != nil {
		return fmt.Errorf("force PID %d to stop: %w", pid, err)
	}
	if !p.wait(ctx, pid, 5*time.Second) {
		return fmt.Errorf("PID %d is still running", pid)
	}
	os.Remove(p.PIDFile)
	return nil
}

// wait waits up to timeout for pid to go.
func (p *Process) wait(ctx context.Context, pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.cmd != nil && p.cmd.Process.Pid == pid {
			select {
			case <-p.exited:
				return true
			default:
			}
		} else if !p.isOurs(pid) {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
