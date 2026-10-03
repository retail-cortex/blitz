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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// pdfBytes builds a PDF with one page per text.
func pdfBytes(t *testing.T, pages ...string) []byte {
	t.Helper()
	f := fpdf.New("P", "mm", "A4", "")
	f.SetFont("Helvetica", "", 12)
	for _, p := range pages {
		f.AddPage()
		f.Cell(0, 10, p)
	}
	var buf bytes.Buffer
	require.NoError(t, f.Output(&buf))
	return buf.Bytes()
}

func TestPrepareDocument(t *testing.T) {
	data := pdfBytes(t, "Abstract", "Results")
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"a PDF", data, ""},
		{"too large", append(append([]byte{}, data...), make([]byte, MaxDocumentBytes)...), "the limit for a PDF is 32.0 MB"},
		{"unreadable", []byte("%PDF-1.7 truncated"), "unreadable PDF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Prepare("papers/attention.pdf", tc.data, Options{MaxInput: 1})
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.True(t, doc.IsDocument())
			assert.Equal(t, "attention.pdf", doc.Name)
			assert.Equal(t, pdftext.MIME, doc.MIME)
			assert.Equal(t, 2, doc.Pages)
			assert.Equal(t, data, doc.Data, "a PDF is kept as it is")
			assert.Len(t, doc.SHA256, 64)
			assert.Regexp(t, `^attention\.pdf 2 pages, \d+ KB$`, doc.Summary())
		})
	}
	one, err := Prepare("one.pdf", pdfBytes(t, "x"), Options{})
	require.NoError(t, err)
	assert.Contains(t, one.Summary(), "one.pdf 1 page,")
}

func TestStoreText(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	require.NoError(t, err)
	doc, err := Prepare("p.pdf", pdfBytes(t, "Gradient descent", "Momentum"), Options{})
	require.NoError(t, err)
	require.NoError(t, s.Put(doc))
	data, mime, err := s.Get(doc.URI())
	require.NoError(t, err)
	assert.Equal(t, pdftext.MIME, mime)
	assert.Equal(t, doc.Data, data)

	text, pages, err := s.Text(context.Background(), doc.URI())
	require.NoError(t, err)
	assert.Equal(t, 2, pages)
	assert.Contains(t, text, "--- Page 2 ---\nMomentum")

	// Read once, then from the cache beside the PDF.
	cache := filepath.Join(dir, doc.SHA256+".txt")
	require.FileExists(t, cache)
	require.NoError(t, os.WriteFile(cache, []byte(textHeader+"7\ncached"), 0o600))
	text, pages, err = s.Text(context.Background(), doc.URI())
	require.NoError(t, err)
	assert.Equal(t, "cached", text)
	assert.Equal(t, 7, pages)

	img, _ := Prepare("a.png", pngBytes(t, 4, 4), Options{})
	require.NoError(t, s.Put(img))
	_, _, err = s.Text(context.Background(), img.URI())
	assert.ErrorContains(t, err, "not a PDF")
	_, _, err = s.Text(context.Background(), "nope")
	assert.ErrorContains(t, err, "invalid image reference")
}

func TestExpandDocuments(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	require.NoError(t, err)
	doc, err := Prepare("paper.pdf", pdfBytes(t, "Attention is all you need"), Options{})
	require.NoError(t, err)
	require.NoError(t, s.Put(doc))
	user := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{Part(doc), genai.NewPartFromText("summarise")}}
	tool := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		Name: "view_document", Response: map[string]any{ToolResultKey: doc.URI(), "path": "paper.pdf"}}}}}

	var asked []int
	tests := []struct {
		name   string
		native DocumentPolicy
		want   func(t *testing.T, p *genai.Part)
	}{
		{"read as it is", func(size, pages int) bool { asked = []int{size, pages}; return true }, func(t *testing.T, p *genai.Part) {
			require.NotNil(t, p.InlineData)
			assert.Equal(t, pdftext.MIME, p.InlineData.MIMEType)
			assert.Equal(t, doc.Data, p.InlineData.Data)
			assert.Equal(t, []int{len(doc.Data), 1}, asked)
		}},
		{"too big for the model", func(size, pages int) bool { return false }, func(t *testing.T, p *genai.Part) {
			assert.Contains(t, p.Text, `<document name="paper.pdf" pages="1">`)
			assert.Contains(t, p.Text, "Attention is all you need")
		}},
		{"a model that can't read PDFs", nil, func(t *testing.T, p *genai.Part) {
			assert.Contains(t, p.Text, "This model can't read PDFs")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := Expand([]*genai.Content{user, tool}, s, tc.native)
			tc.want(t, out[0].Parts[0])
			assert.Equal(t, "summarise", out[0].Parts[1].Text)
			require.Len(t, out[1].Parts, 2, "the document follows the tool result")
			tc.want(t, out[1].Parts[1])
		})
	}
}

func TestExpandDocumentTextFails(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	require.NoError(t, err)
	// Stored, then damaged: the request still goes, with a note.
	doc, err := Prepare("bad.pdf", pdfBytes(t, "x"), Options{})
	require.NoError(t, err)
	require.NoError(t, s.Put(doc))
	require.NoError(t, os.WriteFile(filepath.Join(s.Dir(), doc.SHA256+".pdf"), []byte("%PDF-broken"), 0o600))
	out := Expand([]*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{Part(doc)}}}, s, nil)
	assert.Contains(t, out[0].Parts[0].Text, "[bad.pdf: its text couldn't be read")
}

func TestDocumentTextCut(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	require.NoError(t, err)
	doc, err := Prepare("long.pdf", pdfBytes(t, "x"), Options{})
	require.NoError(t, err)
	require.NoError(t, s.Put(doc))
	long := strings.Repeat("é", MaxDocumentText+10)
	require.NoError(t, os.WriteFile(filepath.Join(s.Dir(), doc.SHA256+".txt"), []byte(textHeader+"1\n"+long), 0o600))
	p := s.documentText(doc.URI(), "")
	assert.Contains(t, p.Text, `name="document.pdf"`)
	assert.Contains(t, p.Text, "[the text is cut short at 300000 characters]")
}

func TestAttachablePaths(t *testing.T) {
	assert.True(t, IsAttachablePath("a.PNG"))
	assert.True(t, IsAttachablePath("papers/x.pdf"))
	assert.False(t, IsAttachablePath("notes.md"))
	assert.Equal(t, []string{"paper.pdf", "fig.png"}, Mentions("read @paper.pdf and @fig.png and @notes.md"))
}

// A store's lookups by reference: nothing for a nil store, a bad
// reference, a file that isn't stored, or a picture asked for pages.
func TestStoreLookups(t *testing.T) {
	var none *Store
	assert.Empty(t, none.MIME("blitz-image:"+strings.Repeat("a", 64)))
	assert.Zero(t, none.Pages("x"))
	s, err := OpenStore(t.TempDir())
	require.NoError(t, err)
	img, _ := Prepare("a.png", pngBytes(t, 4, 4), Options{})
	require.NoError(t, s.Put(img))
	for _, uri := range []string{"nope", "blitz-image:short", "blitz-image:" + strings.Repeat("b", 64)} {
		assert.Empty(t, s.MIME(uri), uri)
		assert.Zero(t, s.Pages(uri), uri)
	}
	assert.Equal(t, "image/png", s.MIME(img.URI()))
	assert.Zero(t, s.Pages(img.URI()), "a picture has no pages")
	_, _, err = s.Text(context.Background(), "blitz-image:"+strings.Repeat("c", 64))
	assert.ErrorIs(t, err, os.ErrNotExist)
}
