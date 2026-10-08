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
	"image/png"
)

// ICO wraps a PNG in an icon file (.ico), which Windows wants for a tray
// icon: one image, stored as the PNG itself, as Windows Vista and later
// read it.
func ICO(pngData []byte) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(pngData))
	if err != nil {
		return nil, err
	}
	side := func(n int) byte { // 0 means 256 or more
		if n >= 256 {
			return 0
		}
		return byte(n)
	}
	var b bytes.Buffer
	// ICONDIR: reserved, type 1 (icon), one image.
	_ = binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, 1})
	// ICONDIRENTRY: size, no palette, reserved, one plane, 32 bits per
	// pixel, the image's length and where it starts (after these 22 bytes).
	b.Write([]byte{side(cfg.Width), side(cfg.Height), 0, 0})
	_ = binary.Write(&b, binary.LittleEndian, [2]uint16{1, 32})
	_ = binary.Write(&b, binary.LittleEndian, [2]uint32{uint32(len(pngData)), 6 + 16})
	b.Write(pngData)
	return b.Bytes(), nil
}
