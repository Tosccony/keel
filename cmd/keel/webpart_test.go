package main

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

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

// phpMyAdmin with a real MariaDB: logged in as root without a password,
// on MariaDB's current port. Needs the development setup and phpMyAdmin
// in dist/ (scripts/fetch-phpmyadmin.sh).
func TestPHPMyAdminReachesMariaDB(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the development web server is set up for macOS")
	}
	pma, _ := filepath.Abs(filepath.Join("..", "..", "dist", "phpmyadmin"))
	if _, err := os.Stat(filepath.Join(pma, "index.php")); err != nil {
		t.Skip("run scripts/fetch-phpmyadmin.sh first")
	}
	bin := setup.Lab().BinDir
	if _, err := os.Stat(filepath.Join(bin, "mariadb-install-db")); err != nil {
		t.Skip("needs MariaDB installed")
	}
	home, err := os.MkdirTemp("", "keel-pma-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	os.Symlink(pma, filepath.Join(home, "phpmyadmin"))
	opts := setup.Options{Home: home, BinDir: bin, Folder: filepath.Join(home, "mariadb"), Port: freeTCPPort(t)}
	st := opts.Settings()
	sites := filepath.Join(home, "htdocs")
	os.MkdirAll(sites, 0o755)
	st.SetSites(sites)
	st.SetPort(setup.PartWeb, freeTCPPort(t))
	st.SetPort(setup.PartPHP, freeTCPPort(t))

	ctx := context.Background()
	db := opts.Server()
	if _, err := opts.Prepare(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Stop(ctx) })

	webSvc := newWebService(opts)
	if webSvc.server == nil {
		t.Skip("no development web server: ", webSvc.err)
	}
	if _, err := webSvc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { webSvc.Stop(ctx) })

	code, body := httpGet(t, "http://127.0.0.1:"+strconv.Itoa(webSvc.server.Port)+"/phpmyadmin/")
	if code != 200 || !strings.Contains(body, "phpMyAdmin") {
		t.Fatalf("phpMyAdmin: %d\n%.600s", code, body)
	}
	for _, bad := range []string{"Access denied", "Cannot log in", "mysqli::real_connect"} {
		if strings.Contains(body, bad) {
			t.Errorf("phpMyAdmin couldn't log in (%q):\n%.800s", bad, body)
		}
	}
	if !strings.Contains(body, "MariaDB") {
		t.Errorf("phpMyAdmin's home page should name the MariaDB server:\n%.800s", body)
	}
	if strings.Contains(body, "Deprecated:") {
		t.Logf("PHP %s shows deprecation notices in phpMyAdmin (production ships PHP 8.4)", webSvc.php)
	}

	// A student gives root a password: phpMyAdmin now shows its login page
	// instead of XAMPP's "Access denied" error, without a restart.
	sqlDB, err := sql.Open("mysql", "root@tcp(127.0.0.1:"+strconv.Itoa(db.Port)+")/")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		if _, err := sqlDB.Exec("ALTER USER IF EXISTS 'root'@'" + host + "' IDENTIFIED BY 'class-password'"); err != nil {
			t.Fatal(err)
		}
	}
	code, body = httpGet(t, "http://127.0.0.1:"+strconv.Itoa(webSvc.server.Port)+"/phpmyadmin/")
	if code != 200 || !strings.Contains(body, "pma_password") {
		t.Errorf("phpMyAdmin with a root password: %d, no login form:\n%.800s", code, body)
	}
	if strings.Contains(body, "Access denied") {
		t.Errorf("phpMyAdmin shows the access error instead of asking:\n%.800s", body)
	}
}
