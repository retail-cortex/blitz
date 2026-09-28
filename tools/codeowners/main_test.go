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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Paths keep their first order, and a path owned twice lists both handles.
func TestGenerate(t *testing.T) {
	got, err := generate("# comment\n\nAda Lovelace @ada *\nAlan Turing  @alan  /pkg/engine/ *\n")
	require.NoError(t, err)
	assert.Equal(t, "# Generated from OWNERS.txt by //tools/codeowners. Don't edit: change\n# OWNERS.txt and run bazel run //tools/codeowners.\n\n"+
		"* @ada @alan\n/pkg/engine/ @alan\n", string(got))
}

func TestGenerateRefuses(t *testing.T) {
	for name, owners := range map[string]string{
		"no handle":      "Ada Lovelace *\n",
		"no name":        "@ada *\n",
		"no paths":       "Ada Lovelace @ada\n",
		"two handles":    "Ada Lovelace @ada @alan *\n",
		"no maintainers": "# nobody\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := generate(owners)
			assert.Error(t, err)
		})
	}
}
