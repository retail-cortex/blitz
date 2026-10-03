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
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/pdftext"
	"google.golang.org/genai"
)

// Store keeps prepared images and PDFs on disk, named by their SHA-256,
// readable only by the owner. Identical files are stored once; a PDF's
// text, once read, is kept beside it (<sha>.txt).
type Store struct {
	dir   string
	pages sync.Map // a PDF's SHA-256 -> its page count
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

var extByMIME = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", pdftext.MIME: ".pdf"}

// OpenStore creates dir (mode 700) if needed.
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("image store directory not set")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the store's directory.
func (s *Store) Dir() string { return s.dir }

// Put saves img unless an identical image is already stored, in which case
// its age is reset so pruning keeps it.
func (s *Store) Put(img *Image) error {
	ext, ok := extByMIME[img.MIME]
	if !ok || !shaRE.MatchString(img.SHA256) {
		return fmt.Errorf("cannot store %s image", img.MIME)
	}
	final := filepath.Join(s.dir, img.SHA256+ext)
	if _, err := os.Stat(final); err == nil {
		now := time.Now()
		os.Chtimes(filepath.Join(s.dir, img.SHA256+".txt"), now, now) // a PDF's text, if read
		return os.Chtimes(final, now, now)
	}
	return s.writeFile(final, img.Data)
}

// Get loads a stored image by the URI from Image.URI.
func (s *Store) Get(uri string) ([]byte, string, error) {
	sha, ok := strings.CutPrefix(uri, URIScheme)
	if !ok || !shaRE.MatchString(sha) {
		return nil, "", fmt.Errorf("invalid image reference %q", uri)
	}
	for mime, ext := range extByMIME {
		data, err := os.ReadFile(filepath.Join(s.dir, sha+ext))
		if err == nil {
			return data, mime, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("image %s: %w", sha[:12], os.ErrNotExist)
}

// MaxDocumentText caps the text a model that can't read PDFs gets for one
// (about 75,000 tokens).
const MaxDocumentText = 300_000

// textHeader starts a cached text file: the page count, then the text.
const textHeader = "blitz-pdftext pages="

// Text is a stored PDF's text (see pdftext.Text) and page count, read once
// and then kept beside the PDF.
func (s *Store) Text(ctx context.Context, uri string) (string, int, error) {
	sha, ok := strings.CutPrefix(uri, URIScheme)
	if !ok || !shaRE.MatchString(sha) {
		return "", 0, fmt.Errorf("invalid image reference %q", uri)
	}
	cache := filepath.Join(s.dir, sha+".txt")
	if b, err := os.ReadFile(cache); err == nil {
		if head, text, ok := strings.Cut(string(b), "\n"); ok {
			if n, err := strconv.Atoi(strings.TrimPrefix(head, textHeader)); err == nil && strings.HasPrefix(head, textHeader) {
				return text, n, nil
			}
		}
	}
	data, mime, err := s.Get(uri)
	if err != nil {
		return "", 0, err
	}
	if mime != pdftext.MIME {
		return "", 0, fmt.Errorf("%s is not a PDF", sha[:12])
	}
	text, pages, err := pdftext.Text(ctx, data, 0)
	if err != nil {
		return "", 0, err
	}
	s.writeFile(cache, []byte(fmt.Sprintf("%s%d\n%s", textHeader, pages, text))) // best effort: read again next time
	return text, pages, nil
}

// MIME is a stored file's media type, by its URI; "" when it isn't stored
// (or s is nil).
func (s *Store) MIME(uri string) string {
	sha, ok := strings.CutPrefix(uri, URIScheme)
	if s == nil || !ok || !shaRE.MatchString(sha) {
		return ""
	}
	for mime, ext := range extByMIME {
		if _, err := os.Stat(filepath.Join(s.dir, sha+ext)); err == nil {
			return mime
		}
	}
	return ""
}

// Pages is a stored PDF's page count, by its URI; 0 when it isn't one (or
// s is nil).
func (s *Store) Pages(uri string) int {
	if s == nil {
		return 0
	}
	data, mime, err := s.Get(uri)
	if err != nil || mime != pdftext.MIME {
		return 0
	}
	return s.pageCount(strings.TrimPrefix(uri, URIScheme), data)
}

// pageCount is a stored PDF's page count, counted once per process.
func (s *Store) pageCount(sha string, data []byte) int {
	if n, ok := s.pages.Load(sha); ok {
		return n.(int)
	}
	n, _ := pdftext.Pages(data)
	s.pages.Store(sha, n)
	return n
}

// writeFile writes data to final atomically, owner-only.
func (s *Store) writeFile(final string, data []byte) error {
	tmp, err := os.CreateTemp(s.dir, ".img-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), final)
}

// Prune deletes images not used for maxAge and reports how many went.
func (s *Store) Prune(maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	n := 0
	for _, e := range entries {
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if e.IsDir() || !shaRE.MatchString(base) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			if os.Remove(filepath.Join(s.dir, e.Name())) == nil {
				n++
			}
		}
	}
	return n, nil
}

// Part is the history reference for img.
func Part(img *Image) *genai.Part {
	return &genai.Part{FileData: &genai.FileData{FileURI: img.URI(), MIMEType: img.MIME, DisplayName: img.Name}}
}

// ToolResultKey marks a tool result that carries an image: its value is an
// Image.URI, and Expand places the picture right after the result.
const ToolResultKey = "image_uri"

func isRef(p *genai.Part) bool {
	return p != nil && p.FileData != nil && strings.HasPrefix(p.FileData.FileURI, URIScheme)
}

func toolImageRef(p *genai.Part) (string, bool) {
	if p == nil || p.FunctionResponse == nil {
		return "", false
	}
	uri, ok := p.FunctionResponse.Response[ToolResultKey].(string)
	return uri, ok && strings.HasPrefix(uri, URIScheme)
}

// DocumentPolicy decides whether a model gets a PDF of size bytes and
// pages pages as it is (true) or as its text.
type DocumentPolicy func(size, pages int) bool

// HasRefs reports whether any content refers to a stored image.
func HasRefs(contents []*genai.Content) bool {
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			if _, ok := toolImageRef(p); ok || isRef(p) {
				return true
			}
		}
	}
	return false
}

// Expand returns contents with stored-image references replaced by the
// image bytes (and tool-result images appended after their result). A PDF
// goes as it is where native allows (nil: never), else as its text in a
// <document> block. Inputs are never modified: they are usually the
// session's own events. A missing file, or a nil store, becomes a short
// note instead so the request still works.
func Expand(contents []*genai.Content, s *Store, native DocumentPolicy) []*genai.Content {
	if !HasRefs(contents) {
		return contents
	}
	out := make([]*genai.Content, len(contents))
	for i, c := range contents {
		out[i] = c
		if c == nil {
			continue
		}
		changed := false
		parts := make([]*genai.Part, 0, len(c.Parts)+1)
		for _, p := range c.Parts {
			switch uri, isTool := toolImageRef(p); {
			case isRef(p):
				parts = append(parts, s.load(p.FileData.FileURI, p.FileData.DisplayName, native))
				changed = true
			case isTool:
				parts = append(parts, p, s.load(uri, toolFileName(p.FunctionResponse), native))
				changed = true
			default:
				parts = append(parts, p)
			}
		}
		if changed {
			cp := *c
			cp.Parts = parts
			out[i] = &cp
		}
	}
	return out
}

func (s *Store) load(uri, name string, native DocumentPolicy) *genai.Part {
	if s != nil {
		if data, mime, err := s.Get(uri); err == nil {
			if mime == pdftext.MIME {
				sha := strings.TrimPrefix(uri, URIScheme)
				if native == nil || !native(len(data), s.pageCount(sha, data)) {
					return s.documentText(uri, name)
				}
			}
			// No DisplayName: the Gemini Developer API rejects it.
			return &genai.Part{InlineData: &genai.Blob{Data: data, MIMEType: mime}}
		}
	}
	if name == "" {
		name = "image"
	}
	return genai.NewPartFromText(fmt.Sprintf("[%s is no longer available]", name))
}

// toolFileName names a tool result's file: the base of its path, else the
// tool.
func toolFileName(fr *genai.FunctionResponse) string {
	if p, _ := fr.Response["path"].(string); p != "" {
		return filepath.Base(p)
	}
	return fr.Name
}

// documentText is a PDF for a model that can't read one: its text.
func (s *Store) documentText(uri, name string) *genai.Part {
	name = cmp.Or(name, "document.pdf")
	text, pages, err := s.Text(context.Background(), uri)
	if err != nil {
		return genai.NewPartFromText(fmt.Sprintf("[%s: its text couldn't be read: %v]", name, err))
	}
	if r := []rune(text); len(r) > MaxDocumentText {
		text = string(r[:MaxDocumentText]) + fmt.Sprintf("\n[the text is cut short at %d characters]", MaxDocumentText)
	}
	return genai.NewPartFromText(fmt.Sprintf("<document name=%q pages=\"%d\">\nThis model can't read PDFs, so this is the text taken from it; figures and layout are lost.\n%s\n</document>", name, pages, text))
}
