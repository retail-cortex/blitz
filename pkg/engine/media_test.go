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
	"encoding/binary"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An attachment the active agent's model can't take is refused at once,
// saying what it takes; what it takes attaches (spec_images_011).
func TestAttachmentsByModel(t *testing.T) {
	w := openTest(t) // a test model: images, PDFs and text, no audio or video
	mp4 := make([]byte, 16)
	binary.BigEndian.PutUint32(mp4, 16)
	copy(mp4[4:], "ftypisom")
	write(t, w.Dir(), "talk.mp4", string(mp4))
	write(t, w.Dir(), "notes.md", "# Notes")

	_, err := w.LoadAttachments([]string{"talk.mp4"}, "", func(string) {})
	assert.ErrorIs(t, err, api.ErrUnsupportedMedia)
	assert.ErrorContains(t, err, "can't take video (it takes images, PDFs and text)")
	_, err = w.AddImage("clip.mp4", mp4)
	assert.ErrorIs(t, err, api.ErrUnsupportedMedia)

	got, err := w.LoadAttachments([]string{"notes.md"}, "", func(string) {})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, images.KindText, got[0].Kind)

	var warned []string
	got, err = w.LoadAttachments(nil, "look at @talk.mp4", func(s string) { warned = append(warned, s) })
	require.NoError(t, err, "a mention only warns")
	assert.Empty(t, got)
	require.Len(t, warned, 1)
	assert.Contains(t, warned[0], "can't take video")

	kinds := map[images.Kind]bool{}
	for _, a := range w.AcceptedMedia() {
		kinds[a.Kind] = true
	}
	assert.Equal(t, map[images.Kind]bool{images.KindImage: true, images.KindDocument: true, images.KindText: true}, kinds)
}

func TestCheckMediaSize(t *testing.T) {
	w := openTest(t)
	assert.ErrorContains(t, w.checkMedia("big.png", "image/png", 30<<20), "takes image up to 20.0 MB")
	assert.NoError(t, w.checkMedia("a.png", "image/png", 1))
}

func TestAcceptedKinds(t *testing.T) {
	assert.Equal(t, "no attachments", acceptedKinds(nil))
	assert.Equal(t, "text", acceptedKinds([]runtime.Accept{{Kind: images.KindText}}))
	assert.Equal(t, "images, audio and video", acceptedKinds([]runtime.Accept{{Kind: images.KindImage}, {Kind: images.KindAudio}, {Kind: images.KindVideo}}))
}

func TestHumanSize(t *testing.T) {
	assert.Equal(t, "512 B", humanSize(512))
	assert.Equal(t, "3 KB", humanSize(3<<10))
	assert.Equal(t, "2.0 GB", humanSize(2<<30))
}
