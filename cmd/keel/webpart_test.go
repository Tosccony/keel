package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tosccony/keel/internal/setup"
)

// The web server and the mail catcher together, from their cards: the
// welcome page appears in an empty websites folder, and PHP's mail() lands
// in the inbox through the sendmail stand-in. Needs the macOS development
// setup (the Mac's Apache and Homebrew's PHP).
func TestWebPartServesPHPAndCatchesItsMail(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the development web server is set up for macOS")
	}
	home, err := os.MkdirTemp("", "keel-ui-") // short: Apache's runtime files have length limits
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	opts := setup.Options{Home: home, Folder: filepath.Join(home, "mariadb"), Port: 3399}
	st := opts.Settings()
	sites := filepath.Join(home, "htdocs")
	os.MkdirAll(sites, 0o755)
	st.SetSites(sites)
	for _, part := range []string{setup.PartWeb, setup.PartPHP, setup.PartMail, setup.PartMailWeb} {
		st.SetPort(part, freeTCPPort(t))
	}

	webSvc := newWebService(opts)
	if webSvc.server == nil {
		t.Skip("no development web server: ", webSvc.err)
	}
	mailSvc := newMailService(opts)
	mailPart, _ := newTestPart(t, mailSvc)
	webPart, _ := newTestPart(t, webSvc)
	mailPart.finishStart(mailPart.runStart())
	webPart.finishStart(webPart.runStart())
	t.Cleanup(func() {
		webSvc.Stop(context.Background())
		mailSvc.Stop(context.Background())
	})
	if !webPart.running {
		t.Fatalf("web server didn't start: %q", webPart.logLines())
	}
	if countContaining(webPart.logLines(), "welcome page") != 1 {
		t.Errorf("the welcome page should be mentioned: %q", webPart.logLines())
	}
	base := "http://127.0.0.1:" + strconv.Itoa(webSvc.server.Port)
	if code, body := httpGet(t, base+"/"); code != 200 || !strings.Contains(body, "Keel is running") || !strings.Contains(body, "PHP 8.") {
		t.Errorf("welcome page: %d\n%.400s", code, body)
	}

	os.WriteFile(filepath.Join(sites, "mail.php"), []byte(`<?php
var_dump(mail("student@example.test", "Hello from PHP", "Body text", "From: app@example.test"));`), 0o644)
	if code, body := httpGet(t, base+"/mail.php"); code != 200 || !strings.Contains(body, "bool(true)") {
		t.Fatalf("mail() from PHP: %d %q", code, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, _ := mailSvc.catcher.List()
		if len(list) == 1 && list[0].Subject == "Hello from PHP" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("PHP's mail didn't reach the catcher: %+v", list)
		}
		time.Sleep(100 * time.Millisecond)
	}

	webPart.finishStop(webPart.runStop())
	if webPart.running || webSvc.Running() {
		t.Error("still running after Stop")
	}
}

func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}
