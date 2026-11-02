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

package secretscan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A key committed once and removed since is still in the history that
// would be shared; a large binary isn't read.
func TestHistory(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@acme.com", "-c", "init.defaultBranch=main"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	git("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.py"), []byte("KEY = 'AKIAIOSFODNN7EXAMPLE'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# plans\n"), 0o600))
	git("add", "-A")
	git("commit", "-q", "-m", "first")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.py"), []byte("KEY = os.environ['KEY']\n"), 0o600))
	git("commit", "-q", "-am", "no key")

	found, complete, err := History(context.Background(), dir, "HEAD", 100)
	require.NoError(t, err)
	assert.True(t, complete)
	require.Len(t, found, 1)
	assert.Equal(t, "config.py", found[0].Path)
	assert.Equal(t, "AWS access key", found[0].Kind)

	_, complete, err = History(context.Background(), dir, "HEAD", 1)
	require.NoError(t, err)
	assert.False(t, complete, "more files than it reads")
	_, _, err = History(context.Background(), dir, "nope", 10)
	assert.Error(t, err)
}
