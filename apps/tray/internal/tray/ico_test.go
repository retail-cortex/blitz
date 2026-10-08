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

package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An icon file holds the PNG whole, with its size in the directory entry
// (0 for 256 pixels or more).
func TestICO(t *testing.T) {
	for _, tc := range []struct {
		name string
		side int
		want byte
	}{
		{"small", 64, 64},
		{"large", 256, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p bytes.Buffer
			require.NoError(t, png.Encode(&p, image.NewRGBA(image.Rect(0, 0, tc.side, tc.side))))
			ico, err := ICO(p.Bytes())
			require.NoError(t, err)
			assert.Equal(t, []byte{0, 0, 1, 0, 1, 0}, ico[:6], "the header")
			assert.Equal(t, []byte{tc.want, tc.want}, ico[6:8], "the size")
			assert.Equal(t, uint32(p.Len()), binary.LittleEndian.Uint32(ico[14:18]), "the length")
			assert.Equal(t, uint32(22), binary.LittleEndian.Uint32(ico[18:22]), "the offset")
			assert.Equal(t, p.Bytes(), ico[22:])
		})
	}
	_, err := ICO([]byte("not a png"))
	assert.Error(t, err)
}
