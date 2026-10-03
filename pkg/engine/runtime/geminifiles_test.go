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

package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// fakeFiles is a Files API: each upload gets a name, and is processing
// for gets polls before it's active (or failed).
type fakeFiles struct {
	uploads    []string // the bytes of each upload
	processing int
	fail       bool
	uploadErr  error
}

func (f *fakeFiles) Upload(_ context.Context, r io.Reader, cfg *genai.UploadFileConfig) (*genai.File, error) {
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	b, _ := io.ReadAll(r)
	f.uploads = append(f.uploads, string(b))
	state := genai.FileStateActive
	if f.processing > 0 {
		state = genai.FileStateProcessing
	}
	return &genai.File{Name: "files/1", URI: "https://g/files/1", MIMEType: cfg.MIMEType, State: state}, nil
}

func (f *fakeFiles) Get(context.Context, string, *genai.GetFileConfig) (*genai.File, error) {
	f.processing--
	switch {
	case f.processing > 0:
		return &genai.File{Name: "files/1", State: genai.FileStateProcessing}, nil
	case f.fail:
		return &genai.File{Name: "files/1", State: genai.FileStateFailed, Error: &genai.FileStatus{Message: "bad codec"}}, nil
	}
	return &genai.File{Name: "files/1", URI: "https://g/files/1", State: genai.FileStateActive}, nil
}

func TestGeminiUploader(t *testing.T) {
	saved := []any{pollEvery, nowForUpload}
	t.Cleanup(func() { pollEvery, nowForUpload = saved[0].(time.Duration), saved[1].(func() time.Time) })
	pollEvery = time.Millisecond
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	nowForUpload = func() time.Time { return now }

	dir := t.TempDir()
	sha := strings.Repeat("e", 64)
	path := filepath.Join(dir, sha+".mp4")
	require.NoError(t, os.WriteFile(path, []byte("VIDEO"), 0o600))

	f := &fakeFiles{processing: 2}
	up := newGeminiUploader(f)
	fd, err := up(context.Background(), path, "video/mp4", sha)
	require.NoError(t, err)
	assert.Equal(t, "https://g/files/1", fd.FileURI)
	assert.Equal(t, "video/mp4", fd.MIMEType)
	assert.Equal(t, []string{"VIDEO"}, f.uploads, "uploaded, then waited for until active")
	assert.FileExists(t, filepath.Join(dir, sha+".gemini.json"))

	_, err = up(context.Background(), path, "video/mp4", sha)
	require.NoError(t, err)
	assert.Len(t, f.uploads, 1, "kept: not uploaded again")

	now = now.Add(48 * time.Hour)
	_, err = up(context.Background(), path, "video/mp4", sha)
	require.NoError(t, err)
	assert.Len(t, f.uploads, 2, "expired: uploaded again")

	failing := newGeminiUploader(&fakeFiles{processing: 1, fail: true})
	_, err = failing(context.Background(), path, "video/mp4", strings.Repeat("f", 64))
	assert.ErrorContains(t, err, "couldn't process the file: bad codec")

	refused := newGeminiUploader(&fakeFiles{uploadErr: errors.New("403")})
	_, err = refused(context.Background(), path, "video/mp4", strings.Repeat("a", 64))
	assert.ErrorContains(t, err, "uploading to Gemini: 403")

	_, err = up(context.Background(), filepath.Join(dir, "gone.mp4"), "video/mp4", strings.Repeat("b", 64))
	assert.ErrorIs(t, err, os.ErrNotExist)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := newGeminiUploader(&fakeFiles{processing: 5})
	_, err = slow(ctx, path, "video/mp4", strings.Repeat("c", 64))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGeminiUploaderTimesOut(t *testing.T) {
	saved := []any{pollEvery, pollFor}
	t.Cleanup(func() { pollEvery, pollFor = saved[0].(time.Duration), saved[1].(time.Duration) })
	pollEvery, pollFor = time.Millisecond, 5*time.Millisecond
	path := filepath.Join(t.TempDir(), "v.mp4")
	require.NoError(t, os.WriteFile(path, []byte("V"), 0o600))
	_, err := newGeminiUploader(&fakeFiles{processing: 1 << 30})(context.Background(), path, "video/mp4", strings.Repeat("d", 64))
	assert.ErrorContains(t, err, "still being processed")
}

func TestNewGeminiUploader(t *testing.T) {
	tests := []struct {
		name string
		cfg  func(*config.Config)
		want bool
	}{
		{"an API key", func(c *config.Config) { c.LLM.Gemini.APIKey = "k" }, true},
		{"no Gemini", func(c *config.Config) {}, false},
		{"Vertex AI", func(c *config.Config) { c.LLM.Gemini.APIKey, c.LLM.Gemini.Auth = "k", config.AuthADC }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.LLM.Gemini.APIKey = ""
			tc.cfg(cfg)
			up, err := NewGeminiUploader(context.Background(), cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.want, up != nil)
		})
	}
}
