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
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The audit log proves which image was sent (path and SHA-256) without
// copying the picture into the log.
func TestLoadImageAuditsHashNotBytes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Dir = t.TempDir()
	cfg.Images.MaxInputMB = 1
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer reg.Close()
	logDir := t.TempDir()
	log, _ := audit.Open(logDir, nil)
	reg.SetAudit(log)

	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	os.WriteFile(filepath.Join(reg.Workspace().Dir(), "pic.png"), buf.Bytes(), 0o644)
	img, err := reg.LoadImage("pic.png")
	require.NoError(t, err)
	log.Close()
	files, _ := filepath.Glob(filepath.Join(logDir, "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	s := string(data)
	assert.Contains(t, s, `"kind":"attachment"`, "audit entry missing: %s", s)
	assert.Contains(t, s, "pic.png sha256="+img.SHA256, "audit entry missing: %s", s)
	assert.NotContains(t, s, base64.StdEncoding.EncodeToString(img.Data)[:24], "image bytes leaked into the audit log")

	// The input limit applies before reading the whole file.
	os.WriteFile(filepath.Join(reg.Workspace().Dir(), "huge.png"), make([]byte, 2<<20), 0o644)
	_, err = reg.LoadImage("huge.png")
	assert.Error(t, err, "size limit")
	assert.Contains(t, err.Error(), "limit", "size limit: %v", err)
}

// imageRegistry is a registry with images in their own store, and a PNG.
func imageRegistry(t *testing.T, enabled bool) (*Registry, []byte) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Enabled = enabled
	cfg.Images.Dir = filepath.Join(t.TempDir(), "images")
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 4))))
	writeFile(t, filepath.Join(reg.Workspace().Dir(), "pic.png"), buf.String())
	return reg, buf.Bytes()
}

// view_image describes the image it loads, or says why it can't.
func TestViewImageTool(t *testing.T) {
	reg, _ := imageRegistry(t, true)
	rt := toolOf(t)(NewViewImageTool(reg))
	writeFile(t, filepath.Join(reg.Workspace().Dir(), "notes.png"), "not a picture")
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"an image", "pic.png", ""},
		{"not an image", "notes.png", "cannot view image"},
		{"missing", "gone.png", "cannot view image"},
		{"outside the workspace", "/etc/hosts", "cannot view image"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runTool(t, rt, map[string]any{"path": tt.path})
			if tt.wantErr != "" {
				assert.Contains(t, errOf(out), tt.wantErr)
				return
			}
			assert.Empty(t, errOf(out))
			assert.Equal(t, "image/png", out["mime_type"])
			assert.EqualValues(t, 8, out["width"])
			assert.EqualValues(t, 4, out["height"])
			assert.NotEmpty(t, out["image_uri"])
		})
	}
}

// Clipboard images are stored like files; a store that can't be written
// fails the image.
func TestAddImage(t *testing.T) {
	reg, data := imageRegistry(t, true)
	img, err := reg.AddImage("clipboard", data)
	require.NoError(t, err)
	assert.Equal(t, 8, img.Width)
	assert.NotNil(t, reg.Images())
	_, err = reg.AddImage("clipboard", []byte("not a picture"))
	assert.Error(t, err)

	require.NoError(t, os.RemoveAll(reg.Images().Dir()))
	_, err = reg.AddImage("clipboard", append(data, 0)) // not stored yet
	assert.ErrorContains(t, err, "store image")
}

// With images off, nothing is loaded or added.
func TestImagesDisabled(t *testing.T) {
	reg, data := imageRegistry(t, false)
	assert.Nil(t, reg.Images())
	_, err := reg.LoadImage("pic.png")
	assert.ErrorIs(t, err, api.ErrImagesDisabled)
	_, err = reg.AddImage("clipboard", data)
	assert.ErrorIs(t, err, api.ErrImagesDisabled)
}
