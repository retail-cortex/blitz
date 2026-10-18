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

// A skill is reviewed at one content hash: another (a changed script)
// isn't, and the decision is kept by the folder, symlinks resolved.
func TestSkillTrust(t *testing.T) {
	settings, skill := t.TempDir(), t.TempDir()
	s := OpenSkillTrust(settings)
	assert.False(t, s.Reviewed(skill, "sha256:a"))
	assert.False(t, s.Reviewed(skill, ""), "no content, nothing reviewed")
	require.NoError(t, s.Trust(skill, "sha256:a"))
	require.NoError(t, s.Trust(skill, "sha256:a"), "again: nothing to write")
	assert.True(t, s.Reviewed(skill, "sha256:a"))
	assert.False(t, s.Reviewed(skill, "sha256:b"), "changed since")
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(skill, link))
	assert.True(t, OpenSkillTrust(settings).Reviewed(link, "sha256:a"), "through a link, from a new store")
	assert.False(t, s.Reviewed(filepath.Join(skill, "missing"), "sha256:a"))
	assert.Error(t, OpenSkillTrust(filepath.Join(skill, "file")).Trust(filepath.Join(skill, "missing"), "x"))
}
