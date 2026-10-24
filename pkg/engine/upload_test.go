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

package engine

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hiddenUploads lists the upload files left in dir.
func hiddenUploads(t *testing.T, dir string) []string {
	t.Helper()
	var left []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), ".tmp-") {
			left = append(left, p)
		}
		return err
	}))
	return left
}

// A dropped file arrives in chunks and appears only with its last one
// (FIL-22).
func TestUploadFile(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		existing string // already at the path
		chunks   []string
		replace  bool
		want     string // the file after; "" when the upload fails
		err      error
	}{
		{name: "one chunk", chunks: []string{"hello"}, want: "hello"},
		{name: "several chunks", chunks: []string{"he", "ll", "o"}, want: "hello"},
		{name: "an empty file", chunks: []string{""}, want: ""},
		{name: "over a file", existing: "old", chunks: []string{"new"}, err: fs.ErrExist},
		{name: "replacing a file", existing: "old", chunks: []string{"n", "ew"}, replace: true, want: "new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := openTest(t)
			path := "in/a folder/a.bin"
			if tt.existing != "" {
				write(t, w.Dir(), path, tt.existing)
			}
			id := ""
			var version string
			var err error
			for i, c := range tt.chunks {
				done := i == len(tt.chunks)-1
				id, version, err = w.UploadFile(ctx, path, id, []byte(c), done, tt.replace)
				if !done {
					require.NoError(t, err)
					require.NotEmpty(t, id)
					assert.Empty(t, version, "no version before the last chunk")
					_, statErr := os.Stat(filepath.Join(w.Dir(), filepath.FromSlash(path)))
					assert.Equal(t, tt.existing != "", statErr == nil, "the file isn't there until done")
				}
			}
			got, _ := os.ReadFile(filepath.Join(w.Dir(), filepath.FromSlash(path)))
			if tt.err != nil {
				assert.ErrorIs(t, err, tt.err)
				assert.Equal(t, tt.existing, string(got), "the file is untouched")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(got))
				assert.Equal(t, FileVersion([]byte(tt.want)), version)
			}
			assert.Empty(t, hiddenUploads(t, w.Dir()), "no upload file is left")
			if tt.err == nil {
				_, _, err = w.UploadFile(ctx, path, id, []byte("more"), true, true)
				assert.ErrorIs(t, err, ErrNoUpload, "a finished upload takes no more")
			}
		})
	}
}

func TestUploadFileRefuses(t *testing.T) {
	ctx := context.Background()
	w := openTest(t)
	require.NoError(t, w.CreateFolder("docs"))
	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"outside the workspace", func() error { _, _, err := w.UploadFile(ctx, "../a", "", nil, true, false); return err }, ErrBadPath},
		{"in .git", func() error { _, _, err := w.UploadFile(ctx, ".git/a", "", nil, true, false); return err }, ErrBadPath},
		{"an unknown upload", func() error { _, _, err := w.UploadFile(ctx, "a", "nope", nil, true, false); return err }, ErrNoUpload},
		{"another path's upload", func() error {
			id, _, err := w.UploadFile(ctx, "a", "", []byte("x"), false, false)
			require.NoError(t, err)
			_, _, err = w.UploadFile(ctx, "b", id, []byte("x"), true, false)
			return err
		}, ErrNoUpload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, tt.call(), tt.want)
		})
	}
	_, _, err := w.UploadFile(ctx, "docs", "", []byte("x"), true, true)
	assert.ErrorContains(t, err, "is a folder", "a folder is never replaced")
	info, err := os.Stat(filepath.Join(w.Dir(), "docs"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// An upload left without a chunk is dropped, and the rest go with the
// workspace (Close drops them all): no hidden files stay behind.
func TestUnfinishedUploadsAreDropped(t *testing.T) {
	ctx := context.Background()
	w := openTest(t)
	old := uploadIdle
	uploadIdle = time.Millisecond
	t.Cleanup(func() { uploadIdle = old })
	stale, _, err := w.UploadFile(ctx, "stale.bin", "", []byte("x"), false, false)
	require.NoError(t, err)
	require.Len(t, hiddenUploads(t, w.Dir()), 1)
	time.Sleep(5 * time.Millisecond)
	_, _, err = w.UploadFile(ctx, "fresh.bin", "", []byte("y"), false, false)
	require.NoError(t, err)
	_, _, err = w.UploadFile(ctx, "stale.bin", stale, []byte("x"), true, false)
	assert.ErrorIs(t, err, ErrNoUpload, "the idle upload was dropped")
	assert.Len(t, hiddenUploads(t, w.Dir()), 1, "only the fresh upload's file is left")

	w.dropUploads(time.Time{}) // as Close does
	assert.Empty(t, hiddenUploads(t, w.Dir()), "every unfinished upload is dropped")
}
