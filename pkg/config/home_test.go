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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHomeWorkspace(t *testing.T) {
	prefix := t.TempDir()
	assert.Equal(t, filepath.Join(prefix, "home"), HomeWorkspace(prefix))
	assert.Equal(t, filepath.Join(prefix, "workers"), UserWorkersDir(prefix))
	assert.False(t, IsHomeWorkspace(prefix, HomeWorkspace(prefix)+"-not"))

	home, err := EnsureHomeWorkspace(prefix)
	require.NoError(t, err)
	info, err := os.Stat(home)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{"as made", home, true},
		{"as named", HomeWorkspace(prefix), true},
		{"with a trailing separator", HomeWorkspace(prefix) + string(filepath.Separator), true},
		{"another folder", t.TempDir(), false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsHomeWorkspace(prefix, tt.dir))
		})
	}
}
