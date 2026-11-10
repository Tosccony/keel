package mail

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
)

// Sendmail stands in for the sendmail program that PHP's mail() runs on
// macOS and Linux. It reads one message from r, as sendmail does, and
// hands it to the mail catcher on port.
//
// args are sendmail's: "-t" takes the recipients from the To, Cc and Bcc
// headers, "-f address" sets the sender, other flags (like "-i") are
// accepted and ignored, and anything else is a recipient. The Bcc header
// is removed before sending, as sendmail does.
func Sendmail(r io.Reader, args []string, port int) error {
	var rcpts []string
	fromHeaders := false
	sender := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t":
			fromHeaders = true
		case a == "-f" && i+1 < len(args):
			i++
			sender = args[i]
		case strings.HasPrefix(a, "-f") && len(a) > 2:
			sender = a[2:]
		case a == "-F" || a == "-o" || a == "-O":
			i++ // these take a value Keel doesn't need
		case strings.HasPrefix(a, "-"):
		default:
			rcpts = append(rcpts, a)
		}
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("the message has no headers: %w", err)
	}
	if fromHeaders {
		for _, h := range []string{"To", "Cc", "Bcc"} {
			if list, err := msg.Header.AddressList(h); err == nil {
				for _, a := range list {
					rcpts = append(rcpts, a.Address)
				}
			}
		}
	}
	if len(rcpts) == 0 {
		return errors.New("no recipients")
	}
	if sender == "" {
		if from, err := mail.ParseAddress(msg.Header.Get("From")); err == nil {
			sender = from.Address
		} else {
			sender = "php@localhost"
		}
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	if err := smtp.SendMail(addr, nil, sender, rcpts, withoutBcc(raw)); err != nil {
		return fmt.Errorf("the mail catcher isn't taking mail on port %d (is it running in Keel?): %w", port, err)
	}
	return nil
}

// withoutBcc drops the Bcc header, and its continuation lines, from a
// message's headers.
func withoutBcc(raw []byte) []byte {
	var out bytes.Buffer
	sc := bufio.NewReader(bytes.NewReader(raw))
	inBcc, inHeaders := false, true
	for {
		line, err := sc.ReadString('\n')
		if inHeaders {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				inHeaders = false
			case inBcc && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
				line = "" // continuation of Bcc
			case strings.HasPrefix(strings.ToLower(trimmed), "bcc:"):
				inBcc, line = true, ""
			default:
				inBcc = false
			}
		}
		out.WriteString(line)
		if err != nil {
			return out.Bytes()
		}
	}
}
