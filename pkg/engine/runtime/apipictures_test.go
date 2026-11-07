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
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// orgPNG is enough of a PNG for content sniffing.
var orgPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")

// gwPicture is the organization's gateway answering with a line of text
// and a picture (the enterprise gateway's GW-04b).
var gwPicture = `{"id":"resp_1","object":"response","status":"completed","model":"fast","output":[
	{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"A fox.","annotations":[]}]},
	{"type":"image_generation_call","id":"ig_1","status":"completed","result":"` + base64.StdEncoding.EncodeToString(orgPNG) + `","output_format":"png"}],
	"usage":{"input_tokens":10,"output_tokens":1120,"total_tokens":1130}}`

// gwPictureStream is the same answer streamed, in the gateway's events.
func gwPictureStream() string {
	one := strings.Join(strings.Fields(gwPicture), " ")
	item := `{"type":"image_generation_call","id":"ig_1","status":"completed","result":"` + base64.StdEncoding.EncodeToString(orgPNG) + `","output_format":"png"}`
	events := []string{
		`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","status":"in_progress","model":"fast","output":[]}}`,
		`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
		`{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"A fox."}`,
		`{"type":"response.output_item.done","sequence_number":3,"output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"A fox.","annotations":[]}]}}`,
		`{"type":"response.output_item.added","sequence_number":4,"output_index":1,"item":{"type":"image_generation_call","id":"ig_1","status":"in_progress"}}`,
		`{"type":"response.output_item.done","sequence_number":5,"output_index":1,"item":` + item + `}`,
		`{"type":"response.completed","sequence_number":6,"response":` + one + `}`,
	}
	var b strings.Builder
	for _, e := range events {
		typ := strings.TrimSuffix(strings.TrimPrefix(e[:strings.Index(e, `","`)], `{"type":"`), `"`)
		b.WriteString("event: " + typ + "\ndata: " + e + "\n\n")
	}
	return b.String()
}

// parts are the answer's parts, streamed or not: the last whole response's.
func answerParts(t *testing.T, m model.LLM, stream bool) []*genai.Part {
	var last *model.LLMResponse
	for resp, err := range m.GenerateContent(inSession("sess-p"), helloRequest(), stream) {
		require.NoError(t, err)
		if resp != nil && !resp.Partial {
			last = resp
		}
	}
	require.NotNil(t, last)
	return last.Content.Parts
}

// A picture an organization's model draws in the conversation reaches it
// as a picture, whole or streamed (MDL-95a): the adapter would refuse it.
func TestOrgModelPictures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  string
		stream bool
	}{
		{"whole", gwPicture, false},
		{"streamed", gwPictureStream(), true},
		{"a stream asked for, answered whole", gwPicture, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newOrgService(t, [2]any{http.StatusOK, tc.reply})
			m, err := NewModel(context.Background(), signedIn(t, s), "fast")
			require.NoError(t, err)
			parts := answerParts(t, m, tc.stream)
			var text string
			var pics []*genai.Blob
			for _, p := range parts {
				if p.InlineData != nil {
					pics = append(pics, p.InlineData)
				} else {
					text += p.Text
				}
			}
			assert.Equal(t, "A fox.", strings.TrimSpace(text), "no marker left")
			require.Len(t, pics, 1)
			assert.Equal(t, "image/png", pics[0].MIMEType)
			assert.Equal(t, orgPNG, pics[0].Data)
		})
	}
}

func TestMadePicturesRestore(t *testing.T) {
	_, mp := withMadePictures(context.Background())
	msg := mp.keep(map[string]json.RawMessage{}) // not an item
	assert.Nil(t, msg)
	resp := &model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
		{Text: "before " + pictureMarker + "x\u2063 after " + pictureMarker + "gone\u2063"},
		{Text: "thinking " + pictureMarker, Thought: true},
	}}}
	mp.byID["x"] = &genai.Blob{MIMEType: "image/png", Data: orgPNG}
	mp.restore(resp)
	parts := resp.Content.Parts
	require.Len(t, parts, 4)
	assert.Equal(t, "before ", parts[0].Text)
	assert.Equal(t, orgPNG, parts[1].InlineData.Data)
	assert.Equal(t, " after ", parts[2].Text, "an unknown picture is left out")
	assert.True(t, parts[3].Thought, "thoughts untouched")
	mp.restore(nil)

	// Formats, and a result that isn't a picture.
	for format, want := range map[string]string{"": "image/png", "jpg": "image/jpeg", "webp": "image/webp"} {
		id := "f-" + format
		item := map[string]json.RawMessage{"type": json.RawMessage(`"image_generation_call"`), "id": json.RawMessage(`"` + id + `"`),
			"result": json.RawMessage(`"` + base64.StdEncoding.EncodeToString(orgPNG) + `"`), "output_format": json.RawMessage(`"` + format + `"`)}
		require.NotNil(t, mp.keep(item))
		assert.Equal(t, want, mp.byID[id].MIMEType, format)
	}
	assert.Nil(t, mp.keep(map[string]json.RawMessage{"type": json.RawMessage(`"image_generation_call"`), "result": json.RawMessage(`"%%%"`)}))
	assert.Equal(t, []byte(`{"x":1}`), mp.rewriteJSON([]byte(`{"x":1}`)), "nothing to swap")
	assert.Equal(t, []byte(`image_generation_call`), mp.rewriteJSON([]byte(`image_generation_call`)), "not JSON")
}

// The SSE rewriter passes every other line through, CRLFs too.
func TestSSERewriter(t *testing.T) {
	in := "event: x\r\ndata: a\r\n\r\n: comment\ndata:b\n\n"
	r := &sseRewriter{r: bufio.NewReader(strings.NewReader(in)), c: io.NopCloser(nil), f: func(b []byte) []byte { return []byte(strings.ToUpper(string(b))) }}
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "event: x\r\ndata: A\r\n\r\n: comment\ndata: B\n\n", string(out))
	assert.NoError(t, r.Close())
}

// generate_image through the organization's service: an image_generation
// tool, the pictures to start from as data URLs, the picture back.
func TestOrgPainter(t *testing.T) {
	s := newOrgService(t, [2]any{http.StatusOK, gwPicture})
	cfg := signedIn(t, s)
	cfg.Images.Model = "enterprise/fast"
	p, err := NewPainter(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "enterprise/fast", p.Model())
	assert.Equal(t, "127.0.0.1", p.Host())

	pic, err := p.Paint(inSession("sess-i"), tools.PaintRequest{Prompt: "a fox", References: []tools.Reference{{MIME: "image/png", Data: orgPNG}}, AspectRatio: "16:9", Size: "2K"})
	require.NoError(t, err)
	assert.Equal(t, orgPNG, pic.Data)
	assert.Equal(t, "image/png", pic.MIME)
	assert.Equal(t, "A fox.", pic.Text)
	assert.EqualValues(t, 1120, pic.Usage.CandidatesTokenCount)

	got := s.g.requests()
	require.Len(t, got, 1)
	assert.Equal(t, "/api/v1/responses", got[0].Path)
	assert.Equal(t, "Bearer at-1", got[0].Header.Get("Authorization"))
	assert.Equal(t, "fast", got[0].Body["model"])
	assert.Equal(t, true, got[0].Body["stream"], "streamed, so a slow picture isn't cut off; read whole")
	assert.Equal(t, []any{map[string]any{"type": "image_generation", "aspect_ratio": "16:9", "image_size": "2K"}}, got[0].Body["tools"])
	content := got[0].Body["input"].([]any)[0].(map[string]any)["content"].([]any)
	assert.Equal(t, "a fox", content[0].(map[string]any)["text"])
	assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(orgPNG), content[1].(map[string]any)["image_url"])

	for _, tc := range []struct {
		name   string
		status int
		reply  string
		want   string
	}{
		{"refused", http.StatusOK, `{"status":"failed","error":{"code":"empty_output","message":"The model answered with nothing the service can pass on (it stopped: IMAGE_SAFETY)."},"output":[]}`, "it stopped: IMAGE_SAFETY"},
		{"no picture", http.StatusOK, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"I can't draw that."}]}]}`, "without a picture (I can't draw that.)"},
		{"an HTTP error", http.StatusTooManyRequests, `{"error":{"message":"Your budget is spent."}}`, "Your budget is spent."},
		{"not JSON", http.StatusOK, `<html>`, "couldn't be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newOrgService(t, [2]any{tc.status, tc.reply})
			cfg := signedIn(t, s)
			cfg.Images.Model = "enterprise/fast"
			p, err := NewPainter(context.Background(), cfg)
			require.NoError(t, err)
			_, err = p.Paint(inSession("sess-e"), tools.PaintRequest{Prompt: "x"})
			assert.ErrorContains(t, err, tc.want)
		})
	}
	t.Run("a streamed answer", func(t *testing.T) {
		s := newOrgService(t, [2]any{http.StatusOK, gwPictureStream()})
		cfg := signedIn(t, s)
		cfg.Images.Model = "enterprise/fast"
		p, err := NewPainter(context.Background(), cfg)
		require.NoError(t, err)
		pic, err := p.Paint(inSession("sess-s"), tools.PaintRequest{Prompt: "x"})
		require.NoError(t, err)
		assert.Equal(t, orgPNG, pic.Data)
	})
	t.Run("models that can't draw", func(t *testing.T) {
		s := newOrgService(t)
		cfg := signedIn(t, s)
		cfg.Images.Model = "enterprise/smart"
		_, err := NewPainter(context.Background(), cfg)
		assert.ErrorContains(t, err, "smart can't draw pictures")
		cfg.Images.Model = "enterprise/nope"
		_, err = NewPainter(context.Background(), cfg)
		assert.ErrorContains(t, err, "doesn't offer nope", "never the organization's default instead")
	})
}
