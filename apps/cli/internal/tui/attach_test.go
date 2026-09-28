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

package tui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)))
	return buf.Bytes()
}

func newImageApp(t *testing.T, replies ...string) (*App, *runtime.MockLLM) {
	t.Helper()
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Images.Dir = t.TempDir()
	cfg.Audit.Enabled = false
	var contents []*genai.Content
	for _, r := range replies {
		contents = append(contents, genai.NewContentFromText(r, genai.RoleModel))
	}
	llm := runtime.NewMockLLM("gemini-3.8-flash", contents...)
	app := openApp(t, cfg, llm)
	local(app).Storage().CreateSession("", "t", "blitz")
	return app, llm
}

func sentImages(llm *runtime.MockLLM) int {
	n := 0
	last := llm.Requests[len(llm.Requests)-1].Contents
	for _, p := range last[len(last)-1].Parts {
		if p.InlineData != nil {
			n++
		}
	}
	return n
}

func TestAttachCommandsAndMentions(t *testing.T) {
	app, llm := newImageApp(t, "one", "two", "three")
	ws := local(app).Tools().Workspace().Dir()
	os.WriteFile(filepath.Join(ws, "a.png"), pngOf(t, 40, 30), 0o644)
	os.WriteFile(filepath.Join(ws, "b.png"), pngOf(t, 50, 30), 0o644)
	ctx := context.Background()

	out := captureStdout(t, func() {
		HandleCommand(ctx, "/attach", app)        // usage
		HandleCommand(ctx, "/attach @a.png", app) // queued
		HandleCommand(ctx, "/attach a.png", app)  // duplicate
		HandleCommand(ctx, "/attach missing.png", app)
		HandleCommand(ctx, "/attach", app) // lists
	})
	for _, want := range []string{"Usage: /attach <image>", "a.png 40×30", "will be sent with your next message", "already queued", "Could not attach missing.png", "1 image waiting"} {
		assert.Contains(t, out, want, "output lacks %q:\n%s", want, out)
	}

	// Queue plus an inline mention: both go with the prompt, then the
	// queue is empty.
	out = captureStdout(t, func() {
		runTurn(ctx, app, local(app).Storage().Active().ID, "compare these with @b.png", nil, turnOptions{})
	})
	n := sentImages(llm)
	assert.Equal(t, 2, n, "sent %d images, want 2", n)
	assert.Len(t, app.Attachments, 0, "queue not emptied / not shown:\n%s", out)
	assert.Contains(t, out, "b.png 50×30", "queue not emptied / not shown:\n%s", out)
	msgs := local(app).Storage().Active().Messages
	last := msgs[len(msgs)-2].Content
	assert.Contains(t, last, "[images: a.png, b.png]", "transcript note missing: %q", last)

	// A bad mention stops the turn and keeps the queue.
	HandleCommand(ctx, "/attach a.png", app)
	calls := len(llm.Requests)
	out = captureStdout(t, func() {
		runTurn(ctx, app, local(app).Storage().Active().ID, "what about @nope.png", nil, turnOptions{})
	})
	assert.Len(t, llm.Requests, calls, "bad mention should not send (calls %d→%d, queue %d):\n%s", calls, len(llm.Requests), len(app.Attachments), out)
	assert.Len(t, app.Attachments, 1, "bad mention should not send (calls %d→%d, queue %d):\n%s", calls, len(llm.Requests), len(app.Attachments), out)
	assert.Contains(t, out, "Nothing was sent", "bad mention should not send (calls %d→%d, queue %d):\n%s", calls, len(llm.Requests), len(app.Attachments), out)
	out = captureStdout(t, func() { HandleCommand(ctx, "/attach clear", app) })
	assert.Len(t, app.Attachments, 0, "clear: %s", out)
	assert.Contains(t, out, "Removed 1 queued image.", "clear: %s", out)

	// Plain prompts send no images.
	runTurn(ctx, app, local(app).Storage().Active().ID, "just text, mail me@example.png", nil, turnOptions{})
	n = sentImages(llm)
	assert.Equal(t, 0, n, "plain prompt sent %d images", n)
}

func TestAttachRespectsSandbox(t *testing.T) {
	app, _ := newImageApp(t)
	outside := filepath.Join(t.TempDir(), "secret.png")
	os.WriteFile(outside, pngOf(t, 4, 4), 0o644)
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/attach "+outside, app) })
	assert.Len(t, app.Attachments, 0, "outside file attached:\n%s", out)
	assert.Contains(t, out, "Could not attach", "outside file attached:\n%s", out)
}

func TestPaste(t *testing.T) {
	defer func(f func(context.Context) ([]byte, error)) { images.ReadClipboard = f }(images.ReadClipboard)
	app, llm := newImageApp(t, "a screenshot")
	ctx := context.Background()

	images.ReadClipboard = func(context.Context) ([]byte, error) { return nil, images.ErrNoClipboardImage }
	out := captureStdout(t, func() { HandleCommand(ctx, "/paste", app) })
	assert.Contains(t, out, "clipboard has no image", "empty clipboard: %s", out)
	assert.Len(t, app.Attachments, 0, "empty clipboard: %s", out)
	images.ReadClipboard = func(context.Context) ([]byte, error) { return []byte("plain text"), nil }
	out = captureStdout(t, func() { HandleCommand(ctx, "/paste", app) })
	assert.Contains(t, out, "not a PNG", "non-image clipboard: %s", out)
	images.ReadClipboard = func(context.Context) ([]byte, error) { return pngOf(t, 20, 10), nil }
	out = captureStdout(t, func() { HandleCommand(ctx, "/paste", app) })
	require.Len(t, app.Attachments, 1, "paste: %s", out)
	require.True(t, strings.HasPrefix(app.Attachments[0].Name, "clipboard-"), "paste: %s", out)
	runTurn(ctx, app, local(app).Storage().Active().ID, "what is this?", nil, turnOptions{})
	assert.Equal(t, 1, sentImages(llm), "pasted image not sent")

	images.ReadClipboard = func(context.Context) ([]byte, error) { return nil, errors.New("osascript exploded") }
	out = captureStdout(t, func() { HandleCommand(ctx, "/paste", app) })
	assert.Contains(t, out, "osascript exploded", "unexpected errors should be shown: %s", out)
}

func TestAttachDisabled(t *testing.T) {
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Images.Enabled = false
	app := openApp(t, cfg, runtime.NewMockLLM("m"))
	out := captureStdout(t, func() {
		HandleCommand(context.Background(), "/paste", app)
		HandleCommand(context.Background(), "/attach x.png", app)
	})
	assert.Contains(t, out, "Images are disabled", "disabled: %s", out)
	assert.Contains(t, out, "image support is disabled", "disabled: %s", out)
}
