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

package jsonl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A record appended after a torn last line starts a line of its own
// (N26 engine-sub-2#8).
func TestAppend(t *testing.T) {
	tests := []struct {
		name, before, want string
	}{
		{"a new file", "", "{\"b\":2}\n"},
		{"after whole lines", "{\"a\":1}\n", "{\"a\":1}\n{\"b\":2}\n"},
		{"after a torn line", "{\"a\":1}\n{\"tor", "{\"a\":1}\n{\"tor\n{\"b\":2}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "log.jsonl")
			if tt.before != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.before), 0o600))
			}
			require.NoError(t, Append(path, 0o600, []byte(`{"b":2}`)))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
	t.Run("a folder it can't make the file in", func(t *testing.T) {
		assert.Error(t, Append(filepath.Join(t.TempDir(), "missing", "log.jsonl"), 0o600, []byte("{}")))
	})
}
