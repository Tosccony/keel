package mail

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freePort picks a free port below the range the system hands out for
// outgoing connections and ":0". Tests restart the catcher on the same
// ports, and a port from that range can be taken by another program (or
// another test package) in between.
func freePort(t *testing.T) int {
	t.Helper()
	for range 100 {
		port := 20000 + rand.IntN(12000) // ephemeral ports start at 32768 (Linux) or 49152 (macOS, Windows)
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			l.Close()
			return port
		}
	}
	t.Fatal("no free port")
	return 0
}

func startCatcher(t *testing.T) *Catcher {
	t.Helper()
	c := &Catcher{Dir: t.TempDir(), SMTPPort: freePort(t), WebPort: freePort(t)}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Stop(context.Background()) })
	return c
}

func (c *Catcher) addr() string { return "127.0.0.1:" + strconv.Itoa(c.SMTPPort) }

func send(t *testing.T, c *Catcher, from string, to []string, msg string) {
	t.Helper()
	if err := smtp.SendMail(c.addr(), nil, from, to, []byte(strings.ReplaceAll(msg, "\n", "\r\n"))); err != nil {
		t.Fatalf("SendMail: %v", err)
	}
}

func only(t *testing.T, c *Catcher) *Message {
	t.Helper()
	list, err := c.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("inbox: %d messages (%v), want 1", len(list), err)
	}
	m, err := c.Read(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// What PHP's mail() sends: plain text, a Bcc that only the envelope knows.
func TestCatchesPlainMail(t *testing.T) {
	c := startCatcher(t)
	send(t, c, "shop@example.test", []string{"student@example.test", "teacher@example.test"}, `From: Shop <shop@example.test>
To: student@example.test
Subject: Your order
Date: Mon, 02 Nov 2026 10:00:00 -0400

Thanks for your order.
.This line started with a dot.
`)
	m := only(t, c)
	if m.From != "Shop <shop@example.test>" || m.Subject != "Your order" || len(m.To) != 1 || m.To[0] != "student@example.test" {
		t.Errorf("summary: %+v", m.Summary)
	}
	if !strings.Contains(m.Text, "Thanks for your order.") || !strings.Contains(m.Text, "\n.This line started with a dot.") {
		t.Errorf("text (dot-stuffing must be undone): %q", m.Text)
	}
	if len(m.Bcc) != 1 || m.Bcc[0] != "teacher@example.test" {
		t.Errorf("Bcc from the envelope: %v", m.Bcc)
	}
	if m.Date.Year() != 2026 || m.HasHTML || len(m.Attachments) != 0 {
		t.Errorf("date %v, html %v, attachments %v", m.Date, m.HasHTML, m.Attachments)
	}
}

// What PHPMailer sends: an encoded subject, HTML with a text alternative
// (quoted-printable), and a base64 attachment, with a login.
func TestCatchesHTMLMailWithAttachment(t *testing.T) {
	c := startCatcher(t)
	cl, err := smtp.Dial(c.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if err := cl.Hello("student-laptop"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := cl.Extension("AUTH"); !ok {
		t.Fatal("AUTH should be offered")
	}
	if err := cl.Auth(smtp.PlainAuth("", "any", "thing", "127.0.0.1")); err != nil {
		t.Fatalf("login: %v", err)
	}
	cl.Mail("app@example.test")
	cl.Rcpt("student@example.test")
	w, _ := cl.Data()
	fmt.Fprint(w, strings.ReplaceAll(`From: app@example.test
To: =?UTF-8?B?w4lsb8Ovc2U=?= <student@example.test>
Subject: =?UTF-8?Q?R=C3=A9initialiser_votre_mot_de_passe?=
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/alternative; boundary="inner"

--inner
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: quoted-printable

Cliquez ici pour r=C3=A9initialiser.
--inner
Content-Type: text/html; charset=utf-8
Content-Transfer-Encoding: quoted-printable

<p>Cliquez <a href=3D"http://localhost/reset">ici</a></p>
--inner--
--outer
Content-Type: application/pdf; name="facture.pdf"
Content-Disposition: attachment; filename="facture.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQK
JSVFT0YK
--outer--
`, "\n", "\r\n"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	cl.Quit()

	m := only(t, c)
	if m.Subject != "Réinitialiser votre mot de passe" || m.To[0] != "Éloïse <student@example.test>" {
		t.Errorf("decoded headers: subject %q, to %v", m.Subject, m.To)
	}
	if !strings.Contains(m.Text, "réinitialiser") || !m.HasHTML {
		t.Errorf("text %q, html %v", m.Text, m.HasHTML)
	}
	if html, _ := c.HTML(m.ID); !strings.Contains(html, `<a href="http://localhost/reset">`) {
		t.Errorf("html: %q", html)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Name != "facture.pdf" {
		t.Fatalf("attachments: %+v", m.Attachments)
	}
	ctype, body, name, err := c.Attachment(m.ID, 0)
	if err != nil || ctype != "application/pdf" || name != "facture.pdf" || string(body) != "%PDF-1.4\n%%EOF\n" {
		t.Errorf("attachment: %s %s %q %v", ctype, name, body, err)
	}
}

// Speaks raw SMTP for the things net/smtp won't do.
func TestSMTPEdgeCases(t *testing.T) {
	c := startCatcher(t)
	conn, err := net.Dial("tcp", c.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	expect := func(code string) {
		t.Helper()
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("waiting for %s: %v", code, err)
			}
			if len(line) >= 4 && line[3] == '-' {
				continue // more lines of the same reply
			}
			if !strings.HasPrefix(line, code) {
				t.Fatalf("got %q, want %s", strings.TrimSpace(line), code)
			}
			return
		}
	}
	say := func(line string) { fmt.Fprintf(conn, "%s\r\n", line) }

	expect("220")
	say("HELO old-client")
	expect("250")
	say("DATA") // before any recipient
	expect("503")
	say("MAIL FROM:<>") // empty sender, like a bounce
	expect("250")
	say("RCPT TO:<a@example.test>")
	expect("250")
	say("RSET") // forget it
	expect("250")
	say("DATA")
	expect("503")
	say("STARTTLS")
	expect("454")
	say("XYZZY")
	expect("500")
	say("AUTH LOGIN")
	expect("334")
	say("dXNlcg==")
	expect("334")
	say("cGFzcw==")
	expect("235")
	say("MAIL FROM:<x@example.test>")
	expect("250")
	say("RCPT TO:<y@example.test>")
	expect("250")
	say("DATA")
	expect("354")
	say("Subject: " + strings.Repeat("long ", 2000)) // longer than the read buffer
	say("")
	say("..starts with two dots")
	say(".")
	expect("250")
	say("QUIT")
	expect("221")

	m := only(t, c)
	if !strings.Contains(m.Text, ".starts with two dots") || strings.Contains(m.Text, "..starts") {
		t.Errorf("text: %q", m.Text)
	}
	if !strings.HasPrefix(m.Subject, "long long") {
		t.Errorf("a subject longer than the read buffer was cut: %.40q", m.Subject)
	}
}

func TestOldMessagesArePruned(t *testing.T) {
	c := startCatcher(t)
	c.Keep = 3
	for i := range 5 {
		send(t, c, "a@example.test", []string{"b@example.test"}, fmt.Sprintf("Subject: number %d\n\nhi\n", i))
		time.Sleep(time.Millisecond) // distinct IDs, in order
	}
	list, _ := c.List()
	if len(list) != 3 || list[0].Subject != "number 4" || list[2].Subject != "number 2" {
		var subjects []string
		for _, s := range list {
			subjects = append(subjects, s.Subject)
		}
		t.Errorf("kept %v, want the 3 newest, newest first", subjects)
	}
}

func TestInboxPageAndAPI(t *testing.T) {
	c := startCatcher(t)
	send(t, c, "a@example.test", []string{"b@example.test"}, "Subject: Hello\nContent-Type: text/html\n\n<script>alert(1)</script><p>Hi</p>\n")
	srv := httptest.NewServer(c.handler())
	defer srv.Close()
	get := func(path string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Host = "localhost:" + strconv.Itoa(c.WebPort)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	if resp := get("/"); resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("inbox page: %s", resp.Status)
	}
	var list []Summary
	json.NewDecoder(get("/api/messages").Body).Decode(&list)
	if len(list) != 1 || list[0].Subject != "Hello" {
		t.Fatalf("list: %+v", list)
	}
	html := get("/api/messages/" + list[0].ID + "/html")
	if csp := html.Header.Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("the email's HTML must be sandboxed, CSP = %q", csp)
	}
	var info map[string]int
	json.NewDecoder(get("/api/info").Body).Decode(&info)
	if info["smtpPort"] != c.SMTPPort {
		t.Errorf("info: %v", info)
	}
	if resp := get("/api/messages/../../etc/passwd"); resp.StatusCode == 200 {
		t.Error("a path outside the inbox was served")
	}
	if resp := get("/api/messages/00000000000000000001-deadbeef"); resp.StatusCode != 404 {
		t.Errorf("a missing message: %s", resp.Status)
	}

	// A website that rebinds its own name to 127.0.0.1 gets nothing.
	req, _ := http.NewRequest("GET", srv.URL+"/api/messages", nil)
	req.Host = "evil.example:8025"
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a foreign Host header: %s, want 403", resp.Status)
	}

	req, _ = http.NewRequest("DELETE", srv.URL+"/api/messages", nil)
	req.Host = "127.0.0.1"
	http.DefaultClient.Do(req)
	if list, _ := c.List(); len(list) != 0 {
		t.Errorf("after Delete all: %d messages", len(list))
	}
}

func TestStartAndStop(t *testing.T) {
	c := startCatcher(t)
	if !c.Running() {
		t.Fatal("not running after Start")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Running() {
		t.Error("still running after Stop")
	}
	if _, err := net.DialTimeout("tcp", c.addr(), time.Second); err == nil {
		t.Error("the SMTP port still answers after Stop")
	}
	// Restartable, and messages survive.
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	send(t, c, "a@example.test", []string{"b@example.test"}, "Subject: again\n\nhi\n")

	// A port another program holds: Start fails and holds nothing.
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	other := &Catcher{Dir: t.TempDir(), SMTPPort: freePort(t), WebPort: busy.Addr().(*net.TCPAddr).Port}
	if err := other.Start(); err == nil {
		t.Error("Start with a taken port succeeded")
	}
	if other.Running() {
		t.Error("a failed Start left the catcher running")
	}
	if l, err := net.Listen("tcp", other.addr()); err != nil {
		t.Errorf("a failed Start kept the SMTP port: %v", err)
	} else {
		l.Close()
	}
}

func TestUnreadableDirIsAnError(t *testing.T) {
	file := t.TempDir() + "/not-a-folder"
	os.WriteFile(file, nil, 0o644)
	c := &Catcher{Dir: file, SMTPPort: freePort(t), WebPort: freePort(t)}
	if err := c.Start(); err == nil {
		c.Stop(context.Background())
		t.Error("Start with a file where the folder should be succeeded")
	}
}

var _ = io.Discard

// What PHP's mail() does on macOS and Linux: run sendmail -t -i with the
// message on its input.
func TestSendmailStandIn(t *testing.T) {
	c := startCatcher(t)
	msg := "From: Shop <shop@example.test>\nTo: student@example.test\nCc: ta@example.test\nBcc: teacher@example.test,\n  principal@example.test\nSubject: Receipt\n\nThanks!\n"
	if err := Sendmail(strings.NewReader(msg), []string{"-t", "-i"}, c.SMTPPort); err != nil {
		t.Fatal(err)
	}
	m := only(t, c)
	if m.Subject != "Receipt" || !strings.Contains(m.Text, "Thanks!") {
		t.Errorf("message: %+v", m.Summary)
	}
	raw, _ := c.Raw(m.ID)
	if strings.Contains(string(raw), "Bcc:") || strings.Contains(string(raw), "principal@example.test\r\nSubject") {
		t.Errorf("the Bcc header should be removed:\n%s", raw)
	}
	if len(m.Bcc) != 2 || m.Bcc[0] != "teacher@example.test" || m.Bcc[1] != "principal@example.test" {
		t.Errorf("Bcc from the envelope: %v", m.Bcc)
	}
	if !strings.Contains(string(raw), "X-Keel-Envelope-From: <shop@example.test>") {
		t.Errorf("sender should come from the From header:\n%.300s", raw)
	}

	// Recipients on the command line, and -f.
	c.DeleteAll()
	if err := Sendmail(strings.NewReader("Subject: hi\n\nyo\n"), []string{"-i", "-fapp@example.test", "a@example.test"}, c.SMTPPort); err != nil {
		t.Fatal(err)
	}
	raw, _ = c.Raw(only(t, c).ID)
	if !strings.Contains(string(raw), "X-Keel-Envelope-From: <app@example.test>") || !strings.Contains(string(raw), "X-Keel-Envelope-To: a@example.test") {
		t.Errorf("envelope:\n%.300s", raw)
	}

	if err := Sendmail(strings.NewReader("Subject: nobody\n\nx\n"), []string{"-i"}, c.SMTPPort); err == nil {
		t.Error("a message with no recipients was sent")
	}
	c.Stop(context.Background())
	if err := Sendmail(strings.NewReader("To: a@example.test\n\nx\n"), []string{"-t"}, c.SMTPPort); err == nil || !strings.Contains(err.Error(), "is it running in Keel?") {
		t.Errorf("with the catcher stopped: %v", err)
	}
}
