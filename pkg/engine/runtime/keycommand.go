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

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Keys from a command (spec_parity_027 PAR-MOD-04): llm.<provider>.
// api_key_command prints the key; it runs when a key is first needed and
// again once api_key_ttl has passed, and every request carries the current
// one, so a key a password manager or gateway rotates keeps working.

const (
	defaultKeyTTL     = 5 * time.Minute
	keyCommandTimeout = 30 * time.Second
)

// commandKey is one key command's cached output.
type commandKey struct {
	command string
	ttl     time.Duration

	mu  sync.Mutex
	key string
	at  time.Time
}

// keyCommands are shared by the models using the same command.
var keyCommands sync.Map // command + ttl → *commandKey

// keyFromCommand is the key source for command (ttl: a Go duration, "" for
// the default).
func keyFromCommand(command, ttl string) (*commandKey, error) {
	d := defaultKeyTTL
	if ttl != "" {
		var err error
		if d, err = time.ParseDuration(ttl); err != nil || d <= 0 {
			return nil, fmt.Errorf("api_key_ttl %q: a duration such as 5m", ttl)
		}
	}
	k, _ := keyCommands.LoadOrStore(command+"\x00"+d.String(), &commandKey{command: command, ttl: d})
	return k.(*commandKey), nil
}

// commandKeyNow is the key source for command, with its key now (so a
// command that fails fails the model's build).
func commandKeyNow(ctx context.Context, command, ttl string) (*commandKey, string, error) {
	k, err := keyFromCommand(command, ttl)
	if err != nil {
		return nil, "", err
	}
	key, err := k.get(ctx)
	return k, key, err
}

// get is the key: cached, or from running the command.
func (k *commandKey) get(ctx context.Context) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.key != "" && time.Since(k.at) < k.ttl {
		return k.key, nil
	}
	ctx, cancel := context.WithTimeout(ctx, keyCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", k.command)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("api_key_command timed out after %s", keyCommandTimeout)
		}
		return "", fmt.Errorf("api_key_command failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	key := strings.TrimSpace(out.String())
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return "", errors.New("api_key_command printed no key (or more than one line)")
	}
	k.key, k.at = key, time.Now()
	return key, nil
}

// keyTransport puts the current key in each request's header (and, with
// bearer, as "Bearer <key>").
type keyTransport struct {
	base   http.RoundTripper
	key    *commandKey
	header string
	bearer bool
}

func (t *keyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	key, err := t.key.get(req.Context())
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	if t.bearer {
		key = "Bearer " + key
	}
	req.Header.Set(t.header, key)
	return t.base.RoundTrip(req)
}

// withKeyCommand wraps client so its requests carry the command's key.
func withKeyCommand(client *http.Client, key *commandKey, header string, bearer bool) *http.Client {
	c := *client
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.Transport = &keyTransport{base: base, key: key, header: header, bearer: bearer}
	return &c
}
