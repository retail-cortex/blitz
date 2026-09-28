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
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func writePNG(t *testing.T, path string) {
	t.Helper()
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 12, 8)))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

func TestOneShotWithImage(t *testing.T) {
	e := testEnv(t)
	writePNG(t, filepath.Join(e.Tools().Workspace().Dir(), "ui.png"))
	llm := runtime.NewMockLLM("gemini-3.8-flash", genai.NewContentFromText("a button", genai.RoleModel))
	require.NoError(t, e.Engine().SetModel(context.Background(), llm))
	imgs, err := e.LoadAttachments([]string{"ui.png"}, "", func(string) {})
	require.NoError(t, err)
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	var out bytes.Buffer
	require.NoError(t, runOneShot(context.Background(), e, oneShotOptions{prompt: "what is this?", sessionID: sess.ID, format: formatText, stdout: &out, images: imgs}))
	last := llm.Requests[0].Contents[len(llm.Requests[0].Contents)-1]
	assert.Len(t, last.Parts, 2, "request parts: %+v", last.Parts)
	assert.NotNil(t, last.Parts[0].InlineData, "request parts: %+v", last.Parts)
	assert.Equal(t, "what is this?", last.Parts[1].Text, "request parts: %+v", last.Parts)
	assert.Contains(t, out.String(), "a button", "output: %s", out.String())
}

func TestImageFlagRejectsMissingFile(t *testing.T) {
	isolate(t)
	t.Setenv("GEMINI_API_KEY", "AIzaSyTESTKEY-1234567890abcdefghijklmnop")
	_, err := runCLI(t, "--image", "nope.png", "describe it")
	assert.Equal(t, exitUsage, exitCodeFor(err), "exit %d: %v", exitCodeFor(err), err)
	assert.Contains(t, err.Error(), "nope.png", "exit %d: %v", exitCodeFor(err), err)
}
