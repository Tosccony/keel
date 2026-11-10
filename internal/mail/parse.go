package mail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// Summary is one line of the inbox.
type Summary struct {
	ID      string    `json:"id"`
	From    string    `json:"from"`
	To      []string  `json:"to"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Size    int64     `json:"size"`
}

// Message is one email, decoded for showing.
type Message struct {
	Summary
	Cc          []string     `json:"cc,omitempty"`
	Bcc         []string     `json:"bcc,omitempty"` // envelope recipients not in To or Cc
	Text        string       `json:"text"`
	HasHTML     bool         `json:"hasHtml"`
	Attachments []Attachment `json:"attachments"`
	Headers     []Header     `json:"headers"`

	html  string
	parts []part // attachment bodies, by Attachment.Index
}

// Attachment describes an attached file.
type Attachment struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int    `json:"size"`
}

// Header is one header line, decoded.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type part struct {
	contentType string
	body        []byte
}

var decoder = mime.WordDecoder{CharsetReader: charsetReader}

// List returns the inbox, newest first, reading only each message's headers.
func (c *Catcher) List() ([]Summary, error) {
	ids, err := c.ids()
	if err != nil {
		return nil, err
	}
	list := make([]Summary, 0, len(ids))
	for _, id := range ids {
		s, err := c.summary(id)
		if err != nil {
			continue // deleted meanwhile
		}
		list = append(list, s)
	}
	return list, nil
}

func (c *Catcher) summary(id string) (Summary, error) {
	p, err := c.path(id)
	if err != nil {
		return Summary{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		return Summary{}, err
	}
	defer f.Close()
	info, _ := f.Stat()
	h, err := textproto.NewReader(bufio.NewReader(f)).ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return Summary{}, err
	}
	return summarize(id, mail.Header(h), info.Size()), nil
}

func summarize(id string, h mail.Header, size int64) Summary {
	s := Summary{ID: id, Size: size, From: decodeWords(h.Get("From")), Subject: decodeWords(h.Get("Subject"))}
	s.To = addresses(h, "To")
	if d, err := h.Date(); err == nil {
		s.Date = d
	} else {
		s.Date = receivedAt(id)
	}
	return s
}

// receivedAt reads the time from an ID, for messages without a Date.
func receivedAt(id string) time.Time {
	var nanos int64
	fmt.Sscan(strings.SplitN(id, "-", 2)[0], &nanos)
	return time.Unix(0, nanos)
}

// Read decodes a whole message.
func (c *Catcher) Read(id string) (*Message, error) {
	p, err := c.path(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("mail: can't read message %s: %w", id, err)
	}
	msg := &Message{Summary: summarize(id, m.Header, int64(len(raw)))}
	msg.Cc = addresses(m.Header, "Cc")
	msg.Bcc = hiddenRecipients(m.Header)
	for name, values := range m.Header {
		for _, v := range values {
			msg.Headers = append(msg.Headers, Header{Name: name, Value: decodeWords(v)})
		}
	}
	sortHeaders(msg.Headers)
	msg.walk(textproto.MIMEHeader(m.Header), m.Body, 0)
	msg.HasHTML = msg.html != ""
	if msg.Attachments == nil {
		msg.Attachments = []Attachment{}
	}
	return msg, nil
}

// HTML returns a message's HTML part, or "" if it has none.
func (c *Catcher) HTML(id string) (string, error) {
	m, err := c.Read(id)
	if err != nil {
		return "", err
	}
	return m.html, nil
}

// Attachment returns an attached file's type and contents.
func (c *Catcher) Attachment(id string, index int) (contentType string, body []byte, name string, err error) {
	m, err := c.Read(id)
	if err != nil {
		return "", nil, "", err
	}
	if index < 0 || index >= len(m.parts) {
		return "", nil, "", fmt.Errorf("mail: message %s has no attachment %d", id, index)
	}
	return m.parts[index].contentType, m.parts[index].body, m.Attachments[index].Name, nil
}

// Raw returns the message exactly as received.
func (c *Catcher) Raw(id string) ([]byte, error) {
	p, err := c.path(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// walk goes through a MIME part, keeping the first text and HTML bodies
// and every attachment. Nested multiparts (mixed, alternative, related)
// are followed up to a sane depth.
func (m *Message) walk(h textproto.MIMEHeader, body io.Reader, depth int) {
	ctype, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		ctype, params = "text/plain", map[string]string{"charset": "utf-8"}
	}
	if strings.HasPrefix(ctype, "multipart/") && depth < 10 {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err != nil {
				return
			}
			m.walk(p.Header, p, depth+1)
		}
	}
	data, err := io.ReadAll(transferDecoder(h.Get("Content-Transfer-Encoding"), body))
	if err != nil && len(data) == 0 {
		return
	}
	disposition, dparams, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	name = decodeWords(name)
	switch {
	case disposition != "attachment" && ctype == "text/plain" && m.Text == "":
		m.Text = toUTF8(data, params["charset"])
	case disposition != "attachment" && ctype == "text/html" && m.html == "":
		m.html = toUTF8(data, params["charset"])
	default:
		if name == "" {
			name = "attachment"
		}
		m.Attachments = append(m.Attachments, Attachment{Index: len(m.parts), Name: name, Type: ctype, Size: len(data)})
		m.parts = append(m.parts, part{contentType: ctype, body: data})
	}
}

func transferDecoder(encoding string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, newlineStripper{r})
	}
	return r
}

// newlineStripper drops the line breaks base64 bodies are wrapped with.
type newlineStripper struct{ r io.Reader }

func (n newlineStripper) Read(p []byte) (int, error) {
	k, err := n.r.Read(p)
	j := 0
	for _, b := range p[:k] {
		if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
			p[j] = b
			j++
		}
	}
	return j, err
}

// toUTF8 converts text in the common charsets to UTF-8. Anything else
// that isn't valid UTF-8 is shown as Latin-1, which at least never fails.
func toUTF8(b []byte, charset string) string {
	switch strings.ToLower(charset) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		if utf8.Valid(b) {
			return string(b)
		}
	}
	return latin1(b)
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(toUTF8(b, charset)), nil
}

func decodeWords(s string) string {
	if d, err := decoder.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// addresses decodes an address header, falling back to the raw text when
// it doesn't parse (students' test emails often have odd headers).
func addresses(h mail.Header, key string) []string {
	v := h.Get(key)
	if v == "" {
		return nil
	}
	list, err := (&mail.AddressParser{WordDecoder: &decoder}).ParseList(v)
	if err != nil {
		return []string{decodeWords(v)}
	}
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.Address
		if a.Name != "" { // for reading, not sending: no quoting or re-encoding
			out[i] = a.Name + " <" + a.Address + ">"
		}
	}
	return out
}

// hiddenRecipients are the envelope recipients missing from To and Cc:
// the Bcc addresses.
func hiddenRecipients(h mail.Header) []string {
	shown := strings.ToLower(h.Get("To") + "," + h.Get("Cc"))
	var hidden []string
	for _, a := range strings.Split(h.Get("X-Keel-Envelope-To"), ",") {
		a = strings.TrimSpace(a)
		if a != "" && !strings.Contains(shown, strings.ToLower(a)) {
			hidden = append(hidden, a)
		}
	}
	return hidden
}

// sortHeaders puts the usual headers first, the rest by name.
func sortHeaders(h []Header) {
	order := map[string]int{"From": 1, "To": 2, "Cc": 3, "Subject": 4, "Date": 5}
	rank := func(name string) int {
		if r, ok := order[name]; ok {
			return r
		}
		return 100
	}
	for i := 1; i < len(h); i++ {
		for j := i; j > 0; j-- {
			a, b := h[j-1], h[j]
			if rank(a.Name) < rank(b.Name) || (rank(a.Name) == rank(b.Name) && a.Name <= b.Name) {
				break
			}
			h[j-1], h[j] = b, a
		}
	}
}
