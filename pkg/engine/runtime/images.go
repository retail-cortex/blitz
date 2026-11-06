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
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"iter"
	"path/filepath"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// mediaSource is where stored files come from, and how large ones go up
// to the provider (set once the workspace has built its uploader).
type mediaSource struct {
	store *images.Store
	mu    sync.RWMutex
	up    images.Uploader
}

func (s *mediaSource) uploader() images.Uploader {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.up
}

func (s *mediaSource) storeOf() *images.Store {
	if s == nil {
		return nil
	}
	return s.store
}

// imageModel expands stored-file references in each request into what the
// model takes (acceptedFor, mediaPolicy): bytes, text, a note, or a file
// uploaded to the provider. Conversation history (and session files) only
// ever hold the references, so switching models mid-session is safe.
type imageModel struct {
	inner model.LLM
	media *mediaSource
}

func withImages(llm model.LLM, media *mediaSource) model.LLM {
	if llm == nil {
		return nil
	}
	if m, ok := llm.(*imageModel); ok {
		llm = m.inner
	}
	return &imageModel{inner: llm, media: media}
}

func (m *imageModel) Name() string { return m.inner.Name() }

func (m *imageModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if req != nil && images.HasRefs(req.Contents) {
		up := m.media.uploader()
		contents, err := images.Expand(ctx, req.Contents, m.media.storeOf(), mediaPolicy(acceptedFor(m.inner, up != nil)), up)
		if err != nil {
			return func(yield func(*model.LLMResponse, error) bool) { yield(nil, err) }
		}
		cp := *req
		cp.Contents = contents
		req = &cp
	}
	return func(yield func(*model.LLMResponse, error) bool) {
		for resp, err := range m.inner.GenerateContent(ctx, req, stream) {
			if err == nil {
				keepMade(m.media.storeOf(), resp)
			}
			if !yield(resp, err) {
				return
			}
		}
	}
}

// keepMade stores the files in a model's whole answer (an image it drew,
// audio it spoke) and puts references in their place, so the conversation
// never holds their bytes (spec_056 CA-70). Each is named for the model
// that made it and its hash. A file Blitz can't keep, or with no store,
// becomes a note.
func keepMade(s *images.Store, resp *model.LLMResponse) {
	if resp == nil || resp.Partial || resp.Content == nil {
		return
	}
	madeBy := "model"
	if o, ok := originOf(resp); ok && o.Model != "" {
		madeBy = o.Model
	}
	for i, p := range resp.Content.Parts {
		if p == nil || p.InlineData == nil || len(p.InlineData.Data) == 0 {
			continue
		}
		blob := p.InlineData
		sum := sha256.Sum256(blob.Data)
		name := fmt.Sprintf("%s-%s%s", madeBy, hex.EncodeToString(sum[:4]), extOf(blob.MIMEType))
		if s == nil {
			resp.Content.Parts[i] = &genai.Part{Text: fmt.Sprintf("[%s made %s, which wasn't kept]", madeBy, name)}
			continue
		}
		img, err := s.PutMade(name, blob.MIMEType, blob.Data)
		if err != nil {
			resp.Content.Parts[i] = &genai.Part{Text: fmt.Sprintf("[%s made a file Blitz couldn't keep: %v]", madeBy, err)}
			continue
		}
		ref := images.Part(img)
		ref.ThoughtSignature = p.ThoughtSignature // Gemini signs the images it makes
		resp.Content.Parts[i] = ref
	}
}

// extOf is the usual extension of mime ("" for one Blitz doesn't keep).
func extOf(mime string) string {
	if m, ok := images.MediaOf(mime); ok && len(m.Exts) > 0 {
		return m.Exts[0]
	}
	return ""
}

// unseenAttachments names the attachments in contents that the model
// taking list won't see as they are (CA-63): each one it gets as text or a
// note instead, for a switch's preview to list.
func unseenAttachments(contents []*genai.Content, s *images.Store, list []Accept, upload bool) []string {
	route := mediaPolicy(list)
	var out []string
	seen := map[string]bool{}
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			uri, name := attachmentRef(p)
			if uri == "" || seen[uri] || s == nil {
				continue
			}
			seen[uri] = true
			_, mime, size, err := s.Path(uri)
			if err != nil {
				continue
			}
			r := route(mime, size, s.Pages(uri))
			if r == images.RouteUpload && !upload {
				r = images.RouteNote
			}
			if r == images.RouteText && images.KindOf(mime) == images.KindText {
				continue // a text file is text either way
			}
			if r == images.RouteText || r == images.RouteNote {
				out = append(out, cmp.Or(name, strings.TrimPrefix(uri, images.URIScheme)[:12]))
			}
		}
	}
	return out
}

// attachmentRef is the stored file a part refers to, and its name: an
// attachment, or a tool result's file.
func attachmentRef(p *genai.Part) (uri, name string) {
	switch {
	case p == nil:
	case p.FileData != nil && strings.HasPrefix(p.FileData.FileURI, images.URIScheme):
		return p.FileData.FileURI, p.FileData.DisplayName
	case p.FunctionResponse != nil:
		if u, ok := p.FunctionResponse.Response[images.ToolResultKey].(string); ok && strings.HasPrefix(u, images.URIScheme) {
			n, _ := p.FunctionResponse.Response["path"].(string)
			return u, filepath.Base(n)
		}
	}
	return "", ""
}

// WithAttachments adds parts (usually images.Part references) to the
// prompt, ahead of its text as providers recommend.
func WithAttachments(parts ...*genai.Part) ExecOption {
	return func(s *runState) { s.attachments = append(s.attachments, parts...) }
}

func userContent(prompt string, attachments []*genai.Part) *genai.Content {
	if len(attachments) == 0 {
		return genai.NewContentFromText(prompt, genai.RoleUser)
	}
	parts := append([]*genai.Part{}, attachments...)
	if prompt != "" {
		parts = append(parts, genai.NewPartFromText(prompt))
	}
	return &genai.Content{Role: genai.RoleUser, Parts: parts}
}
