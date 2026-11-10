package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Tosccony/keel/internal/ports"
)

// The header's tools: a terminal ready for MariaDB, PHP and Perl (XAMPP's
// Shell button), and the ports in use (XAMPP's Netstat button).

// showPorts lists every listening port and its program, marking Keel's own
// parts. The list is read in the background: macOS's takes a moment.
func (u *ui) showPorts() {
	status := widget.NewLabel("Looking…")
	rows := container.NewVBox(status)
	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(560, 320))
	d := dialog.NewCustom("Ports in use", "Close", scroll, u.win)
	d.Resize(fyne.NewSize(620, 440))
	d.Show()

	ours := u.ourPorts()
	go func() {
		list, err := ports.Listening(context.Background())
		fyne.Do(func() {
			rows.Objects = nil
			if err != nil {
				rows.Add(widget.NewLabel("Couldn't list the ports: " + err.Error()))
				rows.Refresh()
				return
			}
			grid := container.NewGridWithColumns(4)
			for _, h := range []string{"Port", "Program", "Listening on", "In Keel"} {
				l := widget.NewLabel(h)
				l.TextStyle.Bold = true
				grid.Add(l)
			}
			for _, l := range list {
				name := l.Name
				if name == "" {
					name = "(another user's program)"
				}
				if l.PID != 0 {
					name += fmt.Sprintf(" (PID %d)", l.PID)
				}
				grid.Add(widget.NewLabel(strconv.Itoa(l.Port)))
				grid.Add(widget.NewLabel(name))
				grid.Add(muted(l.Addr))
				part := widget.NewLabel(ours[l.Port])
				part.Importance = widget.SuccessImportance
				grid.Add(part)
			}
			rows.Add(grid)
			rows.Add(muted(fmt.Sprintf("%d listening ports. Keel's parts move to the next free port if another program has theirs.", len(list))))
			rows.Refresh()
		})
	}()
}

// ourPorts maps the ports Keel's parts are set to use to their names.
func (u *ui) ourPorts() map[int]string {
	m := map[int]string{u.port: "MariaDB"}
	for _, p := range u.parts {
		switch s := p.svc.(type) {
		case *webService:
			if s.server != nil {
				m[s.server.Port] = "Web server"
				if s.server.PHPFPM != "" {
					m[s.server.FPMPort] = "PHP"
				}
			}
		case *mailService:
			m[s.catcher.SMTPPort] = "Mail catcher"
			m[s.catcher.WebPort] = "Mail inbox page"
		case *ftpService:
			m[s.server.Port] = "FTP server"
		case *tomcatService:
			m[s.server.Port] = "Tomcat"
		}
	}
	return m
}

// openTerminal opens a terminal in the websites folder with Keel's
// programs (mariadb, php, perl) first on its PATH.
func (u *ui) openTerminal() {
	dir := u.sites
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	var paths []string
	if u.toolPaths != nil {
		paths = u.toolPaths()
	}
	if err := startTerminal(dir, paths); err != nil {
		u.log(bad, "Couldn't open a terminal: "+err.Error())
	}
}

func startTerminal(dir string, paths []string) error {
	path := strings.Join(append(paths, os.Getenv("PATH")), string(os.PathListSeparator))
	switch runtime.GOOS {
	case "darwin":
		// Terminal opens .command files; this one sets the PATH and folder.
		script := filepath.Join(os.TempDir(), "keel-terminal.command")
		body := fmt.Sprintf("#!/bin/sh\nexport PATH=%s\ncd %s || exit 1\nclear\necho \"Keel: mariadb, php and perl are ready in this terminal.\"\nexec \"${SHELL:-/bin/zsh}\" -i\n",
			shellQuote(path), shellQuote(dir))
		if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
			return err
		}
		return exec.Command("open", "-a", "Terminal", script).Start()
	case "windows":
		cmd := exec.Command("cmd", "/C", "start", "Keel", "cmd", "/K", "echo Keel: mariadb, php and perl are ready in this window.")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+path)
		return cmd.Start()
	default:
		for _, term := range [][]string{
			{"x-terminal-emulator"}, {"gnome-terminal"}, {"konsole"}, {"xfce4-terminal"}, {"xterm"},
		} {
			if p, err := exec.LookPath(term[0]); err == nil {
				cmd := exec.Command(p)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "PATH="+path)
				return cmd.Start()
			}
		}
		return fmt.Errorf("no terminal program found")
	}
}

// shellQuote quotes s for sh.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// sortedKeys is for tests.
func sortedKeys(m map[int]string) []int {
	var keys []int
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
