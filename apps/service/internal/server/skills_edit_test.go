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

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Studio's skills over the API: create one, read it back with its hash,
// add a reference and a script (it becomes a scripts skill), rename it,
// copy a built-in in, refuse a stale hash and a path outside the folder,
// and delete it (SK-100–SK-103).
func TestSkillEditingOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	bad, err := w.SaveSkill(ctx, connect.NewRequest(&pb.SaveSkillRequest{Workspace: dir, Skill: &pb.SkillSpec{Name: "Bad Name"}}))
	require.NoError(t, err)
	assert.Len(t, bad.Msg.Problems, 2, "a bad name and no description")

	made, err := w.SaveSkill(ctx, connect.NewRequest(&pb.SaveSkillRequest{Workspace: dir, Skill: &pb.SkillSpec{Name: "release-notes", Description: "Write release notes", Tools: "read_file", Instructions: "Read the log."}}))
	require.NoError(t, err)
	require.Empty(t, made.Msg.Problems)
	f := made.Msg.File
	assert.Equal(t, filepath.Join(dir, ".agents", "skills", "release-notes", "SKILL.md"), f.Path)
	assert.True(t, f.Editable)
	assert.True(t, f.Skill.Project)
	assert.Equal(t, "simple", f.Skill.Kind)
	assert.Equal(t, "Read the log.", f.Spec.Instructions)

	written, err := w.WriteSkillFile(ctx, connect.NewRequest(&pb.WriteSkillFileRequest{Workspace: dir, Name: "release-notes", Path: "references/style.md", Content: []byte("Short lines.")}))
	require.NoError(t, err)
	assert.Equal(t, "references", written.Msg.File.Skill.Kind)
	assert.Equal(t, []string{"references/style.md"}, written.Msg.File.Skill.References)
	written, err = w.WriteSkillFile(ctx, connect.NewRequest(&pb.WriteSkillFileRequest{Workspace: dir, Name: "release-notes", Path: "scripts/count.py", Content: []byte("print(1)")}))
	require.NoError(t, err)
	assert.Equal(t, "scripts", written.Msg.File.Skill.Kind)
	require.Len(t, written.Msg.File.Skill.Scripts, 1)
	assert.True(t, written.Msg.File.Skill.Scripts[0].Found)
	read, err := w.ReadSkillFile(ctx, connect.NewRequest(&pb.ReadSkillFileRequest{Workspace: dir, Name: "release-notes", Path: "references/style.md"}))
	require.NoError(t, err)
	assert.Equal(t, "Short lines.", string(read.Msg.Content))
	assert.Equal(t, "INVALID_PATH", reason(t, unary(w.WriteSkillFile, &pb.WriteSkillFileRequest{Workspace: dir, Name: "release-notes", Path: "../escape.md"})))

	run, err := w.RunSkillScript(ctx, connect.NewRequest(&pb.RunSkillScriptRequest{Workspace: dir, Name: "release-notes", Script: "nope"}))
	require.NoError(t, err)
	assert.Contains(t, run.Msg.Error, "no script")

	assert.Equal(t, "HASH_MISMATCH", reason(t, unary(w.SaveSkill, &pb.SaveSkillRequest{Workspace: dir, PreviousName: "release-notes", Hash: "sha256:stale", Skill: &pb.SkillSpec{Name: "release-notes", Description: "d"}})))
	cur := written.Msg.File
	renamed, err := w.SaveSkill(ctx, connect.NewRequest(&pb.SaveSkillRequest{Workspace: dir, PreviousName: "release-notes", Hash: cur.Hash, Skill: &pb.SkillSpec{Name: "notes", Description: "Write notes", Members: []string{"other"}, Instructions: "Read the log."}}))
	require.NoError(t, err)
	require.Empty(t, renamed.Msg.Problems)
	assert.Equal(t, "meta", renamed.Msg.File.Skill.Kind)
	assert.FileExists(t, filepath.Join(dir, ".agents", "skills", "notes", "references", "style.md"))

	list, err := w.ListSkills(ctx, connect.NewRequest(&pb.ListSkillsRequest{Workspace: dir}))
	require.NoError(t, err)
	var builtin string
	for _, s := range list.Msg.Skills {
		if !s.Project {
			builtin = s.Name
			break
		}
	}
	require.NotEmpty(t, builtin)
	copied, err := w.CopySkill(ctx, connect.NewRequest(&pb.CopySkillRequest{Workspace: dir, From: builtin, Name: "my-copy"}))
	require.NoError(t, err)
	require.Empty(t, copied.Msg.Problems)
	assert.True(t, copied.Msg.File.Editable)
	assert.Equal(t, "my-copy", copied.Msg.File.Spec.Name)
	taken, err := w.CopySkill(ctx, connect.NewRequest(&pb.CopySkillRequest{Workspace: dir, From: builtin, Name: "notes"}))
	require.NoError(t, err)
	assert.Len(t, taken.Msg.Problems, 1)
	other, err := w.GetSkillFile(ctx, connect.NewRequest(&pb.GetSkillFileRequest{Workspace: dir, Name: builtin}))
	require.NoError(t, err)
	assert.False(t, other.Msg.File.Editable, "a built-in is copied, not edited")
	assert.Equal(t, "SKILL_NOT_FOUND", reason(t, unary(w.DeleteSkill, &pb.DeleteSkillRequest{Workspace: dir, Name: builtin})))

	gone, err := w.DeleteSkillFile(ctx, connect.NewRequest(&pb.DeleteSkillFileRequest{Workspace: dir, Name: "notes", Path: "references/style.md"}))
	require.NoError(t, err)
	assert.Empty(t, gone.Msg.File.Skill.References)
	require.NoError(t, unary(w.DeleteSkill, &pb.DeleteSkillRequest{Workspace: dir, Name: "notes"}))
	_, err = os.Stat(filepath.Join(dir, ".agents", "skills", "notes"))
	assert.True(t, os.IsNotExist(err))
	assert.Equal(t, "SKILL_NOT_FOUND", reason(t, unary(w.GetSkillFile, &pb.GetSkillFileRequest{Workspace: dir, Name: "notes"})))
}
