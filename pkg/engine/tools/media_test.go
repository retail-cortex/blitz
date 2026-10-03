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
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewMedia(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	store, err := images.OpenStore(t.TempDir())
	require.NoError(t, err)
	var checked []string
	r := &Registry{workspace: ws, hooks: allowAll(), images: store, imageOpts: images.Options{MaxInput: 20 << 20}}
	r.SetMediaCheck(func(name, mime string, size int64) error {
		checked = append(checked, mime)
		if mime == "video/quicktime" {
			return errors.New("the model can't take video")
		}
		return nil
	})
	mp4 := make([]byte, 64)
	binary.BigEndian.PutUint32(mp4, 16)
	copy(mp4[4:], "ftypisom")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "talk.mp4"), mp4, 0o644))
	mov := append([]byte{}, mp4...)
	copy(mov[8:], "qt  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "demo.mov"), mov, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# x"), 0o644))
	rt := toolOf(t)(NewViewMediaTool(r))

	out := runTool(t, rt, map[string]any{"path": "talk.mp4"})
	require.Empty(t, errOf(out))
	assert.Equal(t, "video/mp4", out["mime_type"])
	assert.EqualValues(t, 64, out["bytes"])
	assert.Contains(t, out[images.ToolResultKey], images.URIScheme)
	assert.Equal(t, []string{"video/mp4"}, checked, "checked before it's stored")

	assert.Contains(t, errOf(runTool(t, rt, map[string]any{"path": "demo.mov"})), "can't take video")
	assert.Contains(t, errOf(runTool(t, rt, map[string]any{"path": "notes.md"})), "isn't audio or video")
	assert.Contains(t, errOf(runTool(t, rt, map[string]any{"path": "../x.mp4"})), "cannot open it")
}
