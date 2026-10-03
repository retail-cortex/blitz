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
	"iter"
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
	return m.inner.GenerateContent(ctx, req, stream)
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
