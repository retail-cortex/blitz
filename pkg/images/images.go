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

// Package images prepares pictures and PDFs for models and keeps them out
// of session files.
//
// An image is normalised once (format checked, oversized pictures scaled
// down and re-encoded), written to a content-addressed Store and referred to
// in conversation history by a FileData part whose URI starts with URIScheme.
// Session files therefore hold a short reference instead of megabytes of
// base64; Expand swaps the references for the bytes just before each model
// request. A PDF is kept as it is, with its page count; Expand gives it to
// a model that reads PDFs as is, and to others as its text
// (spec_images_011).
package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // registers the GIF decoder
	"image/jpeg"
	"image/png"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/pdftext"
	_ "golang.org/x/image/bmp" // registers the BMP decoder
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the WebP decoder
)

// URIScheme prefixes references to stored images ("blitz-image:<sha256>").
const URIScheme = "blitz-image:"

// Defaults suit every supported provider: Anthropic recommends at most 1568
// pixels on the long edge and caps images at 5 MB (3.75 MB before base64).
const (
	DefaultMaxDimension = 1568
	DefaultMaxInput     = 20 << 20
	MaxEncodedBytes     = 3_750_000
	// MaxDocumentBytes is the largest PDF accepted, Anthropic's request
	// limit.
	MaxDocumentBytes = 32 << 20
	// maxPixels rejects decompression bombs before decoding: a tiny file
	// can declare a gigantic canvas.
	maxPixels = 100_000_000
)

// Options bound what Prepare accepts and produces.
type Options struct {
	MaxDimension int   // longest edge after scaling (default 1568)
	MaxInput     int64 // largest file accepted (default 20 MB)
}

func (o Options) withDefaults() Options {
	if o.MaxDimension <= 0 {
		o.MaxDimension = DefaultMaxDimension
	}
	if o.MaxInput <= 0 {
		o.MaxInput = DefaultMaxInput
	}
	return o
}

// Image is a prepared picture.
type Image struct {
	Name          string // file name for people (no directory)
	Data          []byte
	MIME          string
	Width, Height int
	SHA256        string // of Data
	OriginalBytes int
	Resized       bool
	// Size is len(Data), kept for an image whose data stays elsewhere (one
	// held by the Blitz service).
	Size int
	// Pages is a PDF's page count; 0 for a picture.
	Pages int
	// Kind is what sort of attachment it is ("" for an image prepared
	// before kinds existed).
	Kind Kind
	// Seconds is the length of audio or video, when its header gives it
	// cheaply; 0 otherwise.
	Seconds float64
}

// IsDocument reports whether img is a PDF rather than a picture.
func (img *Image) IsDocument() bool { return img.MIME == pdftext.MIME }

// URI is the reference stored in conversation history.
func (img *Image) URI() string { return URIScheme + img.SHA256 }

// Summary is a short human description: "shot.png 1280×720, 210 KB".
func (img *Image) Summary() string {
	n := len(img.Data)
	if n == 0 {
		n = img.Size
	}
	switch {
	case img.IsDocument():
		return fmt.Sprintf("%s %s, %s", img.Name, pagesLabel(img.Pages), humanBytes(n))
	case img.Kind == KindAudio || img.Kind == KindVideo:
		if img.Seconds > 0 {
			return fmt.Sprintf("%s %s, %s", img.Name, clock(img.Seconds), humanBytes(n))
		}
		return fmt.Sprintf("%s %s", img.Name, humanBytes(n))
	case img.Kind == KindText || img.Width == 0:
		return fmt.Sprintf("%s %s", img.Name, humanBytes(n))
	}
	return fmt.Sprintf("%s %d×%d, %s", img.Name, img.Width, img.Height, humanBytes(n))
}

func pagesLabel(n int) string {
	if n == 1 {
		return "1 page"
	}
	return fmt.Sprintf("%d pages", n)
}

// clock is a length as m:ss (or h:mm:ss).
func clock(seconds float64) string {
	s := int(seconds + 0.5)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ErrNotImage is returned for data that isn't a type Blitz attaches.
var ErrNotImage = errors.New("not a file Blitz can attach (an image, a PDF, a text file, audio or video)")

// decodable are the images decoded, checked and, when too big, scaled.
var decodable = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true}

// IsImagePath reports whether a file name has a supported image extension.
func IsImagePath(p string) bool { return KindOf(TypeForName(p)) == KindImage }

// Prepare validates data for the model, by its type (detected from the
// bytes, the name breaking ties: Detect). Images are checked and scaled
// down when larger than the limits allow, GIF and BMP made PNG (which
// every provider takes); HEIC and HEIF are kept as they are. A PDF (up
// to MaxDocumentBytes) is kept once it's known readable; text (up to
// MaxTextBytes), audio and video (up to the input limit; larger ones come
// from files, PrepareStream) as they are.
func Prepare(name string, data []byte, o Options) (*Image, error) {
	mime := Detect(name, data[:min(len(data), 4096)])
	switch KindOf(mime) {
	case KindDocument:
		return prepareDocument(name, data)
	case KindText:
		if len(data) > MaxTextBytes {
			return nil, fmt.Errorf("%s is %s; the limit for a text file is %s", name, humanBytes(len(data)), humanBytes(MaxTextBytes))
		}
		if !looksLikeText(data) || !utf8.Valid(data) {
			return nil, fmt.Errorf("%s: %w", name, ErrNotImage)
		}
		return kept(name, data, mime, KindText), nil
	case KindAudio, KindVideo:
		o = o.withDefaults()
		if int64(len(data)) > max(o.MaxInput, MaxDocumentBytes) {
			return nil, fmt.Errorf("%s is %s; attach a file this large from the workspace", name, humanBytes(len(data)))
		}
		img := kept(name, data, mime, KindOf(mime))
		img.Seconds = Duration(mime, data)
		return img, nil
	case KindImage:
		if !decodable[mime] { // HEIC, HEIF
			o = o.withDefaults()
			if int64(len(data)) > o.MaxInput {
				return nil, fmt.Errorf("%s is %s; the limit is %s", name, humanBytes(len(data)), humanBytes(int(o.MaxInput)))
			}
			return kept(name, data, mime, KindImage), nil
		}
	default:
		return nil, fmt.Errorf("%s: %w", name, ErrNotImage)
	}
	o = o.withDefaults()
	if int64(len(data)) > o.MaxInput {
		return nil, fmt.Errorf("%s is %s; the limit is %s", name, humanBytes(len(data)), humanBytes(int(o.MaxInput)))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: unreadable image: %w", name, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, fmt.Errorf("%s: %d×%d pixels is too large", name, cfg.Width, cfg.Height)
	}
	// Decode fully even when the image is kept as is: providers reject
	// corrupt images, and the header alone proves little.
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: unreadable image: %w", name, err)
	}
	img := &Image{Name: path.Base(strings.ReplaceAll(name, `\`, "/")), Data: data, MIME: mime, Kind: KindImage,
		Width: cfg.Width, Height: cfg.Height, OriginalBytes: len(data)}

	// GIF (Gemini doesn't take it) and BMP (Claude doesn't) become PNG.
	if cfg.Width > o.MaxDimension || cfg.Height > o.MaxDimension || len(data) > MaxEncodedBytes || mime == "image/gif" || mime == "image/bmp" {
		if err := img.shrink(src, o.MaxDimension); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	sum := sha256.Sum256(img.Data)
	img.SHA256 = hex.EncodeToString(sum[:])
	return img, nil
}

func prepareDocument(name string, data []byte) (*Image, error) {
	if len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("%s is %s; the limit for a PDF is %s", name, humanBytes(len(data)), humanBytes(MaxDocumentBytes))
	}
	pages, err := pdftext.Pages(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	sum := sha256.Sum256(data)
	return &Image{Name: path.Base(strings.ReplaceAll(name, `\`, "/")), Data: data, MIME: pdftext.MIME, Kind: KindDocument,
		Pages: pages, SHA256: hex.EncodeToString(sum[:]), OriginalBytes: len(data)}, nil
}

// kept is data attached as it is.
func kept(name string, data []byte, mime string, kind Kind) *Image {
	sum := sha256.Sum256(data)
	return &Image{Name: path.Base(strings.ReplaceAll(name, `\`, "/")), Data: data, MIME: mime, Kind: kind,
		SHA256: hex.EncodeToString(sum[:]), OriginalBytes: len(data)}
}

// shrink scales the image to fit maxDim and re-encodes it: PNG for
// lossless sources (screenshots stay crisp) unless that is still too big,
// otherwise JPEG.
func (img *Image) shrink(src image.Image, maxDim int) error {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxDim || h > maxDim {
		if w >= h {
			w, h = maxDim, max(1, h*maxDim/w)
		} else {
			w, h = max(1, w*maxDim/h), maxDim
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
		src = dst
	}

	var buf bytes.Buffer
	if img.MIME == "image/png" || img.MIME == "image/gif" || img.MIME == "image/bmp" {
		if err := png.Encode(&buf, src); err != nil {
			return err
		}
		if buf.Len() <= MaxEncodedBytes {
			img.set(buf.Bytes(), "image/png", w, h)
			return nil
		}
		buf.Reset()
	}
	// JPEG has no alpha channel: flatten onto white.
	flat := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), src, src.Bounds().Min, draw.Over)
	for _, q := range []int{85, 70, 50} {
		buf.Reset()
		if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: q}); err != nil {
			return err
		}
		if buf.Len() <= MaxEncodedBytes {
			img.set(buf.Bytes(), "image/jpeg", w, h)
			return nil
		}
	}
	return fmt.Errorf("still %s after compression", humanBytes(buf.Len()))
}

func (img *Image) set(data []byte, mime string, w, h int) {
	img.Data = bytes.Clone(data)
	img.MIME, img.Width, img.Height, img.Resized = mime, w, h, true
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
