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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	cpsession "github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)))
	return buf.Bytes()
}

func imagesDir(t *testing.T) func(*config.Config) {
	dir := t.TempDir()
	return func(c *config.Config) { c.Images.Dir = dir }
}

// inlineImages returns the inline images in the last request the mock saw.
func inlineImages(m *MockLLM) []*genai.Blob {
	var out []*genai.Blob
	for _, c := range m.Requests[len(m.Requests)-1].Contents {
		for _, p := range c.Parts {
			if p.InlineData != nil {
				out = append(out, p.InlineData)
			}
		}
	}
	return out
}

// Attachments reach the model as image bytes, while the persisted session
// holds only the reference, so session files stay small and a resumed
// session still shows the picture.
func TestEngineAttachmentsStayOutOfSessionFiles(t *testing.T) {
	sessDir := t.TempDir()
	svc, _ := cpsession.NewPersistentService(sessDir)
	f := newEngineWith(t, fixtureOpts{cfg: imagesDir(t), opts: []Option{WithSessionService(svc)}}, textContent("a cat"), textContent("still a cat"))
	img, err := f.tools.AddImage("cat.png", testPNG(t, 64, 48))
	require.NoError(t, err)
	require.NoError(t, f.eng.Execute(context.Background(), "s", "what is this?", nil, WithAttachments(images.Part(img))))
	got := inlineImages(f.llm)
	require.Len(t, got, 1, "model did not receive the image: %+v", got)
	require.Equal(t, "image/png", got[0].MIMEType, "model did not receive the image: %+v", got)
	require.True(t, bytes.Equal(got[0].Data, img.Data), "model did not receive the image: %+v", got)
	// Image first, then the question.
	parts := f.llm.Requests[0].Contents[len(f.llm.Requests[0].Contents)-1].Parts
	assert.NotNil(t, parts[0].InlineData, "attachment order: %+v", parts)
	assert.Equal(t, "what is this?", parts[1].Text, "attachment order: %+v", parts)

	files, _ := filepath.Glob(filepath.Join(sessDir, "*"))
	var stored []byte
	for _, p := range files {
		b, _ := os.ReadFile(p)
		stored = append(stored, b...)
	}
	b64 := base64.StdEncoding.EncodeToString(img.Data)
	assert.False(t, bytes.Contains(stored, []byte(b64[:40])), "session files should hold the reference, not the bytes (%d bytes stored)", len(stored))
	assert.True(t, bytes.Contains(stored, []byte(img.URI())), "session files should hold the reference, not the bytes (%d bytes stored)", len(stored))

	// Resume in a new engine: history still includes the picture.
	svc2, _ := cpsession.NewPersistentService(sessDir)
	g := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) { c.Images.Dir = f.cfg.Images.Dir }, opts: []Option{WithSessionService(svc2)}}, textContent("yes"))
	require.NoError(t, g.eng.Execute(context.Background(), "s", "and now?", nil))
	got = inlineImages(g.llm)
	assert.Len(t, got, 1, "resumed history lost the image: %d", len(got))
}

// The model can ask to see a workspace image; the picture follows the tool
// result in the next request. Blocked and outside paths are refused.
func TestViewImageTool(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "private.png")
	os.WriteFile(outside, testPNG(t, 8, 8), 0o644)
	f := newEngineWith(t, fixtureOpts{cfg: imagesDir(t)},
		toolCall("view_image", map[string]any{"path": "ui/shot.png"}), textContent("I see it"),
		toolCall("view_image", map[string]any{"path": "notes.txt"}), textContent("not an image"),
		toolCall("view_image", map[string]any{"path": outside}), textContent("outside"),
		toolCall("view_image", map[string]any{"path": "id_rsa_backup.png"}), textContent("blocked"))
	ws := f.tools.Workspace().Dir()
	os.MkdirAll(filepath.Join(ws, "ui"), 0o755)
	os.WriteFile(filepath.Join(ws, "ui", "shot.png"), testPNG(t, 30, 20), 0o644)
	os.WriteFile(filepath.Join(ws, "notes.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(ws, "id_rsa_backup.png"), testPNG(t, 8, 8), 0o644) // matches a blocked pattern

	res, err := functionResponses(t, f.eng, "s", "look at the screenshot")
	require.NoError(t, err)
	r := res["view_image"]
	require.Nil(t, r["error"], "view_image result: %v", r)
	require.Equal(t, float64(30), r["width"], "view_image result: %v", r)
	require.True(t, strings.HasPrefix(fmt.Sprint(r["image_uri"]), images.URIScheme), "view_image result: %v", r)
	last := f.llm.Requests[len(f.llm.Requests)-1].Contents
	tool := last[len(last)-1]
	assert.Len(t, tool.Parts, 2, "image should follow the tool result: %+v", tool.Parts)
	assert.NotNil(t, tool.Parts[0].FunctionResponse, "image should follow the tool result: %+v", tool.Parts)
	assert.NotNil(t, tool.Parts[1].InlineData, "image should follow the tool result: %+v", tool.Parts)

	// In the order the mock issues the calls.
	for _, c := range []struct{ prompt, want string }{{"now the text file", "not a PNG"}, {"and outside", "outside"}, {"blocked one", "blocked"}} {
		prompt, want := c.prompt, c.want
		res, _ := functionResponses(t, f.eng, "s", prompt)
		msg := fmt.Sprint(res["view_image"]["error"])
		assert.Contains(t, msg, want, "%s: want an error mentioning %q, got %v", prompt, want, res["view_image"])
	}
}

func TestViewImageDisabled(t *testing.T) {
	off := func(c *config.Config) { c.Images.Enabled = false }
	f := newEngineWith(t, fixtureOpts{cfg: off})
	assert.Nil(t, f.tools.Images(), "view_image should not exist when images are disabled")
	assert.Len(t, f.tools.GetToolsForAgent([]string{"view_image"}), 0, "view_image should not exist when images are disabled")
	_, err := f.tools.AddImage("x.png", testPNG(t, 2, 2))
	assert.Error(t, err, "AddImage should fail when disabled")
}

func TestAnthropicImages(t *testing.T) {
	f, opts := newFake(t, jsonReply(message("end_turn", `{"type":"text","text":"ok"}`)))
	m := mustAnthropicModel(t, config.AnthropicConfig{APIKey: "sk-ant-test"}, "claude-opus-5", opts...)
	pic := testPNG(t, 4, 4)
	req := &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{{InlineData: &genai.Blob{Data: pic, MIMEType: "image/png"}}, genai.NewPartFromText("what?")}},
		{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "t1", Name: "view_image", Args: map[string]any{"path": "a.png"}}}}},
		{Role: genai.RoleUser, Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{ID: "t1", Name: "view_image", Response: map[string]any{"path": "a.png"}}},
			{InlineData: &genai.Blob{Data: pic, MIMEType: "image/png"}},
		}},
		{Role: genai.RoleUser, Parts: []*genai.Part{{InlineData: &genai.Blob{Data: []byte("%PDF"), MIMEType: "application/pdf"}}}},
	}}
	_, err := collectResponses(t, m, req, false)
	require.NoError(t, err)
	msgs, _ := json.Marshal(f.requests[0]["messages"])
	s := string(msgs)
	b64 := base64.StdEncoding.EncodeToString(pic)
	wantUser := `{"source":{"data":"` + b64 + `","media_type":"image/png","type":"base64"},"type":"image"},{"text":"what?","type":"text"}`
	assert.Contains(t, s, wantUser, "user image block missing or out of order:\n%s", s)
	// The tool's image is inside its tool_result, not a separate block.
	wantTool := `"content":[{"text":"{\"path\":\"a.png\"}","type":"text"},{"source":{"data":"` + b64
	assert.Contains(t, s, wantTool, "tool_result should contain the image:\n%s", s)
	assert.Contains(t, s, "application/pdf attachment omitted", "unsupported media should become a note:\n%s", s)
}

// fakeOpenAI records Responses API request bodies.
type fakeOpenAI struct {
	mu     sync.Mutex
	bodies []string
}

func (f *fakeOpenAI) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(b))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","model":"gpt-test","output":[{"type":"message","content":[{"type":"output_text","text":"seen"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenAIImages(t *testing.T) {
	f := &fakeOpenAI{}
	srv := f.server(t)
	m, err := newOpenAIModel(context.Background(), "gpt-test", "sk-test", srv.URL+"/v1", option.WithMaxRetries(0), option.WithRequestTimeout(10*time.Second))
	require.NoError(t, err)
	pic := testPNG(t, 4, 4)
	userParts := []*genai.Part{{InlineData: &genai.Blob{Data: pic, MIMEType: "image/png"}}, genai.NewPartFromText("what?")}
	req := &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: userParts},
		{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "c1", Name: "view_image", Args: map[string]any{"path": "a.png"}}}}},
		{Role: genai.RoleUser, Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{ID: "c1", Name: "view_image", Response: map[string]any{"path": "a.png"}}},
			{InlineData: &genai.Blob{Data: pic, MIMEType: "image/png"}},
		}},
	}}
	var text string
	for resp, err := range m.GenerateContent(context.Background(), req, false) {
		require.NoError(t, err)
		text += resp.Content.Parts[0].Text
	}
	assert.Equal(t, "seen", text, "reply %q", text)
	body := f.bodies[0]
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pic)
	assert.Equal(t, 2, strings.Count(body, `"type":"input_image"`), "expected two input_image items:\n%s", body)
	assert.Equal(t, 2, strings.Count(body, dataURL), "expected two input_image items:\n%s", body)
	assert.NotContains(t, body, "blitz-image", "a marker leaked to the provider:\n%s", body)
	assert.LessOrEqual(t, strings.Index(body, `"input_image"`), strings.Index(body, `"what?"`), "image should come before the question")
	// The caller's request is not modified.
	assert.NotNil(t, userParts[0].InlineData, "request contents were mutated")
	assert.Equal(t, "", userParts[0].Text, "request contents were mutated")

	// Without images, the body goes out as the ADK built it.
	plain := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	for _, err := range m.GenerateContent(context.Background(), plain, false) {
		require.NoError(t, err)
	}
	assert.NotContains(t, f.bodies[1], "input_image", "unexpected image in a plain request")
}

// A marker typed by the user (or quoted by a tool) is not a hook for
// injecting images: only markers created for this request are replaced.
func TestOpenAIMarkerNotSpoofable(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"` + imageMarkerPrefix + `deadbeef"}]}],"temperature":0.1}`)
	out, err := rewriteImageMarkers(body, openAIImages{imageMarkerPrefix + "other": "data:image/png;base64,AA=="})
	assert.NoError(t, err, "unknown marker rewritten: %s", out)
	assert.True(t, bytes.Equal(out, body), "unknown marker rewritten: %s %v", out, err)
}

// Images go through the real Gemini SDK (against a fake server), whose
// request checks a mock model skips: the Developer API rejects fields such
// as Blob.DisplayName before anything is sent.
func TestGeminiDeveloperAPIAcceptsImages(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"a cat"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()
	llm, err := gemini.NewModel(context.Background(), "gemini-3.8-flash", &genai.ClientConfig{
		APIKey: "test-key", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: srv.URL}})
	require.NoError(t, err)
	store, _ := images.OpenStore(t.TempDir())
	img, _ := images.Prepare("clipboard-120000.png", testPNG(t, 8, 8), images.Options{})
	store.Put(img)
	m := withImages(llm, store)

	req := &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{images.Part(img), genai.NewPartFromText("what is this?")}},
		{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "view_image", Args: map[string]any{"path": "a.png"}}}}},
		{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			Name: "view_image", Response: map[string]any{images.ToolResultKey: img.URI()}}}}},
	}}
	for _, err := range m.GenerateContent(context.Background(), req, false) {
		require.NoError(t, err, "Gemini rejected the request")
	}
	b64 := base64.StdEncoding.EncodeToString(img.Data)
	assert.Equal(t, 2, strings.Count(body, `"inlineData":{"data":"`+b64+`","mimeType":"image/png"}`), "expected two inline images in the request:\n%s", body)
	// References must never go out as file parts (the tool result's own
	// image_uri field is just text).
	assert.NotContains(t, body, "displayName", "display names or file references reached the API:\n%s", body)
	assert.NotContains(t, body, "fileData", "display names or file references reached the API:\n%s", body)
}

func TestImageInstructionInSystemPrompt(t *testing.T) {
	on := newEngineWith(t, fixtureOpts{cfg: imagesDir(t)}, textContent("ok"))
	runTurns(t, on.eng, "s", "hi")
	sys := systemText(on.llm)
	assert.Contains(t, sys, "You can see images", "image guidance missing:\n%s", sys)
	assert.Contains(t, sys, "call view_image", "image guidance missing:\n%s", sys)
	off := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) { c.Images.Enabled = false }}, textContent("ok"))
	runTurns(t, off.eng, "s", "hi")
	assert.NotContains(t, systemText(off.llm), "You can see images", "no image guidance when images are disabled")
}

// withImages leaves no model alone and never wraps twice; the OpenAI
// image helpers pass through what has no images and reject a body that
// isn't JSON.
func TestImageWrappersEdgeCases(t *testing.T) {
	assert.Nil(t, withImages(nil, nil))
	inner := NewMockLLM("m")
	once := withImages(inner, nil)
	twice := withImages(once, nil)
	assert.Same(t, inner, twice.(*imageModel).inner, "rewrapping unwraps first")

	ctx := context.Background()
	gotCtx, gotReq := replaceImagesWithMarkers(ctx, nil)
	assert.Equal(t, ctx, gotCtx)
	assert.Nil(t, gotReq)
	_, err := rewriteImageMarkers([]byte("not json"), openAIImages{})
	assert.Error(t, err)
}
