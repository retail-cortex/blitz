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
	"slices"

	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/adk/v2/model"
)

// Accept is a kind of attachment a model takes: the media types, the
// largest file, and how: inline up to Inline bytes (and Pages pages, for
// PDFs), uploaded above it (Upload), else as text (AsText: a PDF's text,
// for a model that can't read PDFs).
type Accept struct {
	Kind     images.Kind
	MIMEs    []string
	MaxBytes int64
	Inline   int64
	Pages    int
	Upload   bool
	AsText   bool
}

// Limits on what goes to a model: Gemini's 20 MB request (less base64's
// third) and the Files API's 2 GB; Anthropic's 32 MB request; a PDF given
// as its text, as large as Blitz takes one.
const (
	geminiInline   = 14 << 20
	anthropicBytes = 22 << 20
)

// Media types by kind, for the table below.
var (
	commonImages = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp"} // GIF and BMP become PNG
	geminiImages = append(slices.Clone(commonImages), "image/heic", "image/heif")
	pdf          = []string{"application/pdf"}
	plainText    = []string{"text/plain"}
)

func mimesOf(kind images.Kind) []string {
	var out []string
	for _, m := range images.Media {
		if m.Kind == kind {
			out = append(out, m.MIME)
		}
	}
	return out
}

// AcceptedMedia is what a model of provider takes (spec_011_images), as
// its record says (spec_056 CA-10, ModelInfo.Accepts):
//   - Gemini: pictures (HEIC too), PDFs, text, audio and video; audio and
//     video over 14 MB through the Files API when upload is possible (the
//     Gemini API, not Vertex AI), else no larger than that.
//   - Claude (Anthropic's API, Vertex AI, Bedrock): pictures, PDFs and text.
//   - Others: pictures, text, and PDFs as their text.
//
// A model that doesn't read images takes none (CA-64).
func AcceptedMedia(provider, modelName string, upload bool) []Accept {
	return Info(wireAPI(provider), provider, modelName).Accepts(upload)
}

// accepts finds the entry that takes mime, if any.
func accepts(list []Accept, mime string) (Accept, bool) {
	for _, a := range list {
		if slices.Contains(a.MIMEs, mime) {
			return a, true
		}
	}
	return Accept{}, false
}

// acceptedFor is what m takes. A fallback chain takes only what every
// model in it takes, at the smallest limits, since any of them may
// answer; a model of a kind the engine doesn't know (a test's) takes what
// the other providers do.
func acceptedFor(m model.LLM, upload bool) []Accept {
	switch m := m.(type) {
	case *imageModel:
		return acceptedFor(m.inner, upload)
	case *settingsModel:
		return m.info().Accepts(upload)
	case *fallbackModel:
		var out []Accept
		for i, member := range m.chain {
			got := acceptedFor(member, upload)
			if i == 0 {
				out = got
				continue
			}
			out = intersect(out, got)
		}
		return out
	}
	return AcceptedMedia("", m.Name(), false)
}

// intersect is what both lists accept, each kind at the smaller limits.
func intersect(a, b []Accept) []Accept {
	var out []Accept
	for _, x := range a {
		for _, y := range b {
			if x.Kind != y.Kind {
				continue
			}
			z := x
			z.MIMEs = nil
			for _, m := range x.MIMEs {
				if slices.Contains(y.MIMEs, m) {
					z.MIMEs = append(z.MIMEs, m)
				}
			}
			z.MaxBytes, z.Inline, z.Pages = min(x.MaxBytes, y.MaxBytes), min(x.Inline, y.Inline), min(x.Pages, y.Pages)
			z.Upload, z.AsText = x.Upload && y.Upload, x.AsText && y.AsText
			if len(z.MIMEs) > 0 {
				out = append(out, z)
			}
		}
	}
	return out
}

// mediaPolicy routes stored files for a model that takes list: inline
// within its limits, uploaded above them where it can, as text where that
// stands in (PDFs, text files), else a note.
func mediaPolicy(list []Accept) images.MediaPolicy {
	return func(mime string, size int64, pages int) images.Route {
		a, ok := accepts(list, mime)
		switch {
		case !ok:
			return images.RouteNote
		case a.Kind == images.KindText:
			return images.RouteText
		case a.Inline > 0 && size <= a.Inline && (a.Pages == 0 || pages <= a.Pages):
			return images.RouteInline
		case a.Upload && size <= a.MaxBytes:
			return images.RouteUpload
		case a.AsText:
			return images.RouteText
		}
		return images.RouteNote
	}
}
