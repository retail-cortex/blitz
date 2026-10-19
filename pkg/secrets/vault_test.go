// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeysAndURNs(t *testing.T) {
	for _, k := range []string{"github_token", "a.b-c", "X1"} {
		assert.True(t, ValidKey(k), k)
	}
	for _, k := range []string{"", "has space", "a/b", "{x}", string(make([]byte, 65))} {
		assert.False(t, ValidKey(k), k)
	}
	assert.Equal(t, "urn:blitz:global:secrets:github_token", URN(GlobalScope, "github_token"))
	assert.Equal(t, "urn:blitz:shop-1a2b3c4d:secrets:db", URN("shop-1a2b3c4d", "db"))
}

// Secrets are stored under their URN, listed by name from the index
// (never their values), replaced, deleted, and looked up workspace first.
func TestVault(t *testing.T) {
	store := &Memory{}
	v := Vault{Store: store, Index: filepath.Join(t.TempDir(), "names", "secret-names.toml")}
	require.NoError(t, v.Set(GlobalScope, "token", "global-value-123"))
	require.NoError(t, v.Set(GlobalScope, "alpha", "alpha-value-123"))
	require.NoError(t, v.Set("shop-1", "token", "shop-value-1234"))

	got, err := store.Get(URN("shop-1", "token"))
	require.NoError(t, err)
	assert.Equal(t, "shop-value-1234", got)

	list, err := v.List(GlobalScope)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "alpha", list[0].Key, "by name")
	assert.False(t, list[0].Updated.IsZero())
	data, err := os.ReadFile(v.Index)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "value-123", "the index holds names only")

	val, ok := v.Lookup("shop-1", "token")
	assert.True(t, ok)
	assert.Equal(t, "shop-value-1234", val, "the workspace's own first")
	val, ok = v.Lookup("shop-1", "alpha")
	assert.True(t, ok)
	assert.Equal(t, "alpha-value-123", val, "else the global one")
	_, ok = v.Lookup("shop-1", "missing")
	assert.False(t, ok)

	require.NoError(t, v.Delete("shop-1", "token"))
	list, err = v.List("shop-1")
	require.NoError(t, err)
	assert.Empty(t, list)
	require.NoError(t, v.Delete("shop-1", "token"), "twice")

	assert.ErrorContains(t, v.Set(GlobalScope, "bad key", "x"), "can't name a secret")
	assert.ErrorContains(t, v.Set(GlobalScope, "empty", ""), "needs a value")

	require.NoError(t, os.WriteFile(v.Index, []byte("[[[ broken"), 0o600))
	_, err = v.List(GlobalScope)
	assert.ErrorContains(t, err, "secrets' names")
}

func TestExpand(t *testing.T) {
	lookup := func(scope, k string) (string, bool) {
		m := map[string]string{"/token": "s3cr3t-value", "/db.pass": "pw-value-99", "global/token": "global-only", "workspace/token": "ws-only"}
		v, ok := m[scope+"/"+k]
		return v, ok
	}
	for _, tc := range []struct {
		name, in, want string
		used           []string
	}{
		{"none", "plain text {x}", "plain text {x}", nil},
		{"one", "use {secrets:token} here", "use s3cr3t-value here", []string{"s3cr3t-value"}},
		{"two", "{secrets:token}:{secrets:db.pass}", "s3cr3t-value:pw-value-99", []string{"s3cr3t-value", "pw-value-99"}},
		{"global only", "{secrets:global:token}", "global-only", []string{"global-only"}},
		{"workspace only", "{secrets:workspace:token}", "ws-only", []string{"ws-only"}},
		{"unknown stays", "keep {secrets:nope}", "keep {secrets:nope}", nil},
		{"unknown scope", "{secrets:other:token}", "{secrets:other:token}", nil},
		{"not a key", "{secrets:has space}", "{secrets:has space}", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, used := Expand(tc.in, lookup)
			assert.Equal(t, tc.want, out)
			assert.Equal(t, tc.used, used)
		})
	}
	assert.True(t, HasPlaceholders("a {secrets:token}"))
	assert.True(t, HasPlaceholders("a {secrets:global:token}"))
	assert.False(t, HasPlaceholders("a {secrets:}"))
	assert.False(t, HasPlaceholders("nothing"))
}

// Resolve: no scope tries the workspace's, then the global one; a named
// scope only its own, with no fallback.
func TestResolve(t *testing.T) {
	v := Vault{Store: &Memory{}, Index: filepath.Join(t.TempDir(), "names.toml")}
	require.NoError(t, v.Set(GlobalScope, "token", "global-token-1"))
	require.NoError(t, v.Set(GlobalScope, "only_global", "only-global-1"))
	require.NoError(t, v.Set("shop-1", "token", "shop-token-11"))
	r := v.Resolve("shop-1")
	for _, tc := range []struct {
		scope, key, want string
		ok               bool
	}{
		{AnyScope, "token", "shop-token-11", true},
		{AnyScope, "only_global", "only-global-1", true},
		{OnlyGlobal, "token", "global-token-1", true},
		{OnlyWorkspace, "token", "shop-token-11", true},
		{OnlyWorkspace, "only_global", "", false},
		{OnlyGlobal, "missing", "", false},
	} {
		got, ok := r(tc.scope, tc.key)
		assert.Equal(t, tc.ok, ok, "%s %s", tc.scope, tc.key)
		assert.Equal(t, tc.want, got, "%s %s", tc.scope, tc.key)
	}
	_, ok := v.Resolve("")(OnlyWorkspace, "token")
	assert.False(t, ok, "no workspace: no workspace secrets")
}
