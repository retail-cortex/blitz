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
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/genai"
)

// placeMedia puts the files in cs where the API at o takes them
// (spec_056_conversation_adapters CA-62, CA-72). cs are already a request's
// own copies (adaptRequest), with stored files expanded (imageModel).
//
//   - Gemini takes files in the model's turns (the images it made): their
//     names, which its API refuses on inline data, are cleared.
//   - Claude and OpenAI take files only from the user. A file in the model's
//     turn (one a model made) moves to the user's next turn, after a note
//     naming it; a file reference neither can fetch (another provider's
//     upload, a stored file left unexpanded) becomes a note, never dropped
//     and never an error.
func placeMedia(o Origin, cs []*genai.Content) []*genai.Content {
	if o.API == "gemini" {
		for _, c := range cs {
			for i, p := range c.Parts {
				if p.InlineData != nil && p.InlineData.DisplayName != "" {
					blob := *p.InlineData
					blob.DisplayName = ""
					np := *p
					np.InlineData = &blob
					c.Parts[i] = &np
				}
			}
		}
		return cs
	}
	out := make([]*genai.Content, 0, len(cs))
	var moved []*genai.Part // files the model made, for the user's next turn
	for _, c := range cs {
		nc := *c
		nc.Parts = nil
		for _, p := range c.Parts {
			switch {
			case p.FileData != nil:
				nc.Parts = append(nc.Parts, unreachable(p.FileData))
			case p.InlineData != nil && c.Role == genai.RoleModel:
				name := cmp.Or(p.InlineData.DisplayName, "a file")
				blob := *p.InlineData
				blob.DisplayName = ""
				moved = append(moved, &genai.Part{Text: fmt.Sprintf("[%s, which a model made earlier in this conversation:]", name)}, &genai.Part{InlineData: &blob})
			default:
				nc.Parts = append(nc.Parts, p)
			}
		}
		if c.Role != genai.RoleModel && len(moved) > 0 {
			nc.Parts = append(nc.Parts, moved...)
			moved = nil
		}
		if len(nc.Parts) > 0 {
			out = append(out, &nc)
		}
	}
	if len(moved) > 0 {
		out = append(out, &genai.Content{Role: genai.RoleUser, Parts: moved})
	}
	return out
}

// unreachable is a note for a file reference the model can't fetch.
func unreachable(fd *genai.FileData) *genai.Part {
	name := cmp.Or(fd.DisplayName, fd.FileURI)
	if strings.HasPrefix(fd.FileURI, images.URIScheme) {
		return &genai.Part{Text: fmt.Sprintf("[%s isn't available to this model]", cmp.Or(fd.DisplayName, "a stored file"))}
	}
	return &genai.Part{Text: fmt.Sprintf("[%s: a file this model can't open]", name)}
}
