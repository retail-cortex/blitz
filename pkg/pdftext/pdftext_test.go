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

package pdftext

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/retail-cortex/blitz/pkg/engine/mdpdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePDF builds a PDF with one page per text ("" for a page with none).
func makePDF(t *testing.T, pages ...string) []byte {
	t.Helper()
	f := fpdf.New("P", "mm", "A4", "")
	f.SetFont("Helvetica", "", 12)
	for _, p := range pages {
		f.AddPage()
		if p != "" {
			f.Cell(0, 10, p)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, f.Output(&buf))
	return buf.Bytes()
}

func TestIsPDF(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"header", []byte("%PDF-1.7\n..."), true},
		{"junk before the header", append(bytes.Repeat([]byte{' '}, 100), "%PDF-1.4"...), true},
		{"header too late", append(bytes.Repeat([]byte{' '}, 2000), "%PDF-1.4"...), false},
		{"png", []byte("\x89PNG\r\n"), false},
		{"empty", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPDF(tc.data))
		})
	}
	assert.True(t, IsPDFPath("papers/Attention.PDF"))
	assert.False(t, IsPDFPath("notes.md"))
}

func TestPagesAndText(t *testing.T) {
	data := makePDF(t, "Attention is all you need", "", "Results table")
	n, err := Pages(data)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	text, pages, err := Text(context.Background(), data, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, pages)
	assert.Contains(t, text, "--- Page 1 ---\nAttention is all you need")
	assert.Contains(t, text, "--- Page 2 ---\n(no text on this page")
	assert.Contains(t, text, "--- Page 3 ---\nResults table")
}

func TestTextCut(t *testing.T) {
	data := makePDF(t, strings.Repeat("word ", 30), strings.Repeat("more ", 30))
	text, _, err := Text(context.Background(), data, 60)
	require.NoError(t, err)
	assert.Contains(t, text, "[text cut short at 60 characters, in page 1 of 2]")
	assert.NotContains(t, text, "Page 2")
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"not a PDF", []byte("hello"), "not a PDF"},
		{"truncated", []byte("%PDF-1.7\n1 0 obj\n"), "unreadable PDF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Pages(tc.data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			_, _, err = Text(context.Background(), tc.data, 0)
			require.Error(t, err)
		})
	}
}

func TestTextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Text(ctx, makePDF(t, "a"), 0)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestTextIdentityCMap: text set in an embedded Unicode font, mapped by a
// <0000> <FFFF> range (as fpdf and TCPDF write it), decodes whole
// (third_party/ledongthuc_pdf/bfrange.patch).
func TestTextIdentityCMap(t *testing.T) {
	res, err := mdpdf.Render([]byte("Ελληνικά, кириллица, café ∇"), mdpdf.Options{})
	require.NoError(t, err)
	text, _, err := Text(context.Background(), res.PDF, 0)
	require.NoError(t, err)
	for _, w := range []string{"Ελληνικά", "кириллица", "café", "∇"} {
		assert.Contains(t, text, w)
	}
}
