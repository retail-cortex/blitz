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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// The ADK's OpenAI model (Responses API) refuses an answer with a picture
// in it (OpenAI's image_generation_call item, which the organization's
// service sends for Gemini's image models: spec_015_models MDL-95a). The
// api provider's middleware therefore swaps each picture for a message
// whose text is a unique marker, keeping the picture in the call's
// context, and apiModel swaps the markers back for the pictures, which
// the conversation then keeps like any picture a model made (CA-70). As
// with openai_images.go's markers, nothing is shared between calls.

const pictureMarker = "\u2063blitz-picture:" // U+2063 INVISIBLE SEPARATOR, then an id, then U+2063

type madePicturesKey struct{}

// madePictures are a call's pictures, by marker.
type madePictures struct {
	mu   sync.Mutex
	byID map[string]*genai.Blob
}

// withMadePictures is ctx keeping the pictures of the call it's for.
func withMadePictures(ctx context.Context) (context.Context, *madePictures) {
	mp := &madePictures{byID: map[string]*genai.Blob{}}
	return context.WithValue(ctx, madePicturesKey{}, mp), mp
}

func madePicturesOf(ctx context.Context) *madePictures {
	mp, _ := ctx.Value(madePicturesKey{}).(*madePictures)
	return mp
}

// keep stores an image_generation_call item's picture and returns the
// message item standing in for it, or nil when item is something else
// or carries no picture.
func (mp *madePictures) keep(item map[string]json.RawMessage) map[string]json.RawMessage {
	if string(item["type"]) != `"image_generation_call"` {
		return nil
	}
	var result, format, id string
	_ = json.Unmarshal(item["result"], &result)
	_ = json.Unmarshal(item["output_format"], &format)
	_ = json.Unmarshal(item["id"], &id)
	data, err := base64.StdEncoding.DecodeString(result)
	if err != nil || len(data) == 0 {
		return nil
	}
	if id == "" {
		id = randomID()
	}
	mime := "image/" + strings.ToLower(format)
	switch format {
	case "", "png":
		mime = "image/png"
	case "jpg":
		mime = "image/jpeg"
	}
	mp.mu.Lock()
	mp.byID[id] = &genai.Blob{MIMEType: mime, Data: data}
	mp.mu.Unlock()
	text, _ := json.Marshal(pictureMarker + id + "\u2063")
	content := `[{"type":"output_text","text":` + string(text) + `,"annotations":[]}]`
	idJSON, _ := json.Marshal(id)
	return map[string]json.RawMessage{
		"type": json.RawMessage(`"message"`), "id": idJSON, "role": json.RawMessage(`"assistant"`),
		"status": json.RawMessage(`"completed"`), "content": json.RawMessage(content),
	}
}

// rewriteOutput swaps the pictures in a response object's output; it
// reports whether there were any.
func (mp *madePictures) rewriteOutput(resp map[string]json.RawMessage) bool {
	var output []map[string]json.RawMessage
	if json.Unmarshal(resp["output"], &output) != nil {
		return false
	}
	changed := false
	for i, item := range output {
		if msg := mp.keep(item); msg != nil {
			output[i], changed = msg, true
		}
	}
	if changed {
		b, err := json.Marshal(output)
		if err != nil {
			return false
		}
		resp["output"] = b
	}
	return changed
}

// rewriteJSON is a response body, or a stream event's data, with its
// pictures swapped: a response's output, an event's response or item.
func (mp *madePictures) rewriteJSON(data []byte) []byte {
	if !bytes.Contains(data, []byte("image_generation_call")) {
		return data
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil || m == nil {
		return data
	}
	changed := mp.rewriteOutput(m)
	var inner map[string]json.RawMessage
	if json.Unmarshal(m["response"], &inner) == nil && inner != nil && mp.rewriteOutput(inner) {
		m["response"], _ = json.Marshal(inner)
		changed = true
	}
	var item map[string]json.RawMessage
	if json.Unmarshal(m["item"], &item) == nil && item != nil {
		if msg := mp.keep(item); msg != nil {
			m["item"], _ = json.Marshal(msg)
			changed = true
		}
	}
	if !changed {
		return data
	}
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}

// rewrite swaps the pictures in a successful /responses answer: a stream's
// events as they're read, or a whole body.
func (mp *madePictures) rewrite(res *http.Response) error {
	if res.Body == nil {
		return nil
	}
	if isEventStream(res) {
		res.Body = &sseRewriter{r: bufio.NewReaderSize(res.Body, 64<<10), c: res.Body, f: mp.rewriteJSON}
		res.ContentLength = -1
		res.Header.Del("Content-Length")
		return nil
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return err
	}
	body = mp.rewriteJSON(body)
	res.Body = io.NopCloser(bytes.NewReader(body))
	res.ContentLength = int64(len(body))
	res.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}

// sseRewriter is a stream of server-sent events with each data line
// passed through f.
type sseRewriter struct {
	r   *bufio.Reader
	c   io.Closer
	f   func([]byte) []byte
	buf bytes.Buffer
	err error
}

func (s *sseRewriter) Read(p []byte) (int, error) {
	for s.buf.Len() == 0 && s.err == nil {
		line, err := s.r.ReadBytes('\n')
		if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			body := bytes.TrimRight(bytes.TrimPrefix(rest, []byte(" ")), "\r\n")
			end := rest[len(bytes.TrimRight(rest, "\r\n")):]
			line = append(append([]byte("data: "), s.f(body)...), end...)
		}
		s.buf.Write(line)
		s.err = err
	}
	if s.buf.Len() > 0 {
		return s.buf.Read(p)
	}
	return 0, s.err
}

func (s *sseRewriter) Close() error { return s.c.Close() }

// restore is resp with each picture marker in its text swapped back for
// the picture, splitting the text around it.
func (mp *madePictures) restore(resp *model.LLMResponse) {
	if resp == nil || resp.Content == nil {
		return
	}
	var parts []*genai.Part
	changed := false
	for _, p := range resp.Content.Parts {
		if p == nil || p.Thought || !strings.Contains(p.Text, pictureMarker) {
			parts = append(parts, p)
			continue
		}
		changed = true
		rest := p.Text
		for {
			before, after, found := strings.Cut(rest, pictureMarker)
			if strings.TrimSpace(before) != "" {
				parts = append(parts, &genai.Part{Text: before})
			}
			if !found {
				break
			}
			id, tail, _ := strings.Cut(after, "\u2063")
			mp.mu.Lock()
			blob := mp.byID[id]
			mp.mu.Unlock()
			if blob != nil {
				parts = append(parts, &genai.Part{InlineData: blob})
			}
			rest = tail
		}
	}
	if changed {
		resp.Content.Parts = parts
	}
}
