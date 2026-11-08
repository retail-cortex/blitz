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

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOpenAIImages answers OpenAI's image API as the SDK expects: n
// pictures for a generation (JSON) or an edit (multipart), recording each.
type fakeOpenAIImages struct {
	mu    sync.Mutex
	paths []string
	sizes []string
}

func (f *fakeOpenAIImages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n, size := 1, ""
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(8 << 20); err == nil {
			n, _ = strconv.Atoi(r.FormValue("n"))
			size = r.FormValue("size")
		}
	} else {
		var body struct {
			N    int    `json:"n"`
			Size string `json:"size"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		n, size = body.N, body.Size
	}
	f.mu.Lock()
	f.paths, f.sizes = append(f.paths, r.URL.Path), append(f.sizes, size)
	f.mu.Unlock()
	var data []map[string]string
	for i := range max(n, 1) {
		data = append(data, map[string]string{"b64_json": base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("\x89PNG\r\n\x1a\npicture %d", i+1)))})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"created": 1, "data": data, "usage": map[string]int{"input_tokens": 10, "output_tokens": 100, "total_tokens": 110}})
}

// blitz picture makes a Pictures turn through the service, saves its
// pictures and keeps the session (spec_058 PIC-80).
func TestPictureCommand(t *testing.T) {
	home := isolate(t)
	ws, err := os.Getwd()
	require.NoError(t, err)

	// The settings before the service first opens the workspace.
	fake := &fakeOpenAIImages{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	settings := fmt.Sprintf("[images]\nmodel = \"openai/gpt-image-2\"\n\n[llm.openai]\nbase_url = %q\n", srv.URL+"/v1")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte(settings), 0o600))

	out, err := runCLI(t, "picture", "models")
	require.NoError(t, err, out)
	assert.Regexp(t, `openai/gpt-image-2 \*\s+1:1 3:2 2:3`, out, "the [images] model, marked")

	out, err = runCLI(t, "picture", "-n", "2", "--ratio", "3:2", "-o", "art/fox.png", "a red", "fox")
	require.NoError(t, err, out)
	assert.Contains(t, out, "art/fox.png\n")
	assert.Contains(t, out, "art/fox-2.png\n")
	assert.Regexp(t, `Cost \$0\.080 · Pictures session pic-[0-9a-f]{12}`, out)
	for _, p := range []string{"art/fox.png", "art/fox-2.png"} {
		data, err := os.ReadFile(filepath.Join(ws, p))
		require.NoError(t, err, p)
		assert.True(t, strings.HasPrefix(string(data), "\x89PNG"))
	}
	assert.Equal(t, []string{"/v1/images/generations"}, fake.paths)
	assert.Equal(t, "1536x1024", fake.sizes[0], "the ratio as OpenAI's size")

	// The prompt from stdin, a reference with its role: an edit, saved in the pictures folder.
	out, err = runCLIWith(t, "make it a watercolour\n", "picture", "-", "--ref", "art/fox.png:style")
	require.NoError(t, err, out)
	assert.Equal(t, "/v1/images/edits", fake.paths[1])
	assert.Regexp(t, `pictures/make-it-a-watercolour[^\n]*\.png\n`, out)
	session := regexp.MustCompile(`pic-[0-9a-f]{12}`).FindString(out)

	// Continuing a session adds a turn to it.
	out, err = runCLI(t, "picture", "--continue", session, "-o", "art/again.png", "once more")
	require.NoError(t, err, out)
	assert.Contains(t, out, session)

	out, err = runCLI(t, "picture", "list")
	require.NoError(t, err, out)
	assert.Contains(t, out, session+"\tmake it a watercolour\tturns: 2\n")
	assert.Equal(t, 2, strings.Count(out, "pic-"))

	out, err = runCLI(t, "picture", "delete", session)
	require.NoError(t, err, out)
	assert.Contains(t, out, "Deleted Pictures session "+session)
	out, _ = runCLI(t, "picture", "list")
	assert.Equal(t, 1, strings.Count(out, "pic-"))
	_, err = runCLI(t, "picture", "delete", "pic-000000000000")
	assert.Equal(t, exitFailure, exitCodeFor(err))

	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"a shape the model doesn't make", []string{"--ratio", "8:1", "x"}, exitFailure, "doesn't make 8:1 pictures"},
		{"a seed it doesn't take", []string{"--seed", "7", "x"}, exitFailure, "doesn't take a seed"},
		{"too many", []string{"-n", "9", "x"}, exitFailure, "at most 4"},
		{"a search that isn't one", []string{"--search=maps", "x"}, exitUsage, "--search is web or image"},
		{"a reference outside", []string{"--ref", "../far.png", "x"}, exitUsage, "outside the workspace"},
		{"saved outside", []string{"-o", "../far.png", "x"}, exitUsage, "outside the workspace"},
		{"a model not offered", []string{"-m", "openai/gpt-4o", "x"}, exitFailure, "image model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCLI(t, append([]string{"picture"}, tc.args...)...)
			assert.Equal(t, tc.code, exitCodeFor(err), "%v", err)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// Without a prompt or an image model, blitz picture says what to do.
func TestPictureCommandWithoutModels(t *testing.T) {
	isolate(t)
	_, err := runCLIWith(t, "", "picture")
	assert.Equal(t, exitUsage, exitCodeFor(err), "no prompt: %v", err)
	assert.ErrorContains(t, err, "Say what to draw")
	_, err = runCLI(t, "picture", "a fox")
	assert.Equal(t, exitUsage, exitCodeFor(err), "no image model offered: %v", err)
	assert.ErrorContains(t, err, "No image model is offered")
	out, err := runCLI(t, "picture", "models")
	require.NoError(t, err)
	assert.Contains(t, out, "No image model is offered")
	out, err = runCLI(t, "picture", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No Pictures sessions yet")
}

// A settings file that doesn't read stops each picture and data command
// before it reaches the service.
func TestPictureAndDataBrokenSettings(t *testing.T) {
	home := isolate(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[llm\nprovider = \n"), 0o600))
	for _, args := range [][]string{
		{"picture", "a fox"},
		{"picture", "models"},
		{"picture", "list"},
		{"picture", "delete", "pic-000000000000"},
		{"data", "list"},
		{"data", "query", "kitchen", "SELECT 1"},
	} {
		_, err := runCLI(t, args...)
		assert.Error(t, err, strings.Join(args, " "))
	}
}

// The helpers: where a numbered picture goes, and the default model.
func TestPictureHelpers(t *testing.T) {
	for _, tc := range []struct {
		out  string
		i    int
		want string
	}{{"", 2, ""}, {"a/b.png", 0, "a/b.png"}, {"a/b.png", 1, "a/b-2.png"}, {"b", 2, "b-3"}} {
		assert.Equal(t, tc.want, numbered(tc.out, tc.i))
	}
	assert.Empty(t, defaultPictureModel(nil))
	dir := t.TempDir()
	_, err := workspacePath(dir, filepath.Join(dir, "..", "x.png"))
	assert.Error(t, err)
	rel, err := workspacePath(dir, filepath.Join(dir, "sub", "x.png"))
	require.NoError(t, err)
	assert.Equal(t, "sub/x.png", rel)
}
