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
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
)

// Keys from a command (spec_parity_027 PAR-MOD-04): llm.<provider>.
// api_key_command prints the key; it runs when a key is first needed and
// again once api_key_ttl has passed, and every request carries the current
// one, so a key a password manager or gateway rotates keeps working. The
// command runs through runScript, as shell (scriptrun.go).

// defaultKeyTTL is how long a key is kept when api_key_ttl isn't set.
const defaultKeyTTL = 5 * time.Minute

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
	key, err := runScript(ctx, config.ScriptShell, k.command)
	if err != nil {
		return "", fmt.Errorf("api_key_command %w", err)
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
