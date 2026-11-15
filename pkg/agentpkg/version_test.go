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

package agentpkg

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func v(s string) Version {
	out, err := ParseVersion(s)
	if err != nil {
		panic(err)
	}
	return out
}

// PK-01: versions read, compare and bump.
func TestVersion(t *testing.T) {
	for _, bad := range []string{"", "1", "1.2", "1.2.x", "01.2.3", "1.-2.3", "a.b.c", "1.2.3.4"} {
		_, err := ParseVersion(bad)
		assert.Error(t, err, bad)
	}
	assert.Equal(t, Version{1, 4, 2}, v("v1.4.2"))
	assert.Equal(t, "1.4.2", v("1.4.2").String())
	assert.Equal(t, -1, v("1.4.2").Compare(v("1.10.0")))
	assert.Equal(t, 1, v("2.0.0").Compare(v("1.99.99")))
	assert.Equal(t, 0, v("1.0.0").Compare(v("1.0.0")))
	for part, want := range map[string]string{"major": "2.0.0", "minor": "1.5.0", "patch": "1.4.3", "": "1.4.3"} {
		got, err := v("1.4.2").Bump(part)
		require.NoError(t, err)
		assert.Equal(t, want, got.String(), part)
	}
	_, err := v("1.0.0").Bump("huge")
	assert.Error(t, err)
	latest, ok := Latest([]Version{v("1.0.0"), v("2.1.0"), v("2.0.9")})
	assert.True(t, ok)
	assert.Equal(t, "2.1.0", latest.String())
	_, ok = Latest(nil)
	assert.False(t, ok)
}

// PK-02: ranges.
func TestRange(t *testing.T) {
	all := []Version{v("0.4.1"), v("0.4.7"), v("0.5.0"), v("1.0.0"), v("1.4.2"), v("1.4.9"), v("1.5.0"), v("2.0.0"), v("2.3.1")}
	tests := []struct {
		rng, best string
		match     []string
	}{
		{"", "2.3.1", nil},
		{"*", "2.3.1", nil},
		{"latest", "2.3.1", nil},
		{"1.4.2", "1.4.2", []string{"1.4.2"}},
		{"1", "1.5.0", []string{"1.0.0", "1.4.2", "1.4.9", "1.5.0"}},
		{"1.x", "1.5.0", []string{"1.0.0", "1.4.2", "1.4.9", "1.5.0"}},
		{"1.4", "1.4.9", []string{"1.4.2", "1.4.9"}},
		{"1.4.x", "1.4.9", []string{"1.4.2", "1.4.9"}},
		{"^1.4.2", "1.5.0", []string{"1.4.2", "1.4.9", "1.5.0"}},
		{"~1.4.2", "1.4.9", []string{"1.4.2", "1.4.9"}},
		{"^0.4.1", "0.4.7", []string{"0.4.1", "0.4.7"}},
		{"^0.0.3", "", nil},
		{"3.x", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.rng, func(t *testing.T) {
			r, err := ParseRange(tt.rng)
			require.NoError(t, err)
			best, ok := r.Best(all)
			if tt.best == "" {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.Equal(t, tt.best, best.String())
			if tt.match != nil {
				var got []string
				for _, x := range all {
					if r.Match(x) {
						got = append(got, x.String())
					}
				}
				assert.Equal(t, tt.match, got)
			}
		})
	}
	for _, bad := range []string{"^1", "~x", "1.2.3.4", ">=1", "a.x", "-1"} {
		_, err := ParseRange(bad)
		assert.Error(t, err, bad)
	}
	r, _ := ParseRange("")
	assert.Equal(t, "*", r.String())
	r, _ = ParseRange("^1.2.0")
	assert.Equal(t, "^1.2.0", r.String())
}

// PK-03: dependencies in each of their forms.
func TestDep(t *testing.T) {
	var deps []Dep
	require.NoError(t, yaml.Unmarshal([]byte("[sql-review, pii-scrub@2.3.0, {name: tone, version: 1.x}]"), &deps))
	assert.Equal(t, []Dep{{Name: "sql-review"}, {Name: "pii-scrub", Version: "2.3.0"}, {Name: "tone", Version: "1.x"}}, deps)
	for _, d := range deps {
		assert.NoError(t, d.Check())
	}
	out, err := yaml.Marshal(deps)
	require.NoError(t, err)
	assert.Equal(t, "- sql-review\n- pii-scrub@2.3.0\n- tone@1.x\n", string(out))
	assert.True(t, deps[1].Range().Match(v("2.3.0")))
	assert.True(t, deps[0].Range().Match(v("9.9.9")))
	assert.Error(t, Dep{Name: "Bad Name"}.Check())
	assert.Error(t, Dep{Name: "ok", Version: "^1"}.Check())
	assert.Error(t, yaml.Unmarshal([]byte("[{name: [x]}]"), &deps))
}
