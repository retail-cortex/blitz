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
	"reflect"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// loneSignature reports whether a part holds nothing but a thought
// signature.
func loneSignature(p *genai.Part) bool {
	if p == nil || len(p.ThoughtSignature) == 0 {
		return false
	}
	rest := *p
	rest.ThoughtSignature = nil
	return reflect.ValueOf(rest).IsZero()
}

// attachLoneSignatures returns req with each part that holds nothing but a
// thought signature folded into the part before it in the same content.
// Gemini 3 sends a text answer's signature on an empty last chunk, which
// the session keeps as a part of its own; Vertex AI refuses requests
// carrying such parts now and then (400 INVALID_ARGUMENT, "Request
// contains an invalid argument"; about one in five, 2026-09-28), and takes
// the signature on the text it belongs to. A lone signature with no part
// before it to carry it (or one already signed) is dropped: signatures are
// required only on function calls, which always carry their own. req is
// shared with the other models of a fallback chain, so a changed request
// is a copy.
func attachLoneSignatures(req *model.LLMRequest) *model.LLMRequest {
	if req == nil || !slicesContainsLone(req.Contents) {
		return req
	}
	cp := *req
	cp.Contents = make([]*genai.Content, 0, len(req.Contents))
	for _, c := range req.Contents {
		if c == nil || !slicesContainsLone([]*genai.Content{c}) {
			cp.Contents = append(cp.Contents, c)
			continue
		}
		nc := *c
		nc.Parts = make([]*genai.Part, 0, len(c.Parts))
		for _, p := range c.Parts {
			if !loneSignature(p) {
				nc.Parts = append(nc.Parts, p)
				continue
			}
			if n := len(nc.Parts); n > 0 && len(nc.Parts[n-1].ThoughtSignature) == 0 {
				signed := *nc.Parts[n-1]
				signed.ThoughtSignature = p.ThoughtSignature
				nc.Parts[n-1] = &signed
			}
		}
		if len(nc.Parts) > 0 {
			cp.Contents = append(cp.Contents, &nc)
		}
	}
	return &cp
}

func slicesContainsLone(cs []*genai.Content) bool {
	for _, c := range cs {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			if loneSignature(p) {
				return true
			}
		}
	}
	return false
}
