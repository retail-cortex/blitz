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

package images

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/bmp"
	"google.golang.org/genai"
)

// iso is the start of an ISO base media file with a major brand.
func iso(brand string) []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint32(b, 16)
	copy(b[4:], "ftyp"+brand)
	return b
}

// wav is a WAVE file with seconds of 8 kHz 16-bit mono silence.
func wav(seconds int) []byte {
	rate, n := 8000, 8000*2*seconds
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+n))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(n))
	b.Write(make([]byte, n))
	return b.Bytes()
}

// mvhd is an MP4's movie header box: duration over a time scale.
func mvhd(scale, duration uint32) []byte {
	b := make([]byte, 8+20)
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	copy(b[4:], "mvhd")
	binary.BigEndian.PutUint32(b[8+12:], scale)
	binary.BigEndian.PutUint32(b[8+16:], duration)
	return b
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		head []byte
		want string
	}{
		{"a.png", pngBytes(t, 2, 2), "image/png"},
		{"a.mov", pngBytes(t, 2, 2), "image/png"}, // the bytes win over the name
		{"paper.bin", []byte("%PDF-1.7"), "application/pdf"},
		{"clip.mp4", iso("isom"), "video/mp4"},
		{"clip.mov", iso("qt  "), "video/quicktime"},
		{"song.m4a", iso("M4A "), "audio/mp4"},
		{"voice.m4a", iso("isom"), "audio/mp4"}, // a generic brand: the name tells
		{"x.3gp", iso("3gp5"), "video/3gpp"},
		{"photo.heic", iso("heic"), "image/heic"},
		{"photo.heif", iso("mif1"), "image/heif"},
		{"a.flac", []byte("fLaC\x00"), "audio/flac"},
		{"a.flv", []byte("FLV\x01"), "video/x-flv"},
		{"a.wmv", asfGUID, "video/x-ms-wmv"},
		{"a.wav", wav(1)[:44], "audio/wav"},
		{"a.avi", []byte("RIFF\x00\x00\x00\x00AVI LIST"), "video/x-msvideo"},
		{"a.ogg", []byte("OggS\x00\x02vorbis"), "audio/ogg"},
		{"a.opus", []byte("OggS\x00\x02____OpusHead"), "audio/opus"},
		{"a.webm", []byte{0x1A, 0x45, 0xDF, 0xA3}, "video/webm"},
		{"a.weba", []byte{0x1A, 0x45, 0xDF, 0xA3}, "audio/webm"},
		{"a.aac", []byte{0xFF, 0xF1, 0x50}, "audio/aac"},
		{"a.mp3", []byte("ID3\x04"), "audio/mpeg"},
		{"b.mp3", []byte{0xFF, 0xFB, 0x90}, "audio/mpeg"},
		{"a.mpg", []byte{0, 0, 1, 0xBA}, "video/mpeg"},
		{"a.bmp", []byte("BM\x00\x00"), "image/bmp"},
		{"notes.md", []byte("# Notes\n\nCafé ∇"), "text/plain"},
		{"cut.md", []byte("caf\xc3"), "text/plain"}, // a character cut at the end
		{"data.csv", []byte("a,b\x00c"), ""},        // a NUL: not text
		{"blob.bin", []byte("hello"), ""},           // text only by name
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Detect(tc.name, tc.head))
		})
	}
}

func TestCatalogue(t *testing.T) {
	assert.Equal(t, "video/quicktime", TypeForName("Demo.MOV"))
	assert.Equal(t, "text/plain", TypeForName("main.go"))
	assert.Empty(t, TypeForName("Makefile"))
	assert.Equal(t, KindVideo, KindOf("video/webm"))
	assert.Equal(t, Kind(""), KindOf("application/zip"))
	for p, want := range map[string]bool{"a.heic": true, "lecture.mp3": true, "demo.mov": true, "paper.pdf": true, "notes.md": false, "x.zip": false} {
		assert.Equal(t, want, IsAttachablePath(p), p)
	}
	assert.True(t, IsImagePath("scan.BMP"))
}

func TestPrepareKinds(t *testing.T) {
	var g, b bytes.Buffer
	require.NoError(t, gif.Encode(&g, image.NewGray(image.Rect(0, 0, 4, 4)), nil))
	require.NoError(t, bmp.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 4))))
	tests := []struct {
		name    string
		data    []byte
		mime    string
		kind    Kind
		summary string
		wantErr string
	}{
		{"anim.gif", g.Bytes(), "image/png", KindImage, "anim.gif 4×4", ""},
		{"scan.bmp", b.Bytes(), "image/png", KindImage, "scan.bmp 4×4", ""},
		{"photo.heic", iso("heic"), "image/heic", KindImage, "photo.heic 16 B", ""},
		{"notes.md", []byte("# hi"), "text/plain", KindText, "notes.md 4 B", ""},
		{"big.md", bytes.Repeat([]byte("a"), MaxTextBytes+1), "", "", "", "the limit for a text file"},
		{"bad.md", []byte("caf\xc3("), "", "", "", "not a file Blitz can attach"},
		{"tone.wav", wav(2), "audio/wav", KindAudio, "tone.wav 0:02", ""},
		{"clip.mp4", append(iso("isom"), mvhd(1000, 61500)...), "video/mp4", KindVideo, "clip.mp4 1:02", ""},
		{"x.zip", []byte("PK\x03\x04"), "", "", "", "not a file Blitz can attach"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img, err := Prepare(tc.name, tc.data, Options{})
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.mime, img.MIME)
			assert.Equal(t, tc.kind, img.Kind)
			assert.Contains(t, img.Summary(), tc.summary)
			assert.Len(t, img.SHA256, 64)
		})
	}
	_, err := Prepare("long.wav", wav(1), Options{MaxInput: 10})
	assert.NoError(t, err, "sent bytes may be as large as a PDF")
}

func TestPutFile(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	require.NoError(t, err)
	// The movie header at the end, as many encoders write it.
	video := append(append(iso("isom"), bytes.Repeat([]byte{7}, 2<<20)...), mvhd(600, 1800)...)
	src := filepath.Join(t.TempDir(), "talk.mp4")
	require.NoError(t, os.WriteFile(src, video, 0o644))
	f, err := os.Open(src)
	require.NoError(t, err)
	defer f.Close()

	img, err := s.PutFile("talk.mp4", f, int64(len(video)))
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", img.MIME)
	assert.Equal(t, KindVideo, img.Kind)
	assert.Equal(t, len(video), img.Size)
	assert.InDelta(t, 3, img.Seconds, 0.001)
	assert.Nil(t, img.Data, "not held in memory")
	path, mime, size, err := s.Path(img.URI())
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", mime)
	assert.EqualValues(t, len(video), size)
	assert.Equal(t, filepath.Join(dir, img.SHA256+".mp4"), path)

	again, err := s.PutFile("copy.mp4", f, int64(len(video)))
	require.NoError(t, err)
	assert.Equal(t, img.SHA256, again.SHA256, "stored once")

	_, err = s.PutFile("notes.md", strings.NewReader("hi"), 2)
	assert.ErrorContains(t, err, "not audio or video")
	_, err = s.PutFile("huge.mp4", f, MaxMediaBytes+1)
	assert.ErrorContains(t, err, "the limit")
	_, _, _, err = s.Path("blitz-image:" + strings.Repeat("0", 64))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDuration(t *testing.T) {
	assert.InDelta(t, 3, Duration("audio/wav", wav(3)), 0.001)
	assert.Zero(t, Duration("audio/wav", []byte("RIFF")))
	v1 := mvhd(0, 0)
	v1[8] = 1 // version 1, too short for its fields
	assert.Zero(t, Duration("video/mp4", v1))
	assert.Zero(t, Duration("video/mp4", mvhd(0, 100)), "no time scale")
	assert.Zero(t, Duration("audio/mpeg", []byte("ID3")))
}

func TestExpandRoutes(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	require.NoError(t, err)
	put := func(name string, data []byte) *Image {
		img, err := Prepare(name, data, Options{})
		require.NoError(t, err)
		require.NoError(t, s.Put(img))
		return img
	}
	text := put("notes.md", []byte("# Week 1"))
	audio := put("tone.wav", wav(1))
	video := put("clip.mp4", iso("isom"))
	pic := put("a.png", pngBytes(t, 4, 4))
	in := []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{Part(text), Part(audio), Part(video), Part(pic)}}}

	var uploaded []string
	up := func(_ context.Context, path, mime, sha string) (*genai.FileData, error) {
		uploaded = append(uploaded, filepath.Base(path))
		return &genai.FileData{FileURI: "https://files/" + sha[:6], MIMEType: mime}, nil
	}
	route := func(mime string, size int64, pages int) Route {
		switch KindOf(mime) {
		case KindText:
			return RouteText
		case KindAudio:
			return RouteInline
		case KindVideo:
			return RouteUpload
		}
		return RouteNote
	}
	out, err := Expand(context.Background(), in, s, route, up)
	require.NoError(t, err)
	parts := out[0].Parts
	assert.Equal(t, "<file name=\"notes.md\">\n# Week 1\n</file>", parts[0].Text)
	assert.Equal(t, "audio/wav", parts[1].InlineData.MIMEType)
	assert.Equal(t, "https://files/"+video.SHA256[:6], parts[2].FileData.FileURI)
	assert.Equal(t, []string{video.SHA256 + ".mp4"}, uploaded)
	assert.Equal(t, "[a.png: this model can't take this image type]", parts[3].Text)

	// Without an uploader, or a policy: notes for what can't go inline.
	out, err = Expand(context.Background(), in, s, route, nil)
	require.NoError(t, err)
	assert.Equal(t, "[clip.mp4: this model can't take video]", out[0].Parts[2].Text)
	out, err = Expand(context.Background(), in, s, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "[tone.wav: this model can't take audio]", out[0].Parts[1].Text)
	assert.NotNil(t, out[0].Parts[3].InlineData, "a picture goes inline")

	// An upload that fails fails the request, naming the file.
	_, err = Expand(context.Background(), in, s, route, func(context.Context, string, string, string) (*genai.FileData, error) {
		return nil, errors.New("quota")
	})
	assert.ErrorContains(t, err, "sending clip.mp4: quota")
}

func TestPruneSidecars(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	require.NoError(t, err)
	sha := strings.Repeat("d", 64)
	for _, name := range []string{sha + ".mp4", sha + ".gemini.json", sha + ".pdf.txt", "keep.me"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
		require.NoError(t, os.Chtimes(filepath.Join(dir, name), time0, time0))
	}
	n, err := s.Prune(1)
	require.NoError(t, err)
	assert.Equal(t, 3, n, "a file and what's kept beside it go together")
	assert.FileExists(t, filepath.Join(dir, "keep.me"))
}

// time0 is long ago.
var time0 = time.Unix(0, 0)
