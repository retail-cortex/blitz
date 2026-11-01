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

package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selfSigned is a certificate for 127.0.0.1 and a pool that trusts it.
func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}, pool
}

// tokens knows two people by bearer token.
type tokens map[string]Caller

func (k tokens) Authenticate(r *http.Request) (Caller, error) {
	c, ok := k[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		return Caller{}, errors.New("sign in first")
	}
	return c, nil
}

// readers may list agents and nothing else.
type readers struct{}

func (readers) Authorize(_ context.Context, c Caller, procedure string) error {
	if strings.HasSuffix(procedure, "/Terms") {
		return fmt.Errorf("refused: %w", &Refusal{Status: 451, Code: "terms_required", Message: "accept the terms"})
	}
	if strings.HasSuffix(procedure, "/ListAgents") || c.Roles[0] == "admin" {
		return nil
	}
	return errors.New("not allowed for " + c.Roles[0])
}

// bearer adds a token to every request.
type bearer struct {
	token string
	rt    http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.rt.RoundTrip(r)
}

// The network listener serves the API over TLS (HTTP/2) to callers the
// authenticator knows, as far as the authorizer allows; the socket goes
// on as before.
func TestRunNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "LLM_PROVIDER"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	dir, err := os.MkdirTemp("/tmp", "bd") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")

	cert, pool := selfSigned(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	n := &Network{
		Listener: l, TLS: &tls.Config{Certificates: []tls.Certificate{cert}},
		Auth:  tokens{"ana": {ID: "u1", Email: "ana@acme.com", Roles: []string{"member"}}, "bo": {ID: "u2", Roles: []string{"admin"}}},
		Authz: readers{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{Socket: sock, Network: n}) }()
	for deadline := time.Now().Add(10 * time.Second); !socket.Running(sock); time.Sleep(20 * time.Millisecond) {
		require.False(t, time.Now().After(deadline), "service didn't start")
	}

	base := "https://" + l.Addr().String()
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}
	defer tr.CloseIdleConnections()
	client := func(token string) pb.WorkspaceServiceClient {
		return pb.NewWorkspaceServiceClient(&http.Client{Transport: bearer{token, tr}}, base)
	}
	ws := t.TempDir()

	t.Run("a known caller, allowed", func(t *testing.T) {
		res, err := client("ana").ListAgents(context.Background(), connect.NewRequest(&pb.ListAgentsRequest{Workspace: ws}))
		require.NoError(t, err)
		assert.NotEmpty(t, res.Msg.Agents)
	})
	t.Run("over HTTP/2", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, base+"/", nil)
		res, err := (&http.Client{Transport: bearer{"bo", tr}}).Do(req)
		require.NoError(t, err)
		res.Body.Close()
		assert.Equal(t, 2, res.ProtoMajor)
	})
	t.Run("a known caller, not allowed", func(t *testing.T) {
		c := pb.NewSessionServiceClient(&http.Client{Transport: bearer{"ana", tr}}, base)
		_, err := c.ListSessions(context.Background(), connect.NewRequest(&pb.ListSessionsRequest{Workspace: ws}))
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	})
	t.Run("nobody", func(t *testing.T) {
		_, err := client("eve").ListAgents(context.Background(), connect.NewRequest(&pb.ListAgentsRequest{Workspace: ws}))
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "%v", err)
	})
	t.Run("the socket asks for nothing", func(t *testing.T) {
		c := pb.NewSessionServiceClient(socket.Client(sock), socket.BaseURL)
		_, err := c.ListSessions(context.Background(), connect.NewRequest(&pb.ListSessionsRequest{Workspace: ws}))
		assert.NoError(t, err)
	})

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "serve")
	case <-time.After(15 * time.Second):
		t.Fatal("service didn't stop")
	}
}

func TestNetworkCheck(t *testing.T) {
	cert, _ := selfSigned(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	good := func() *Network {
		return &Network{Listener: l, TLS: &tls.Config{Certificates: []tls.Certificate{cert}}, Auth: tokens{}}
	}
	cases := map[string]struct {
		change func(*Network)
		says   string
	}{
		"no listener":       {func(n *Network) { n.Listener = nil }, "no listener"},
		"no TLS":            {func(n *Network) { n.TLS = nil }, "no TLS certificate"},
		"TLS without certs": {func(n *Network) { n.TLS = &tls.Config{} }, "no TLS certificate"},
		"no authenticator":  {func(n *Network) { n.Auth = nil }, "no authenticator"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			n := good()
			c.change(n)
			err := Run(context.Background(), Options{Socket: filepath.Join(t.TempDir(), "s.sock"), Network: n})
			assert.ErrorContains(t, err, c.says)
		})
	}
	assert.NoError(t, good().check())
	plain := good()
	plain.TLS, plain.Plain = nil, true
	assert.NoError(t, plain.check(), "plain needs no certificate: a front end ends TLS")
	n := good()
	n.TLS = &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &cert, nil }}
	assert.NoError(t, n.check(), "a certificate callback will do")
}

// guard puts the caller in the request's context, and answers refusals
// with a JSON error.
func TestGuard(t *testing.T) {
	n := &Network{Auth: tokens{"ana": {ID: "u1", Roles: []string{"member"}}}, Authz: readers{}}
	var seen Caller
	h := n.guard(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = CallerFrom(r.Context())
	}))
	cases := map[string]struct {
		token, path string
		status      int
		body        string
	}{
		"allowed":         {"ana", "/blitz.v1.WorkspaceService/ListAgents", http.StatusOK, ""},
		"refused":         {"ana", "/blitz.v1.WorkspaceService/RunTurn", http.StatusForbidden, `{"code":"forbidden","message":"not allowed for member"}`},
		"no caller":       {"", "/blitz.v1.WorkspaceService/ListAgents", http.StatusUnauthorized, `{"code":"unauthenticated","message":"sign in first"}`},
		"bad caller":      {"eve", "/", http.StatusUnauthorized, `"unauthenticated"`},
		"its own refusal": {"ana", "/blitz.v1.WorkspaceService/Terms", 451, `{"code":"terms_required","message":"accept the terms"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			seen = Caller{}
			req := httptest.NewRequest(http.MethodPost, c.path, nil)
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.Equal(t, c.status, rec.Code)
			if c.status == http.StatusOK {
				assert.Equal(t, "u1", seen.ID)
				return
			}
			assert.Empty(t, seen.ID, "the handler wasn't reached")
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.Contains(t, rec.Body.String(), c.body)
		})
	}
	_, ok := CallerFrom(context.Background())
	assert.False(t, ok)
}

// The program's own routes answer without sign-in; every other path is
// Blitz's, guarded.
func TestNetworkRoutes(t *testing.T) {
	routes := http.NewServeMux()
	routes.HandleFunc("GET /api/v1/info", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("info")) })
	n := &Network{Auth: tokens{"ana": {ID: "u1", Roles: []string{"member"}}}, Routes: routes}
	h := n.handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("blitz")) }))
	cases := map[string]struct {
		method, path, token string
		status              int
		body                string
	}{
		"its route, no sign-in":    {http.MethodGet, "/api/v1/info", "", http.StatusOK, "info"},
		"Blitz's, signed in":       {http.MethodPost, "/blitz.v1.WorkspaceService/ListAgents", "ana", http.StatusOK, "blitz"},
		"Blitz's, no sign-in":      {http.MethodPost, "/blitz.v1.WorkspaceService/ListAgents", "", http.StatusUnauthorized, `"unauthenticated"`},
		"a path neither has":       {http.MethodGet, "/nothing", "", http.StatusUnauthorized, `"unauthenticated"`},
		"its path, another method": {http.MethodPost, "/api/v1/info", "", http.StatusUnauthorized, `"unauthenticated"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.Equal(t, c.status, rec.Code)
			assert.Contains(t, rec.Body.String(), c.body)
		})
	}
	t.Run("no routes", func(t *testing.T) {
		rec := httptest.NewRecorder()
		(&Network{Auth: tokens{}}).handler(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/info", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
	t.Run("a program's session stands in for credentials", func(t *testing.T) {
		n.Wrap = func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c, err := r.Cookie("session"); err == nil && c.Value == "ana's" {
					r.Header.Set("Authorization", "Bearer ana")
				}
				next.ServeHTTP(w, r)
			})
		}
		defer func() { n.Wrap = nil }()
		h := n.handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("blitz")) }))
		req := httptest.NewRequest(http.MethodPost, "/blitz.v1.WorkspaceService/ListAgents", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "ana's"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "blitz", rec.Body.String())
	})
}
