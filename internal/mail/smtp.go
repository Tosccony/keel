package mail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"
)

// The SMTP side: just enough of RFC 5321 to receive mail from PHP's mail(),
// PHPMailer, Laravel and the like. It accepts any sender, any recipient
// and any login, because nothing it receives is ever delivered.

const (
	commandTimeout = 5 * time.Minute
	maxLine        = 4096
)

func (c *Catcher) serveSMTP(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return // closed by Stop
		}
		c.conns.Add(1)
		go func() {
			defer c.conns.Done()
			c.session(conn)
		}()
	}
}

// session talks to one client.
func (c *Catcher) session(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReaderSize(conn, maxLine)
	w := bufio.NewWriter(conn)
	reply := func(format string, args ...any) {
		fmt.Fprintf(w, format+"\r\n", args...)
		w.Flush()
	}

	var helo, from string
	var to []string
	reset := func() { from, to = "", nil }

	reply("220 localhost Keel mail catcher ready")
	for {
		conn.SetDeadline(time.Now().Add(commandTimeout))
		line, err := readLine(r)
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			helo = arg
			reset()
			reply("250-localhost greets %s", arg)
			reply("250-8BITMIME")
			reply("250-SMTPUTF8")
			reply("250-SIZE %d", MaxSize)
			reply("250 AUTH PLAIN LOGIN")
		case "HELO":
			helo = arg
			reset()
			reply("250 localhost")
		case "AUTH":
			c.auth(r, reply, arg)
		case "MAIL":
			addr, ok := address(arg, "FROM:")
			if !ok {
				reply("501 Syntax: MAIL FROM:<address>")
				continue
			}
			reset()
			from = addr
			reply("250 OK")
		case "RCPT":
			addr, ok := address(arg, "TO:")
			switch {
			case !ok:
				reply("501 Syntax: RCPT TO:<address>")
			case len(to) >= 1000:
				reply("452 Too many recipients")
			default:
				to = append(to, addr)
				reply("250 OK")
			}
		case "DATA":
			if len(to) == 0 {
				reply("503 Need RCPT first")
				continue
			}
			reply("354 End data with <CR><LF>.<CR><LF>")
			body, tooBig, err := readData(r)
			if err != nil {
				return
			}
			if tooBig {
				reply("552 Message bigger than %d bytes", MaxSize)
				reset()
				continue
			}
			id, err := c.save(envelope(helo, from, to, conn.RemoteAddr(), body))
			if err != nil {
				reply("451 Couldn't save the message: %v", err)
			} else {
				reply("250 OK: caught as %s", id)
			}
			reset()
		case "RSET":
			reset()
			reply("250 OK")
		case "NOOP":
			reply("250 OK")
		case "VRFY":
			reply("252 Send some mail and see")
		case "QUIT":
			reply("221 Bye")
			return
		case "STARTTLS":
			reply("454 TLS isn't offered: the mail catcher only accepts mail from this computer")
		default:
			reply("500 Unknown command")
		}
	}
}

// auth accepts any login. PHPMailer and Laravel send one when they're
// set up for a real mail server; refusing would only break the student's
// app.
func (c *Catcher) auth(r *bufio.Reader, reply func(string, ...any), arg string) {
	mech, initial, _ := strings.Cut(arg, " ")
	switch strings.ToUpper(mech) {
	case "PLAIN":
		if initial == "" {
			reply("334 ")
			if _, err := readLine(r); err != nil {
				return
			}
		}
		reply("235 Logged in")
	case "LOGIN":
		if initial == "" {
			reply("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
			if _, err := readLine(r); err != nil {
				return
			}
		}
		reply("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
		if _, err := readLine(r); err != nil {
			return
		}
		reply("235 Logged in")
	default:
		reply("504 Use PLAIN or LOGIN")
	}
}

// readLine reads one command line without its CRLF.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// address takes the <address> out of "FROM:<a@b> SIZE=123". An empty
// sender, <>, is allowed (bounces use it).
func address(arg, prefix string) (string, bool) {
	if len(arg) < len(prefix) || !strings.EqualFold(arg[:len(prefix)], prefix) {
		return "", false
	}
	rest := strings.TrimSpace(arg[len(prefix):])
	start, end := strings.IndexByte(rest, '<'), strings.IndexByte(rest, '>')
	if start != 0 || end < start {
		if rest != "" && !strings.ContainsAny(rest, " <>") { // a bare address
			return rest, true
		}
		return "", false
	}
	return rest[1:end], true
}

// readData reads the message after DATA up to the lone ".", undoing dot
// stuffing. Past MaxSize it keeps reading, so the reply comes at the right
// point, but drops the text.
func readData(r *bufio.Reader) (body []byte, tooBig bool, err error) {
	var b bytes.Buffer
	start := true // at the start of a line, not partway through a long one
	for {
		line, err := r.ReadSlice('\n')
		partial := err == bufio.ErrBufferFull
		if err != nil && !partial {
			return nil, false, err
		}
		if start && !partial && (bytes.Equal(line, []byte(".\r\n")) || bytes.Equal(line, []byte(".\n"))) {
			return b.Bytes(), tooBig, nil
		}
		if start && len(line) > 0 && line[0] == '.' {
			line = line[1:]
		}
		if !tooBig {
			b.Write(line) // copies: line is only valid until the next read
			tooBig = b.Len() > MaxSize
		}
		start = !partial
	}
}

// envelope adds a Received line and the envelope recipients, which show
// Bcc addresses that aren't in the message's own headers.
func envelope(helo, from string, to []string, remote net.Addr, body []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Received: from %s (%s) by Keel mail catcher; %s\r\n",
		helo, remote, time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "X-Keel-Envelope-From: <%s>\r\n", from)
	fmt.Fprintf(&b, "X-Keel-Envelope-To: %s\r\n", strings.Join(to, ", "))
	b.Write(body)
	return b.Bytes()
}
