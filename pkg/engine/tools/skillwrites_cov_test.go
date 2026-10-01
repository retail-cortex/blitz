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

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const covWriterSkill = `---
name: writer
execution_hints:
  writes_workspace: true
scripts:
  - name: edit
    language: python
    inline_code: |
      import os
      open("README.md", "w").write("rewritten\n")
      open("new.txt", "w").write("new\n")
      os.remove("gone.txt")
      os.makedirs("sub", exist_ok=True)
      open("sub/deep.txt", "w").write("deep\n")
  - name: noop
    language: python
    inline_code: "print(sorted(__import__('os').listdir('.')))"
  - name: broken
    language: python
    inline_code: |
      open("README.md", "w").write("half done\n")
      raise SystemExit(1)
  - name: secret
    language: python
    inline_code: |
      open(".env", "w").write("STOLEN=1\n")
      open("ok.txt", "w").write("ok\n")
  - name: unreadable
    language: python
    inline_code: |
      import os
      open("locked.txt", "w").write("x")
      os.chmod("locked.txt", 0)
---
`

// newWriterFixture is the writes_workspace skill over a workspace with a
// .git directory, a blocked file and a symbolic link.
func newWriterFixture(t *testing.T, decide func(api.ApprovalRequest) api.Decision) covFixture {
	t.Helper()
	f := newCovFixture(t, map[string]string{"writer": covWriterSkill}, nil, decide, nil)
	for name, body := range map[string]string{"gone.txt": "bye\n", ".git/config": "[core]\n", ".env": "SECRET=1\n"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(f.ws, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(f.ws, name), []byte(body), 0o644))
	}
	require.NoError(t, os.Symlink("README.md", filepath.Join(f.ws, "link.md")))
	return f
}

// A writes_workspace script works in a copy without .git, blocked paths or
// links; what it changed is kept once approved, and not when it failed,
// changed nothing, or was refused.
func TestRunSkillScriptWritesWorkspace(t *testing.T) {
	f := newWriterFixture(t, approveAll)
	out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "noop"})
	require.Equal(t, "", out.Error, "%+v", out)
	for _, hidden := range []string{".git", ".env", "link.md"} {
		assert.NotContains(t, out.Stdout, "'"+hidden+"'", "the copy has neither .git, blocked paths nor links")
	}
	assert.Contains(t, out.Stdout, "'README.md', 'gone.txt'")
	assert.Empty(t, out.Changed)
	assert.Empty(t, *f.reqs, "nothing changed: nothing to approve")

	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "broken"})
	assert.Contains(t, out.Error, "the script failed, so its changes to the workspace weren't kept")
	assert.Equal(t, "original\n", read(t, f.ws, "README.md"))

	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "edit"})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.Equal(t, []string{"README.md", "gone.txt", "new.txt", "sub/deep.txt"}, out.Changed)
	require.Len(t, *f.reqs, 1)
	assert.Equal(t, api.ActionWrite, (*f.reqs)[0].Kind)
	assert.Equal(t, out.Changed, (*f.reqs)[0].Targets)
	assert.Contains(t, (*f.reqs)[0].Diff, "-bye")
	assert.Equal(t, "rewritten\n", read(t, f.ws, "README.md"))
	assert.Equal(t, "<none>", read(t, f.ws, "gone.txt"))

	// A blocked file the script made isn't written; the rest is.
	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "secret"})
	assert.Contains(t, out.Error, "the script's changes weren't kept: .env:", "%+v", out)
	assert.Equal(t, []string{"ok.txt"}, out.Changed)
	assert.Equal(t, "SECRET=1\n", read(t, f.ws, ".env"))
	assert.Equal(t, "ok\n", read(t, f.ws, "ok.txt"))

	refused := newWriterFixture(t, denyAll)
	out = refused.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "edit"})
	assert.Contains(t, out.Error, "the script's changes weren't kept: action not approved", "%+v", out)
	assert.Equal(t, "original\n", read(t, refused.ws, "README.md"))
}

// Files that can't be read: left out of the copy, and an error when the
// script leaves one behind.
func TestRunSkillScriptUnreadableFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	f := newWriterFixture(t, approveAll)
	require.NoError(t, os.WriteFile(filepath.Join(f.ws, "private.txt"), []byte("x"), 0o000))
	out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "noop"})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.NotContains(t, out.Stdout, "private.txt", "an unreadable file isn't copied")

	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "unreadable"})
	assert.Contains(t, out.Error, "reading the script's changes", "%+v", out)

	locked := filepath.Join(f.ws, "locked")
	require.NoError(t, os.Mkdir(locked, 0o000))
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "noop"})
	assert.Contains(t, out.Error, "permission denied", "an unreadable directory stops the copy: %+v", out)
}

// Keeping a deletion of a file that's already gone isn't an error.
func TestKeepChangesDeletedAlready(t *testing.T) {
	f := newWriterFixture(t, approveAll)
	cp, err := copyWorkspace(f.r.ws)
	require.NoError(t, err)
	defer cp.remove()
	written, err := cp.keepChanges(context.Background(), f.r.ws, f.r.hooks, "writer", []wsChange{{path: "never-there.txt", deleted: true}})
	require.NoError(t, err)
	assert.Equal(t, []string{"never-there.txt"}, written)
}
