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

package enterprise

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
)

// PK-16, EN-96: the registry through a token or the person's sign-in: a
// package fetched by range (the latest when none), searched, published; a
// package of another registry's refused.
func TestRegistryClient(t *testing.T) {
	_, s, c, _, _ := setup(t)
	ctx := context.Background()
	s.Mu.Lock()
	s.RegistryToken = "blitzreg_ci"
	s.Registry = map[string]enterprisetest.RegistryAsset{"skill/tone": {Version: "1.2.0", Description: "Our tone", Files: map[string]string{"skills/tone/SKILL.md": "T"}}}
	s.Mu.Unlock()
	host := strings.TrimPrefix(s.URL, "http://")
	tests := []struct {
		name string
		r    *Registry
	}{
		{"a token", &Registry{URL: s.URL, Token: "blitzreg_ci"}},
		{"the person", &Registry{Client: c}},
	}
	require.NoError(t, c.SignInBrowser(ctx, browse(t), nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, files, err := tt.r.Fetch(ctx, host, "skill", "tone", "")
			require.NoError(t, err)
			assert.Equal(t, "1.2.0", v)
			assert.Equal(t, "T", files["skills/tone/SKILL.md"])
			_, _, err = tt.r.Fetch(ctx, "other.example", "skill", "tone", "1.x")
			assert.ErrorContains(t, err, "your registry is "+host)
			_, _, err = tt.r.Fetch(ctx, host, "skill", "nope", "1.x")
			assert.Error(t, err)
			found, err := tt.r.Search(ctx, "tone", "")
			require.NoError(t, err)
			require.Len(t, found, 1)
			assert.Equal(t, "available", found[0].State)
			p, err := tt.r.Publish(ctx, "skill", "tone", map[string]string{"skills/tone/SKILL.md": "T2"}, "patch", "")
			require.NoError(t, err)
			assert.Equal(t, "1.0.0", p.Version)
		})
	}
	found, err := c.RegistrySearch(ctx, "", "agent")
	require.NoError(t, err)
	assert.Empty(t, found)
	_, err = (&Registry{URL: s.URL, Token: "blitzreg_wrong"}).Search(ctx, "", "")
	assert.Error(t, err)
	_, _, err = (&Registry{URL: "not a url", Token: "t"}).Fetch(ctx, host, "skill", "tone", "")
	assert.ErrorContains(t, err, "your registry is not a url")
	_, err = c.CallAPI(ctx, "x", "BAD METHOD", "/", "", nil, nil)
	assert.Error(t, err)
}
