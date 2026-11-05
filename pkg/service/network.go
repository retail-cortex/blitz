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
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
)

// Network is a listener other machines reach, beside the socket: for a
// program that runs the service for many people (spec_053_enterprise
// EN-03). Every request on it needs credentials Auth accepts. blitzd never
// sets it: the socket is only its owner's, and the service runs commands.
type Network struct {
	// Listener is where it listens; TLS, its certificate.
	Listener net.Listener
	TLS      *tls.Config
	// Plain serves without TLS, HTTP/1.1 and HTTP/2 in cleartext (h2c),
	// for a listener only a front end that ends TLS itself reaches: Cloud
	// Run's, say. TLS is then unused.
	Plain bool
	// Auth says who a request is from; Authz, whether they may make it
	// (nil: anyone Auth accepts may).
	Auth  Authenticator
	Authz Authorizer
	// Routes are the program's own paths on the listener (discovery,
	// health checks, its own sign-in): a request a pattern in Routes
	// matches goes there without Auth, the program checking its callers
	// itself; every other request goes to Blitz's services, guarded.
	Routes *http.ServeMux
	// Wrap, when set, sees every request on the listener first, Routes'
	// and Blitz's alike: a program's own session standing in for the
	// credentials Auth reads (the enterprise admin console's cookie, say).
	Wrap func(http.Handler) http.Handler
}

// Caller is who a request on the network listener is from.
type Caller struct {
	ID    string // stable: never an email, which can change
	Email string
	Roles []string
}

// Authenticator says who a request is from, from its credentials (a
// bearer token, say); an error when it has none or they don't hold.
type Authenticator interface {
	Authenticate(r *http.Request) (Caller, error)
}

// Authorizer says whether c may call procedure (a Connect procedure, as
// "/blitz.v1.WorkspaceService/RunTurn", or another path the service
// serves); an error says why not.
type Authorizer interface {
	Authorize(ctx context.Context, c Caller, procedure string) error
}

// Refusal is an error an Authenticator or Authorizer returns to answer
// with a status and code of its own rather than 401 unauthenticated or
// 403 forbidden: 403 terms_required until the person accepts the
// organization's terms, say.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

// Error is the message, for the person.
func (r *Refusal) Error() string { return r.Message }

// check is n's problems before it's used.
func (n *Network) check() error {
	switch {
	case n.Listener == nil:
		return errors.New("network listener: no listener")
	case !n.Plain && (n.TLS == nil || (len(n.TLS.Certificates) == 0 && n.TLS.GetCertificate == nil)):
		return errors.New("network listener: no TLS certificate")
	case n.Auth == nil:
		return errors.New("network listener: no authenticator")
	}
	return nil
}

// guard lets a request through to h only when n.Auth knows its caller
// and n.Authz allows the call; others get 401 or 403, with a JSON error as the enterprise service's (SV-21).
func (n *Network) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := n.Auth.Authenticate(r)
		if err != nil {
			refuse(w, http.StatusUnauthorized, "unauthenticated", err)
			return
		}
		if n.Authz != nil {
			if err := n.Authz.Authorize(r.Context(), c, r.URL.Path); err != nil {
				refuse(w, http.StatusForbidden, "forbidden", err)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// handler is what the listener serves: n.Routes' paths, and blitz's
// behind guard.
func (n *Network) handler(blitz http.Handler) http.Handler {
	h := n.guard(blitz)
	if n.Routes != nil {
		guarded := h
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, pattern := n.Routes.Handler(r); pattern != "" {
				n.Routes.ServeHTTP(w, r)
				return
			}
			guarded.ServeHTTP(w, r)
		})
	}
	if n.Wrap != nil {
		h = n.Wrap(h)
	}
	return h
}

func refuse(w http.ResponseWriter, status int, code string, err error) {
	msg := err.Error()
	if r, ok := errors.AsType[*Refusal](err); ok {
		status, code, msg = r.Status, r.Code, r.Message
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}
