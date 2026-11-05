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

// Package gitcred answers Git's credential protocol for the organization's
// service (spec_053_enterprise EN-61): Git runs the helper with get, store
// or erase and the request's attributes on stdin (protocol=https,
// host=…); for get, the helper prints a username and password. Blitz's is
// the person's access token, which only the Blitz service renews, so the
// helper asks it.
package gitcred

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
)

// Request is what Git asks for: the attributes it sent.
type Request map[string]string

// Read reads Git's attributes, key=value lines up to a blank line or the
// end.
func Read(in io.Reader) Request {
	r := Request{}
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			r[k] = v
		}
	}
	return r
}

// Answer gives the credential for host, asked over protocol (https or
// http): ok false when there's none (Git then tries its other helpers, or
// fails), or an error to say why.
type Answer func(ctx context.Context, protocol, host string) (user, password string, ok bool, err error)

// loopback reports whether host (with a port or not) is this computer.
func loopback(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Run is the helper: action is get, store or erase. Only get answers;
// Blitz keeps nothing Git hands it, since the service renews the token.
func Run(ctx context.Context, action string, in io.Reader, out io.Writer, answer Answer) error {
	switch action {
	case "get":
	case "store", "erase":
		_, _ = io.Copy(io.Discard, in)
		return nil
	default:
		return fmt.Errorf("git-credential: %q isn't get, store or erase", action)
	}
	req := Read(in)
	// Over http the token would go in clear: only to this computer, as
	// the service's address allows (a development service).
	if req["protocol"] != "https" && (req["protocol"] != "http" || !loopback(req["host"])) {
		return nil
	}
	user, password, ok, err := answer(ctx, req["protocol"], req["host"])
	if err != nil || !ok {
		return err
	}
	_, err = fmt.Fprintf(out, "username=%s\npassword=%s\n", user, password)
	return err
}
