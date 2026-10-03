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
	"strings"

	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/adk/v2/model"
)

// SupportsDocuments reports whether a model of provider reads a PDF as it
// is, pages, figures and all (spec_images_011): Gemini models, and Claude
// models through Anthropic's API or Vertex AI. Others are given the PDF's
// text.
func SupportsDocuments(provider, modelName string) bool {
	name := strings.ToLower(strings.TrimPrefix(modelName, "models/"))
	switch provider {
	case "gemini":
		return true
	case "anthropic", "vertex-anthropic":
		return strings.HasPrefix(name, "claude-")
	}
	return false
}

// documentLimits are the largest PDFs each API takes inline: Gemini's
// 20 MB request (less base64's third) and 1,000 pages; Anthropic's 32 MB
// request and 100 pages. A larger PDF is given as its text.
var documentLimits = map[string]struct{ bytes, pages int }{
	"gemini":    {14 << 20, 1000},
	"anthropic": {22 << 20, 100},
}

// documentPolicy is how m takes PDFs: as they are within its API's limits,
// or (nil) always as text. A fallback chain reads a PDF only if every
// model in it does, since any of them may answer.
func documentPolicy(m model.LLM) images.DocumentPolicy {
	switch m := m.(type) {
	case *imageModel:
		return documentPolicy(m.inner)
	case *settingsModel:
		lim, ok := documentLimits[m.provider]
		if !ok || !SupportsDocuments(m.provider, m.inner.Name()) {
			return nil
		}
		return func(size, pages int) bool { return size <= lim.bytes && pages <= lim.pages }
	case *fallbackModel:
		var all []images.DocumentPolicy
		for _, member := range m.chain {
			p := documentPolicy(member)
			if p == nil {
				return nil
			}
			all = append(all, p)
		}
		return func(size, pages int) bool {
			for _, p := range all {
				if !p(size, pages) {
					return false
				}
			}
			return true
		}
	}
	return nil
}
