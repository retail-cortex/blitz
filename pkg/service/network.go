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
// program that runs the service for many people (spec_enterprise_053
// EN-03). Every request on it needs credentials Auth accepts. blitzd never
// sets it: the socket is only its owner's, and the service runs commands.
type Network struct {
	// Listener is where it listens; TLS, its certificate.
	Listener net.Listener
	TLS      *tls.Config
	// Auth says who a request is from; Authz, whether they may make it
	// (nil: anyone Auth accepts may).
	Auth  Authenticator
	Authz Authorizer
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

type callerKey struct{}

// CallerFrom is the caller of a request that came in on the network
// listener; false on the socket, whose callers are its owner.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// check is n's problems before it's used.
func (n *Network) check() error {
	switch {
	case n.Listener == nil:
		return errors.New("network listener: no listener")
	case n.TLS == nil || (len(n.TLS.Certificates) == 0 && n.TLS.GetCertificate == nil):
		return errors.New("network listener: no TLS certificate")
	case n.Auth == nil:
		return errors.New("network listener: no authenticator")
	}
	return nil
}

// guard lets a request through to h only when n.Auth knows its caller
// and n.Authz allows the call, with the caller in its context; others get
// 401 or 403, with a JSON error as the enterprise service's (SV-21).
func (n *Network) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := n.Auth.Authenticate(r)
		if err != nil {
			refuse(w, http.StatusUnauthorized, "unauthenticated", err)
			return
		}
		ctx := context.WithValue(r.Context(), callerKey{}, c)
		if n.Authz != nil {
			if err := n.Authz.Authorize(ctx, c, r.URL.Path); err != nil {
				refuse(w, http.StatusForbidden, "forbidden", err)
				return
			}
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func refuse(w http.ResponseWriter, status int, code string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": err.Error()})
}
