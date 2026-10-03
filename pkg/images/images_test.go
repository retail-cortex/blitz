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
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y += 7 {
		for x := 0; x < w; x += 5 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 99, 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// noisyPNG compresses badly, to exceed the byte limit at a small size.
func noisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(r.IntN(256))
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestPrepareKeepsSmallImages(t *testing.T) {
	data := pngBytes(t, 200, 100)
	img, err := Prepare("dir/shot.png", data, Options{})
	require.NoError(t, err)
	assert.False(t, img.Resized, "small image changed: %+v", img)
	assert.True(t, bytes.Equal(img.Data, data), "small image changed: %+v", img)
	assert.Equal(t, "image/png", img.MIME, "small image changed: %+v", img)
	assert.Equal(t, 200, img.Width, "small image changed: %+v", img)
	assert.Equal(t, 100, img.Height, "small image changed: %+v", img)
	assert.Equal(t, "shot.png", img.Name, "metadata: %q %q", img.Name, img.URI())
	assert.Len(t, img.SHA256, 64, "metadata: %q %q", img.Name, img.URI())
	assert.True(t, strings.HasPrefix(img.URI(), URIScheme), "metadata: %q %q", img.Name, img.URI())
	s := img.Summary()
	assert.Contains(t, s, "shot.png 200×100", "summary %q", s)
}

func TestPrepareScalesLargeImages(t *testing.T) {
	img, err := Prepare("wide.png", pngBytes(t, 3200, 800), Options{})
	require.NoError(t, err)
	assert.True(t, img.Resized, "got %dx%d %s resized=%v", img.Width, img.Height, img.MIME, img.Resized)
	assert.Equal(t, 1568, img.Width, "got %dx%d %s resized=%v", img.Width, img.Height, img.MIME, img.Resized)
	assert.Equal(t, 392, img.Height, "got %dx%d %s resized=%v", img.Width, img.Height, img.MIME, img.Resized)
	assert.Equal(t, "image/png", img.MIME, "got %dx%d %s resized=%v", img.Width, img.Height, img.MIME, img.Resized)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	assert.NoError(t, err, "stored data doesn't match: %+v", cfg)
	assert.Equal(t, 1568, cfg.Width, "stored data doesn't match: %+v %v", cfg, err)
	tall, _ := Prepare("tall.png", pngBytes(t, 300, 900), Options{MaxDimension: 300})
	assert.Equal(t, 100, tall.Width, "portrait: %dx%d", tall.Width, tall.Height)
	assert.Equal(t, 300, tall.Height, "portrait: %dx%d", tall.Width, tall.Height)
}

// A picture within the pixel limit but over the byte limit is re-encoded as
// JPEG rather than sent too large.
func TestPrepareCompressesHeavyImages(t *testing.T) {
	data := noisyPNG(t, 1500, 1000)
	require.Greater(t, len(data), MaxEncodedBytes, "fixture too small: %d", len(data))
	img, err := Prepare("noise.png", data, Options{})
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", img.MIME, "got %s %d bytes %dx%d", img.MIME, len(img.Data), img.Width, img.Height)
	assert.LessOrEqual(t, len(img.Data), MaxEncodedBytes, "got %s %d bytes %dx%d", img.MIME, len(img.Data), img.Width, img.Height)
	assert.Equal(t, 1500, img.Width, "got %s %d bytes %dx%d", img.MIME, len(img.Data), img.Width, img.Height)
}

func TestPrepareRejects(t *testing.T) {
	cases := map[string][]byte{
		"text":      []byte("hello, not an image"),
		"svg":       []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		"truncated": pngBytes(t, 50, 50)[:40],
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Prepare(name+".png", data, Options{})
			assert.Error(t, err, "%s accepted", name)
		})
	}
	_, err := Prepare("big.png", pngBytes(t, 10, 10), Options{MaxInput: 10})
	assert.Error(t, err, "size limit")
	assert.Contains(t, err.Error(), "limit", "size limit: %v", err)
	_, err = Prepare("x.txt", []byte("plain"), Options{})
	assert.ErrorIs(t, err, ErrNotImage, "want ErrNotImage, got %v", err)
}

// A decompression bomb declares a huge canvas in a tiny file; it must be
// refused from the header, before any pixels are allocated.
func TestPrepareRejectsDecompressionBomb(t *testing.T) {
	data := pngBytes(t, 1, 1)
	// Patch the IHDR width/height (bytes 16..23) to 50000×50000 and fix
	// the chunk's CRC (bytes 29..32, over type and data).
	bomb := bytes.Clone(data)
	copy(bomb[16:24], []byte{0, 0, 0xC3, 0x50, 0, 0, 0xC3, 0x50})
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	_, err := Prepare("bomb.png", bomb, Options{})
	assert.Error(t, err, "bomb")
	assert.Contains(t, err.Error(), "too large", "bomb: %v", err)
}

func TestPrepareFlattensTransparencyForJPEG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 40)) // fully transparent
	var buf bytes.Buffer
	png.Encode(&buf, src)
	// A JPEG source takes the JPEG path.
	img := &Image{Data: buf.Bytes(), MIME: "image/jpeg", Width: 40, Height: 40}
	require.NoError(t, img.shrink(src, 20))
	dec, err := jpeg.Decode(bytes.NewReader(img.Data))
	require.NoError(t, err)
	r, g, b, _ := dec.At(5, 5).RGBA()
	assert.GreaterOrEqual(t, r>>8, uint32(250), "transparent pixels should become white, got %v %v %v", r>>8, g>>8, b>>8)
	assert.GreaterOrEqual(t, g>>8, uint32(250), "transparent pixels should become white, got %v %v %v", r>>8, g>>8, b>>8)
	assert.GreaterOrEqual(t, b>>8, uint32(250), "transparent pixels should become white, got %v %v %v", r>>8, g>>8, b>>8)
}

func TestStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "images")
	s, err := OpenStore(dir)
	require.NoError(t, err)
	info, _ := os.Stat(dir)
	assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "dir mode %v", info.Mode().Perm())
	img, _ := Prepare("a.png", pngBytes(t, 20, 20), Options{})
	require.NoError(t, s.Put(img))
	require.NoError(t, s.Put(img))
	file := filepath.Join(dir, img.SHA256+".png")
	info, statErr := os.Stat(file)
	if assert.NoError(t, statErr) {
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "the stored image is owner-only")
	}
	data, mime, err := s.Get(img.URI())
	assert.NoError(t, err, "Get: %s", mime)
	assert.Equal(t, "image/png", mime, "Get: %s %v", mime, err)
	assert.True(t, bytes.Equal(data, img.Data), "Get: %s %v", mime, err)
	for _, bad := range []string{"", "blitz-image:../../etc/passwd", URIScheme + strings.Repeat("A", 64), "file:///x.png"} {
		t.Run(bad, func(t *testing.T) {
			_, _, err := s.Get(bad)
			assert.Error(t, err, "Get(%q) should fail", bad)
		})
	}
	_, _, err = s.Get(URIScheme + strings.Repeat("0", 64))
	assert.ErrorIs(t, err, os.ErrNotExist, "missing image: %v", err)

	// Prune removes old images only; Put refreshes the age of reused ones.
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(file, old, old)
	other, _ := Prepare("b.png", pngBytes(t, 30, 30), Options{})
	s.Put(other)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600)
	os.Chtimes(filepath.Join(dir, "notes.txt"), old, old)
	n, err := s.Prune(24 * time.Hour)
	assert.NoError(t, err, "Prune = %d,", n)
	assert.Equal(t, 1, n, "Prune = %d, %v", n, err)
	_, _, err = s.Get(img.URI())
	assert.Error(t, err, "old image should be pruned")
	_, _, err = s.Get(other.URI())
	assert.NoError(t, err, "recent image pruned")
	_, err = os.Stat(filepath.Join(dir, "notes.txt"))
	assert.NoError(t, err, "prune must only touch image files")
}

func TestExpand(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	img, _ := Prepare("shot.png", pngBytes(t, 20, 20), Options{})
	s.Put(img)
	missing := &Image{Name: "gone.png", MIME: "image/png", SHA256: strings.Repeat("1", 64)}

	user := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{Part(img), genai.NewPartFromText("what is this?")}}
	tool := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		Name: "view_image", Response: map[string]any{ToolResultKey: img.URI(), "path": "shot.png"}}}}}
	gone := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{Part(missing)}}
	plain := genai.NewContentFromText("hi", genai.RoleUser)
	in := []*genai.Content{plain, user, tool, gone}
	snapshot := []*genai.Part{user.Parts[0], user.Parts[1]}

	out := Expand(in, s, nil)
	assert.Same(t, plain, out[0], "contents without images should be passed through")
	b := out[1].Parts[0].InlineData
	assert.NotNil(t, b, "user image not expanded: %+v", out[1].Parts)
	assert.Equal(t, "image/png", b.MIMEType, "user image not expanded: %+v", out[1].Parts)
	assert.True(t, bytes.Equal(b.Data, img.Data), "user image not expanded: %+v", out[1].Parts)
	assert.Equal(t, "what is this?", out[1].Parts[1].Text, "user image not expanded: %+v", out[1].Parts)
	assert.Len(t, out[2].Parts, 2, "tool image should follow its result: %+v", out[2].Parts)
	assert.NotNil(t, out[2].Parts[0].FunctionResponse, "tool image should follow its result: %+v", out[2].Parts)
	assert.NotNil(t, out[2].Parts[1].InlineData, "tool image should follow its result: %+v", out[2].Parts)
	assert.Contains(t, out[3].Parts[0].Text, "gone.png is no longer available", "missing image: %+v", out[3].Parts[0])
	// The session's own contents are untouched.
	assert.Equal(t, snapshot, user.Parts, "Expand modified its input")
	assert.Nil(t, user.Parts[0].InlineData, "Expand modified its input")
	assert.Len(t, tool.Parts, 1, "Expand modified its input")
	got := Expand([]*genai.Content{plain}, s, nil)
	assert.Same(t, plain, got[0], "no refs: same slice expected")
	nilStore := Expand([]*genai.Content{user}, nil, nil)
	assert.NotEqual(t, "", nilStore[0].Parts[0].Text, "nil store should give a placeholder")
}

func TestMentions(t *testing.T) {
	got := Mentions(`look at @shot.png and @"My Screens/a b.JPG", not me@example.png, @src/main.go, again @shot.png. And @ui/x.webp,`)
	want := []string{"shot.png", "My Screens/a b.JPG", "ui/x.webp"}
	assert.Equal(t, want, got, "Mentions = %q", got)
	assert.Nil(t, Mentions("no mentions here"), "expected none")
}

func TestMentionedPaths(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"", nil},
		{"@main.go", []string{"main.go"}},
		{`see @src/ and @"a b/c.md", then @main.go? @main.go!`, []string{"src/", "a b/c.md", "main.go"}},
		{"me@example.com (@x.png)", nil},
		{"(@x.png) @shot.png", []string{"shot.png"}},
		{"a lone @ sign", nil},
	} {
		t.Run(tc.text, func(t *testing.T) { assert.Equal(t, tc.want, MentionedPaths(tc.text)) })
	}
}

func TestReadClipboardIsReplaceable(t *testing.T) {
	defer func(f func(context.Context) ([]byte, error)) { ReadClipboard = f }(ReadClipboard)
	ReadClipboard = func(context.Context) ([]byte, error) { return nil, ErrNoClipboardImage }
	_, err := ReadClipboard(context.Background())
	assert.ErrorIs(t, err, ErrNoClipboardImage, "%v", err)
}

// Summary uses the data's size, or Size when the data is elsewhere.
func TestSummary(t *testing.T) {
	img := &Image{Name: "a.png", Width: 2, Height: 3, Data: make([]byte, 2048)}
	assert.Equal(t, "a.png 2×3, 2 KB", img.Summary())
	img = &Image{Name: "b.png", Width: 2, Height: 3, Size: 3 << 20}
	assert.Equal(t, "b.png 2×3, 3.0 MB", img.Summary())
}

// A file that looks like a PNG but whose header is broken is refused.
func TestPrepareBrokenHeader(t *testing.T) {
	data := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	_, err := Prepare("bad.png", data, Options{})
	assert.ErrorContains(t, err, "unreadable image")
}

// The store refuses a missing directory, a directory it can't make, and
// images it can't name; Prune with no age, or over a missing directory.
func TestStoreErrors(t *testing.T) {
	_, err := OpenStore("")
	assert.Error(t, err, "no directory")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err = OpenStore(filepath.Join(file, "images"))
	assert.Error(t, err, "a file where the directory should be")

	dir := filepath.Join(t.TempDir(), "images")
	s, err := OpenStore(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, s.Dir())
	assert.Error(t, s.Put(&Image{MIME: "image/bmp", SHA256: strings.Repeat("a", 64)}), "unsupported type")
	assert.Error(t, s.Put(&Image{MIME: "image/png", SHA256: "../x"}), "not a hash")

	n, err := s.Prune(0)
	assert.NoError(t, err)
	assert.Zero(t, n, "no age: nothing pruned")

	// A directory where an image should be can't be read.
	sha := strings.Repeat("b", 64)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, sha+".png", "x"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, sha+".jpg", "x"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, sha+".gif", "x"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, sha+".webp", "x"), 0o700))
	_, _, err = s.Get(URIScheme + sha)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, os.RemoveAll(dir))
	_, err = s.Prune(time.Hour)
	assert.Error(t, err, "the directory is gone")
	img, err := Prepare("a.png", pngBytes(t, 4, 4), Options{})
	require.NoError(t, err)
	assert.Error(t, s.Put(img), "the directory is gone")
}

// Expand skips nil contents, and names an unnamed missing image "image".
func TestExpandNilAndUnnamed(t *testing.T) {
	missing := &Image{MIME: "image/png", SHA256: strings.Repeat("1", 64)}
	in := []*genai.Content{nil, {Role: genai.RoleUser, Parts: []*genai.Part{Part(missing)}}}
	assert.True(t, HasRefs(in))
	out := Expand(in, nil, nil)
	assert.Nil(t, out[0])
	assert.Equal(t, "[image is no longer available]", out[1].Parts[0].Text)
}
