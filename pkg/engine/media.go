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

package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/images"
)

// AcceptedMedia is what the active agent's model takes as attachments
// (spec_images_011), for the front ends to offer only those.
func (w *Workspace) AcceptedMedia() []runtime.Accept { return w.engine.AcceptedMedia("") }

// checkMedia refuses an attachment the active agent's model can't take:
// its type, or its size.
func (w *Workspace) checkMedia(name, mime string, size int64) error {
	model, _ := w.engine.AgentModel(w.engine.ActiveAgent())
	list := w.engine.AcceptedMedia("")
	kind := images.KindOf(mime)
	for _, a := range list {
		if !slices.Contains(a.MIMEs, mime) {
			continue
		}
		if size > a.MaxBytes {
			return fmt.Errorf("%s is %s; %s takes %s up to %s: %w", name, humanSize(size), model, kind, humanSize(a.MaxBytes), api.ErrUnsupportedMedia)
		}
		return nil
	}
	what := string(kind)
	if what == "" {
		what = "this kind of file"
	}
	return fmt.Errorf("%s: %s can't take %s (it takes %s): %w", name, model, what, acceptedKinds(list), api.ErrUnsupportedMedia)
}

// acceptedKinds names what a model takes: "images, PDFs and text".
func acceptedKinds(list []runtime.Accept) string {
	names := map[images.Kind]string{images.KindImage: "images", images.KindDocument: "PDFs", images.KindText: "text",
		images.KindAudio: "audio", images.KindVideo: "video"}
	var out []string
	for _, a := range list {
		out = append(out, names[a.Kind])
	}
	switch len(out) {
	case 0:
		return "no attachments"
	case 1:
		return out[0]
	}
	return strings.Join(out[:len(out)-1], ", ") + " and " + out[len(out)-1]
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// setupUploader gives the models Gemini's Files API for attachments too
// large to go inline, signed in as [llm.gemini]; none without a Gemini
// API key (Vertex AI has no Files API).
func (w *Workspace) setupUploader(ctx context.Context, warn func(string)) {
	up, err := runtime.NewGeminiUploader(ctx, w.cfg)
	if err != nil {
		warn(i18n.T("media.uploader_failed", "error", err.Error()))
	}
	w.engine.SetUploader(up)
}
