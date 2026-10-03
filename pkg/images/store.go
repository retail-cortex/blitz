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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
// text, once read, is kept beside it (<sha>.pdf.txt).
type Store struct {
	dir    string
	pages  sync.Map // a PDF's SHA-256 -> its page count
	failed sync.Map // a PDF's SHA-256 -> why its text couldn't be read
}

// pdfText reads a PDF's text; tests replace it.
var pdfText = pdftext.Text

var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// extByMIME names stored files by type: every attachable type's usual
// extension, ".txt" for text.
var extByMIME = func() map[string]string {
	out := map[string]string{}
	for _, m := range Media {
		out[m.MIME] = m.Exts[0]
	}
	out["text/plain"] = ".txt"
	return out
}()

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
		os.Chtimes(filepath.Join(s.dir, img.SHA256+".pdf.txt"), now, now) // a PDF's text, if read
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

// PutFile stores an audio or video file as it is, streamed (it may be as
// large as MaxMediaBytes), and returns it without its data (Size set):
// its type comes from its first bytes and its name (Detect), its length
// from its header when that's cheap.
func (s *Store) PutFile(name string, f io.ReaderAt, size int64) (*Image, error) {
	if size > MaxMediaBytes {
		return nil, fmt.Errorf("%s is %s; the limit is %s", name, humanBytes(int(min(size, 1<<40))), humanBytes(MaxMediaBytes))
	}
	head := make([]byte, min(size, 1<<20))
	if _, err := f.ReadAt(head, 0); err != nil && err != io.EOF {
		return nil, err
	}
	mime := Detect(name, head[:min(len(head), 4096)])
	kind := KindOf(mime)
	if kind != KindAudio && kind != KindVideo {
		return nil, fmt.Errorf("%s: not audio or video", name)
	}
	seconds := Duration(mime, head)
	if seconds == 0 && size > int64(len(head)) { // the movie header is often at the end
		tail := make([]byte, min(size, 1<<20))
		if _, err := f.ReadAt(tail, size-int64(len(tail))); err == nil || err == io.EOF {
			seconds = Duration(mime, tail)
		}
	}
	tmp, err := os.CreateTemp(s.dir, ".img-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), io.NewSectionReader(f, 0, size)); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	img := &Image{Name: filepath.Base(name), MIME: mime, Kind: kind, SHA256: hex.EncodeToString(h.Sum(nil)),
		Size: int(size), OriginalBytes: int(size), Seconds: seconds}
	final := filepath.Join(s.dir, img.SHA256+extByMIME[mime])
	if _, err := os.Stat(final); err == nil {
		now := time.Now()
		return img, os.Chtimes(final, now, now)
	}
	return img, os.Rename(tmp.Name(), final)
}

// Path is a stored file's path, media type and size, by its URI: for
// sending a large one without reading it all.
func (s *Store) Path(uri string) (string, string, int64, error) {
	sha, ok := strings.CutPrefix(uri, URIScheme)
	if s == nil || !ok || !shaRE.MatchString(sha) {
		return "", "", 0, fmt.Errorf("invalid image reference %q", uri)
	}
	for mime, ext := range extByMIME {
		p := filepath.Join(s.dir, sha+ext)
		if info, err := os.Stat(p); err == nil {
			return p, mime, info.Size(), nil
		}
	}
	return "", "", 0, fmt.Errorf("file %s: %w", sha[:12], os.ErrNotExist)
}

// MaxDocumentText caps the text a model that can't read PDFs gets for one
// (about 75,000 tokens).
const MaxDocumentText = 300_000

// textHeader starts a cached text file: the page count, then the text.
const textHeader = "blitz-pdftext pages="

// Text is a stored PDF's text (see pdftext.Text) and page count, read once
// and then kept beside the PDF. A PDF whose text couldn't be read (it timed
// out, or is unreadable) isn't tried again by this process.
func (s *Store) Text(ctx context.Context, uri string) (string, int, error) {
	sha, ok := strings.CutPrefix(uri, URIScheme)
	if !ok || !shaRE.MatchString(sha) {
		return "", 0, fmt.Errorf("invalid image reference %q", uri)
	}
	cache := filepath.Join(s.dir, sha+".pdf.txt")
	if b, err := os.ReadFile(cache); err == nil {
		if head, text, ok := strings.Cut(string(b), "\n"); ok {
			if n, err := strconv.Atoi(strings.TrimPrefix(head, textHeader)); err == nil && strings.HasPrefix(head, textHeader) {
				return text, n, nil
			}
		}
	}
	if err, ok := s.failed.Load(sha); ok {
		return "", 0, err.(error)
	}
	data, mime, err := s.Get(uri)
	if err != nil {
		return "", 0, err
	}
	if mime != pdftext.MIME {
		return "", 0, fmt.Errorf("%s is not a PDF", sha[:12])
	}
	text, pages, err := pdfText(ctx, data, 0)
	if err != nil {
		if ctx.Err() == nil { // not the caller giving up: it would fail again
			s.failed.Store(sha, err)
		}
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
	if err := tmp.Sync(); err != nil {
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
		base, _, _ := strings.Cut(e.Name(), ".") // <sha>.png, <sha>.txt, <sha>.gemini.json
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

// Route is how a stored file goes to the model about to answer.
type Route int

// The routes.
const (
	// RouteInline sends the bytes in the request.
	RouteInline Route = iota
	// RouteText sends text instead: a PDF's text, a text file's content.
	RouteText
	// RouteNote sends a note that the model can't take the file.
	RouteNote
	// RouteUpload sends a reference to the file in the provider's own
	// store (Gemini's Files API), uploading it first.
	RouteUpload
)

// MediaPolicy routes a stored file of type mime, size bytes and (a PDF)
// pages for the model about to answer.
type MediaPolicy func(mime string, size int64, pages int) Route

// Uploader puts a stored file (path, of type mime, named by its SHA-256)
// in the provider's own store and returns its reference there.
type Uploader func(ctx context.Context, path, mime, sha string) (*genai.FileData, error)

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

// Expand returns contents with stored-file references replaced by what
// the model about to answer takes, as route says (nil: Inline for images,
// Text for PDFs and text files, Note for audio and video): the bytes; a
// PDF's text in a <document> block or a text file's in a <file> block; a
// note; or a reference to the file uploaded with up. A tool result's file
// follows the result. Inputs are never modified: they are usually the
// session's own events. A missing file, or a nil store, becomes a short
// note instead so the request still works; an upload that fails is an
// error.
func Expand(ctx context.Context, contents []*genai.Content, s *Store, route MediaPolicy, up Uploader) ([]*genai.Content, error) {
	if !HasRefs(contents) {
		return contents, nil
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
			uri, isTool := toolImageRef(p)
			var name string
			switch {
			case isRef(p):
				uri, name = p.FileData.FileURI, p.FileData.DisplayName
			case isTool:
				name = toolFileName(p.FunctionResponse)
				parts = append(parts, p)
			default:
				parts = append(parts, p)
				continue
			}
			part, err := s.load(ctx, uri, name, route, up)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
			changed = true
		}
		if changed {
			cp := *c
			cp.Parts = parts
			out[i] = &cp
		}
	}
	return out, nil
}

// defaultRoute is the route without a policy: images inline, PDFs and
// text as text, other media as a note.
func defaultRoute(mime string, size int64, pages int) Route {
	switch KindOf(mime) {
	case KindImage, "":
		return RouteInline
	case KindDocument, KindText:
		return RouteText
	}
	return RouteNote
}

func (s *Store) load(ctx context.Context, uri, name string, route MediaPolicy, up Uploader) (*genai.Part, error) {
	if route == nil {
		route = defaultRoute
	}
	gone := func() (*genai.Part, error) {
		return genai.NewPartFromText(fmt.Sprintf("[%s is no longer available]", cmp.Or(name, "image"))), nil
	}
	if s == nil {
		return gone()
	}
	path, mime, size, err := s.Path(uri)
	if err != nil {
		return gone()
	}
	sha := strings.TrimPrefix(uri, URIScheme)
	pages := 0
	if mime == pdftext.MIME {
		if data, _, err := s.Get(uri); err == nil {
			pages = s.pageCount(sha, data)
		}
	}
	r := route(mime, size, pages)
	if r == RouteUpload && up == nil {
		r = RouteNote
	}
	switch r {
	case RouteText:
		switch KindOf(mime) {
		case KindDocument:
			return s.documentText(uri, name), nil
		case KindText:
			data, err := os.ReadFile(path)
			if err != nil {
				return gone()
			}
			return genai.NewPartFromText(fmt.Sprintf("<file name=%q>\n%s\n</file>", cmp.Or(name, "file.txt"), strings.ToValidUTF8(string(data), "\uFFFD"))), nil
		}
		return note(name, mime), nil
	case RouteNote:
		return note(name, mime), nil
	case RouteUpload:
		fd, err := up(ctx, path, mime, sha)
		if err != nil {
			return nil, fmt.Errorf("sending %s: %w", cmp.Or(name, "a file"), err)
		}
		return &genai.Part{FileData: fd}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return gone()
	}
	// No DisplayName: the Gemini Developer API rejects it.
	return &genai.Part{InlineData: &genai.Blob{Data: data, MIMEType: mime}}, nil
}

// note says a model can't take a file.
func note(name, mime string) *genai.Part {
	kind := KindOf(mime)
	what := map[Kind]string{KindImage: "this image type", KindAudio: "audio", KindVideo: "video"}[kind]
	return genai.NewPartFromText(fmt.Sprintf("[%s: this model can't take %s]", cmp.Or(name, "a file"), cmp.Or(what, mime)))
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
