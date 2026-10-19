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

package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// The user's own secrets (spec_secrets_039): values kept in the Store
// under urn:blitz:<scope>:secrets:<key>, where scope is "global" or a
// workspace's (config.SecretScope), and named in prompts, instructions
// and settings as {secrets:<key>}. An index beside the settings keeps
// their names (never their values), so they can be listed on any store.

// GlobalScope is the scope of secrets every workspace sees.
const GlobalScope = "global"

// keyRE is what a secret's key may be.
var keyRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// ValidKey reports whether key can name a secret: letters, digits, _, .
// and -, up to 64.
func ValidKey(key string) bool { return keyRE.MatchString(key) }

// URN is the stored name of the secret key in scope.
func URN(scope, key string) string { return "urn:blitz:" + scope + ":secrets:" + key }

// Entry is a secret as listed: its key and when its value was last set.
type Entry struct {
	Key     string
	Updated time.Time
}

// Vault keeps the user's secrets in Store, their names in the index file
// Index.
type Vault struct {
	Store Store
	Index string
}

// vaultMu serialises the index's read-modify-write within the process.
var vaultMu sync.Mutex

// index is the index file: scope → key → when it was set.
type index map[string]map[string]time.Time

func (v Vault) read() (index, error) {
	ix := index{}
	if _, err := toml.DecodeFile(v.Index, &ix); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading the secrets' names: %w", err)
	}
	return ix, nil
}

func (v Vault) write(ix index) error {
	if err := os.MkdirAll(filepath.Dir(v.Index), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# The names of Blitz's secrets (their values are in the system's keychain).\n")
	if err := toml.NewEncoder(&b).Encode(ix); err != nil {
		return err
	}
	tmp := v.Index + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.Index)
}

// List is scope's secrets, by key.
func (v Vault) List(scope string) ([]Entry, error) {
	vaultMu.Lock()
	defer vaultMu.Unlock()
	ix, err := v.read()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for k, t := range ix[scope] {
		out = append(out, Entry{Key: k, Updated: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Set stores value as scope's secret key.
func (v Vault) Set(scope, key, value string) error {
	if !ValidKey(key) {
		return fmt.Errorf("%q can't name a secret: use letters, digits, _, . and -", key)
	}
	if value == "" {
		return errors.New("a secret needs a value")
	}
	vaultMu.Lock()
	defer vaultMu.Unlock()
	ix, err := v.read()
	if err != nil {
		return err
	}
	if err := v.Store.Set(URN(scope, key), value); err != nil {
		return err
	}
	if ix[scope] == nil {
		ix[scope] = map[string]time.Time{}
	}
	ix[scope][key] = time.Now().UTC().Truncate(time.Second)
	return v.write(ix)
}

// Delete removes scope's secret key (a missing one isn't an error).
func (v Vault) Delete(scope, key string) error {
	vaultMu.Lock()
	defer vaultMu.Unlock()
	ix, err := v.read()
	if err != nil {
		return err
	}
	if err := v.Store.Delete(URN(scope, key)); err != nil {
		return err
	}
	if _, ok := ix[scope][key]; !ok {
		return nil
	}
	delete(ix[scope], key)
	if len(ix[scope]) == 0 {
		delete(ix, scope)
	}
	return v.write(ix)
}

// Lookup is the secret key as a workspace in scope sees it: its own,
// else the global one.
func (v Vault) Lookup(scope, key string) (string, bool) { return v.Resolve(scope)(AnyScope, key) }

// placeholderRE is {secrets:<key>}, {secrets:global:<key>} or
// {secrets:workspace:<key>}.
var placeholderRE = regexp.MustCompile(`\{secrets:(?:(global|workspace):)?([A-Za-z0-9_.-]{1,64})\}`)

// The scopes a placeholder can name: none (the workspace's, else the
// global one), only the global one, or only the workspace's.
const (
	AnyScope      = ""
	OnlyGlobal    = "global"
	OnlyWorkspace = "workspace"
)

// HasPlaceholders reports whether text names a secret.
func HasPlaceholders(text string) bool {
	return strings.Contains(text, "{secrets:") && placeholderRE.MatchString(text)
}

// Expand puts each placeholder in text's place with its value, as lookup
// finds it for the scope the placeholder names (AnyScope, OnlyGlobal or
// OnlyWorkspace), and returns the values used (for the redactor). One
// lookup doesn't find is left as written.
func Expand(text string, lookup func(scope, key string) (string, bool)) (string, []string) {
	if !strings.Contains(text, "{secrets:") {
		return text, nil
	}
	var used []string
	out := placeholderRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := placeholderRE.FindStringSubmatch(m)
		val, ok := lookup(sub[1], sub[2])
		if !ok {
			return m
		}
		used = append(used, val)
		return val
	})
	return out, used
}

// Resolve is a lookup for Expand in a workspace's scope: AnyScope tries
// the workspace's own, then the global one; OnlyGlobal and OnlyWorkspace
// only theirs.
func (v Vault) Resolve(workspaceScope string) func(scope, key string) (string, bool) {
	return func(scope, key string) (string, bool) {
		var scopes []string
		switch scope {
		case OnlyGlobal:
			scopes = []string{GlobalScope}
		case OnlyWorkspace:
			scopes = []string{workspaceScope}
		default:
			scopes = []string{workspaceScope, GlobalScope}
		}
		for _, s := range scopes {
			if s == "" {
				continue
			}
			if val, err := v.Store.Get(URN(s, key)); err == nil {
				return val, true
			}
		}
		return "", false
	}
}
