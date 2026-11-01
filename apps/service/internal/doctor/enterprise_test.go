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

package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The organization's line: connected and signed in or not, reached or
// not when asked, an address that isn't one (EN-14).
func TestCheckEnterprise(t *testing.T) {
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	signedIn := &secrets.Memory{}
	_ = signedIn.Set("enterprise-session", fmt.Sprintf(`{"url":%q,"refresh_token":"rt","person":{"email":"ana@acme.com"},"info":{"name":"Acme Assist"}}`, org.URL))
	ctx := context.Background()

	cases := map[string]struct {
		url    string
		store  secrets.Store
		online bool
		status checkStatus
		want   string
	}{
		"signed in":      {org.URL, signedIn, false, statusOK, "signed in to Acme Assist (" + org.URL + ") as ana@acme.com"},
		"and reached":    {org.URL, signedIn, true, statusOK, "as ana@acme.com, reached (version 1.0.0)"},
		"not signed in":  {org.URL, &secrets.Memory{}, false, statusFail, "connected to " + org.URL + ", not signed in: blitz enterprise login"},
		"unreachable":    {"http://127.0.0.1:1", &secrets.Memory{}, true, statusFail, "isn't reached"},
		"not an address": {"ftp://acme", &secrets.Memory{}, false, statusFail, "[enterprise] url"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			st, detail := checkEnterprise(ctx, c.url, c.store, c.online)
			assert.Equal(t, c.status, st)
			assert.Contains(t, detail, c.want)
		})
	}
	t.Run("in the report", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("MODENV_PREFIX", "")
		secrets.SetDefault(&secrets.Memory{}) // never the developer's keychain
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), fmt.Appendf(nil, "[enterprise]\nurl = %q\n", org.URL), 0o600))
		checks := Run(ctx, Options{Config: dir})
		var found bool
		for _, c := range checks {
			if c.name == "organization" {
				found = true
				assert.Equal(t, statusFail, c.status, "not signed in in this keychain")
			}
			assert.NotEqual(t, "credentials", c.name, "the organization's stand in for the person's own")
		}
		assert.True(t, found)
	})
}
