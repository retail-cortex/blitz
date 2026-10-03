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

package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"
)

// proxy is the browser's only way out: an HTTP proxy on loopback that
// refuses denied hosts, and addresses that aren't public unless local ones
// are allowed. The address is checked as dialled, after DNS, so a name that
// resolves (or rebinds) to an internal address is refused too. Plain
// requests, and tunnels for https and WebSockets, go through it.
type proxy struct {
	ln        net.Listener
	srv       *http.Server
	transport *http.Transport
	dialer    *net.Dialer
	// host decides a host before anything is dialled (deny_domains).
	host func(host string) error
	// refused records what was refused, for the tool to report.
	refused func(host string, err error)

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// ErrBlockedAddress means the browser tried to reach an address that isn't
// public.
var ErrBlockedAddress = errors.New("not a public address")

func startProxy(allowAddr func(netip.Addr) bool, host func(string) error, refused func(string, error)) (*proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &proxy{ln: ln, host: host, refused: refused, conns: map[net.Conn]struct{}{}}
	p.dialer = &net.Dialer{
		Timeout: 15 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !allowAddr(ap.Addr()) {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
			}
			return nil
		},
	}
	p.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           p.dialer.DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
	}
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second}
	go p.srv.Serve(ln)
	return p, nil
}

// addr is the proxy's host:port.
func (p *proxy) addr() string { return p.ln.Addr().String() }

func (p *proxy) close() {
	p.srv.Close()
	p.transport.CloseIdleConnections()
	p.mu.Lock()
	for c := range p.conns {
		c.Close()
	}
	p.mu.Unlock()
}

func (p *proxy) check(host string) error {
	if err := p.host(host); err != nil {
		p.refused(host, err)
		return err
	}
	return nil
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	if r.URL.Host == "" || (r.URL.Scheme != "http" && r.URL.Scheme != "https") {
		http.Error(w, "the browser's proxy only forwards http", http.StatusBadRequest)
		return
	}
	if err := p.check(r.URL.Hostname()); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range []string{"Proxy-Connection", "Proxy-Authorization", "Connection", "Keep-Alive", "Te", "Trailer", "Upgrade"} {
		out.Header.Del(h)
	}
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			p.refused(r.URL.Hostname(), err)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// tunnel serves CONNECT: https and WebSockets.
func (p *proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "bad CONNECT target", http.StatusBadRequest)
		return
	}
	if err := p.check(host); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	upstream, err := p.dialer.DialContext(ctx, "tcp", r.Host)
	cancel()
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			p.refused(host, err)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "no tunnel", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	p.track(client, upstream)
	client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	go func() {
		if n := buf.Reader.Buffered(); n > 0 {
			b, _ := buf.Reader.Peek(n)
			upstream.Write(b)
		}
		io.Copy(upstream, client)
		upstream.Close()
	}()
	io.Copy(client, upstream)
	client.Close()
	p.untrack(client, upstream)
}

func (p *proxy) track(cs ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range cs {
		p.conns[c] = struct{}{}
	}
}

func (p *proxy) untrack(cs ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range cs {
		delete(p.conns, c)
	}
}

// PublicAddr reports whether addr is a routable public address: not
// loopback, private, link-local (cloud metadata), CGNAT, multicast or
// unspecified.
func PublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && !addr.IsLoopback() && !addr.IsPrivate() && !addr.IsLinkLocalUnicast() &&
		!addr.IsLinkLocalMulticast() && !addr.IsInterfaceLocalMulticast() && !addr.IsMulticast() &&
		!addr.IsUnspecified() && !cgnat.Contains(addr) && !(addr.Is4() && addr.As4()[0] == 0)
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// normalHost is host lower-cased, without a trailing dot or brackets.
func normalHost(host string) string {
	return strings.Trim(strings.ToLower(strings.TrimSuffix(host, ".")), "[]")
}
