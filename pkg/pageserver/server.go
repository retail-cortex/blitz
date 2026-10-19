// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package pageserver puts the desktop page in front of the service: Proxy
// forwards the page's API calls to the service's socket (the desktop app's
// web view and a Server both use it), and a Server serves the page itself
// to a browser, on a loopback port only whoever holds its link can use.
//
// The service never listens on a network port; a Server does, for the
// page, so it lets in only requests that carry its cookie, which a
// one-time link (OpenURL) hands out. Requests must also name the Server's
// own address as their host (no DNS rebinding) and, when they say where
// they come from, come from the page (no other site, not even another
// port on 127.0.0.1, whose requests a browser would send the cookie with).
//
// A Server speaks only HTTP/2, over TLS: a browser keeps at most six
// HTTP/1.1 connections to a host, and each of the page's open streams
// (a turn, its watches) held one, so a seventh request waited for one to
// end and the window froze. HTTP/2 carries them all on one connection.
// The certificate is the Server's own, made in memory as it starts and
// never written down; the window that shows the page trusts that one
// certificate by its fingerprint (Fingerprint), so a program that took
// the port after the Server stopped couldn't pose as it.
package pageserver

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TicketTTL is how long a link from OpenURL lasts unused.
const TicketTTL = time.Minute

// HostPrefix is where Options.Host answers: what the page asks of the
// program serving it (opening folders, the service's controls).
const HostPrefix = "/host/"

// Options are what a Server serves.
type Options struct {
	// Page is the page's files, index.html at the root.
	Page fs.FS
	// Socket is the service's socket, for Proxy.
	Socket string
	// Host answers under HostPrefix (nil: not found).
	Host http.Handler
	// Addr is where to listen: 127.0.0.1:0 (any free port) by default.
	// Only a loopback address is allowed.
	Addr string
}

// Server is the page on a loopback port. Close it when done.
type Server struct {
	ln      net.Listener
	srv     *http.Server
	host    string // 127.0.0.1:port, what requests must name
	cert    *x509.Certificate
	print   string // the certificate's fingerprint
	cookie  string // the cookie's name: per port, as cookies ignore ports
	secret  string // the cookie's value
	page    http.Handler
	proxy   http.Handler
	hostAPI http.Handler

	mu      sync.Mutex
	tickets map[string]time.Time // unused links, and when they expire
	now     func() time.Time
}

// Start listens and serves in the background.
func Start(o Options) (*Server, error) {
	if o.Page == nil {
		return nil, errors.New("pageserver: no page")
	}
	addr := o.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !tcp.IP.IsLoopback() {
		ln.Close()
		return nil, errors.New("pageserver: only a loopback address is allowed")
	}
	cert, print, err := newCert(tcp.IP, time.Now())
	if err != nil {
		ln.Close()
		return nil, err
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0]) // just made
	s := &Server{
		ln:      ln,
		host:    tcp.String(),
		cert:    leaf,
		print:   print,
		cookie:  "blitz_" + strconv.Itoa(tcp.Port),
		secret:  random(),
		page:    pageHandler(o.Page),
		proxy:   Proxy(o.Socket),
		hostAPI: o.Host,
		tickets: map[string]time.Time{},
		now:     time.Now,
	}
	// HTTP/2 only: an HTTP/1.1 client is turned away at the handshake.
	var protocols http.Protocols
	protocols.SetHTTP2(true)
	s.srv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         &protocols,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		},
	}
	go s.srv.ServeTLS(ln, "", "")
	return s, nil
}

// Origin is the page's origin: https://127.0.0.1:port.
func (s *Server) Origin() string { return "https://" + s.host }

// Fingerprint is the SHA-256 fingerprint of the Server's certificate, the
// one the window showing the page trusts (pageserver's Fingerprint).
func (s *Server) Fingerprint() string { return s.print }

// Transport reaches this Server, and nothing else, over HTTP/2: what a Go
// client (a test, say) uses in place of the window.
func (s *Server) Transport() *http.Transport {
	pool := x509.NewCertPool()
	pool.AddCert(s.cert)
	var protocols http.Protocols
	protocols.SetHTTP2(true)
	return &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
		Protocols:       &protocols,
	}
}

// OpenURL is a link that opens the page once: it lets the browser in (a
// cookie) and then goes to the page, so the link in the address bar and
// the history is the page's, not one that would let anyone in again.
func (s *Server) OpenURL() string {
	t := random()
	s.mu.Lock()
	now := s.now()
	for k, exp := range s.tickets {
		if now.After(exp) {
			delete(s.tickets, k)
		}
	}
	s.tickets[t] = now.Add(TicketTTL)
	s.mu.Unlock()
	return s.Origin() + "/open?t=" + t
}

// Close stops the server.
func (s *Server) Close() error { return s.srv.Close() }

// ServeHTTP checks a request and serves it: a link from OpenURL, the API,
// the host's calls or the page.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Host != s.host {
		http.Error(w, "wrong host", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != s.Origin() {
		http.Error(w, "wrong origin", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/open" {
		s.open(w, r)
		return
	}
	if !s.allowed(r) {
		http.Error(w, "Open Blitz from its tray icon.", http.StatusForbidden)
		return
	}
	r.Header.Del("Cookie") // the service needn't see it
	switch {
	case strings.HasPrefix(r.URL.Path, apiPrefix):
		r.Header.Del("Origin")
		s.proxy.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, HostPrefix):
		if s.hostAPI == nil {
			http.NotFound(w, r)
			return
		}
		s.hostAPI.ServeHTTP(w, r)
	default:
		s.page.ServeHTTP(w, r)
	}
}

// open takes a link's ticket, once, and lets the browser in. A link with
// tasks goes on to the page's tasks window (/?tasks): the desktop app's
// small window for workflows' human tasks.
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	to := "/"
	if r.URL.Query().Has("tasks") {
		to = "/?tasks"
	}
	t := r.URL.Query().Get("t")
	s.mu.Lock()
	exp, ok := s.tickets[t]
	delete(s.tickets, t)
	s.mu.Unlock()
	if !ok || s.now().After(exp) {
		if s.allowed(r) { // already in: a reload of the link
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		http.Error(w, "This link has been used or has expired: open Blitz from its tray icon again.", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookie,
		Value:    s.secret,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// allowed reports whether the request carries the cookie.
func (s *Server) allowed(r *http.Request) bool {
	c, err := r.Cookie(s.cookie)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.secret)) == 1
}

// pageHandler serves the page's files; index.html is never cached, so a
// new version's page loads its new assets.
func pageHandler(page fs.FS) http.Handler {
	files := http.FileServerFS(page)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-store")
		}
		files.ServeHTTP(w, r)
	})
}

// random is 32 random bytes in hex.
func random() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}
