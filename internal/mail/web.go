package mail

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
)

//go:embed inbox.html
var inboxPage []byte

// handler serves the inbox page and its JSON API.
func (c *Catcher) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-src 'self'; img-src 'self' data:")
		w.Write(inboxPage)
	})
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]int{"smtpPort": c.SMTPPort}, nil)
	})
	mux.HandleFunc("GET /api/messages", func(w http.ResponseWriter, r *http.Request) {
		list, err := c.List()
		reply(w, list, err)
	})
	mux.HandleFunc("DELETE /api/messages", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, c.DeleteAll())
	})
	mux.HandleFunc("GET /api/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		m, err := c.Read(r.PathValue("id"))
		reply(w, m, err)
	})
	mux.HandleFunc("DELETE /api/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, c.Delete(r.PathValue("id")))
	})
	mux.HandleFunc("GET /api/messages/{id}/raw", func(w http.ResponseWriter, r *http.Request) {
		raw, err := c.Raw(r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(raw)
	})
	mux.HandleFunc("GET /api/messages/{id}/html", func(w http.ResponseWriter, r *http.Request) {
		html, err := c.HTML(r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// The email's HTML runs in a sandbox: no scripts, no forms, and no
		// way to reach the inbox's API. Images and styles still show.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data: http: https:; style-src 'unsafe-inline' http: https:; font-src data: http: https:")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write([]byte(html))
	})
	mux.HandleFunc("GET /api/messages/{id}/attachments/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			http.Error(w, "bad attachment number", http.StatusBadRequest)
			return
		}
		ctype, body, name, err := c.Attachment(r.PathValue("id"), n)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(body)
	})
	return localOnly(mux)
}

// localOnly refuses requests whose Host isn't this computer. A website
// open in the browser could otherwise point its own domain at 127.0.0.1
// (DNS rebinding) and read the inbox.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		switch strings.ToLower(strings.Trim(host, "[]")) {
		case "localhost", "127.0.0.1", "::1":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "The mail catcher only answers to localhost.", http.StatusForbidden)
		}
	})
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprint(err)})
		return
	}
	json.NewEncoder(w).Encode(v)
}
