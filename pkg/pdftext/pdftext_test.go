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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/retail-cortex/blitz/pkg/engine/mdpdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePDF builds a PDF with one page per text ("" for a page with none).
// TestMain lets this test binary be the helper (MaybeServe). The helper
// can be made to hang or crash, for the tests of what Text does then.
func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv(helperEnv); ok {
		switch {
		case os.Getenv("PDFTEXT_TEST_HANG") != "":
			select {}
		case os.Getenv("PDFTEXT_TEST_CRASH") != "":
			fmt.Fprintln(os.Stderr, "fatal error: out of memory")
			os.Exit(2)
		}
	}
	MaybeServe()
	os.Exit(m.Run())
}

// useTestHelper makes Text read in this test binary, as the helper.
func useTestHelper(t *testing.T) {
	helperExe.Store(os.Args[0])
	t.Cleanup(func() { helperExe.Store("") })
}

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

// TestTextIdentityCMap checks that text set in an embedded Unicode font,
// mapped by a <0000> <FFFF> range (as fpdf and TCPDF write it), decodes
// whole (third_party/ledongthuc_pdf/bfrange.patch).
func TestTextIdentityCMap(t *testing.T) {
	res, err := mdpdf.Render([]byte("Ελληνικά, кириллица, café ∇"), mdpdf.Options{})
	require.NoError(t, err)
	text, _, err := Text(context.Background(), res.PDF, 0)
	require.NoError(t, err)
	for _, w := range []string{"Ελληνικά", "кириллица", "café", "∇"} {
		assert.Contains(t, text, w)
	}
}

// In the helper, the text is what it is in-process; a helper that hangs
// is killed at the timeout, one that crashes or is cancelled is an error.
func TestTextInHelper(t *testing.T) {
	data := makePDF(t, "first page", "second page")
	want, wantPages, err := textHere(context.Background(), data, 0)
	require.NoError(t, err)

	t.Run("same text", func(t *testing.T) {
		useTestHelper(t)
		text, pages, err := Text(context.Background(), data, 0)
		require.NoError(t, err)
		assert.Equal(t, want, text)
		assert.Equal(t, wantPages, pages)
		cut, _, err := Text(context.Background(), data, 10)
		require.NoError(t, err)
		assert.Contains(t, cut, "cut short at 10 characters")
	})
	t.Run("unreadable", func(t *testing.T) {
		useTestHelper(t)
		_, _, err := Text(context.Background(), []byte("%PDF-1.7\n1 0 obj\n"), 0)
		assert.ErrorContains(t, err, "unreadable PDF")
	})
	t.Run("hangs", func(t *testing.T) {
		useTestHelper(t)
		t.Setenv("PDFTEXT_TEST_HANG", "1")
		old := timeout
		timeout = 300 * time.Millisecond
		t.Cleanup(func() { timeout = old })
		start := time.Now()
		_, _, err := Text(context.Background(), data, 0)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 3*time.Second, "killed, not waited for")
	})
	t.Run("crashes", func(t *testing.T) {
		useTestHelper(t)
		t.Setenv("PDFTEXT_TEST_CRASH", "1")
		_, _, err := Text(context.Background(), data, 0)
		assert.ErrorContains(t, err, "out of memory")
	})
	t.Run("cancelled", func(t *testing.T) {
		useTestHelper(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := Text(ctx, data, 0)
		assert.ErrorIs(t, err, context.Canceled)
	})
	t.Run("not a PDF", func(t *testing.T) {
		useTestHelper(t)
		_, _, err := Text(context.Background(), []byte("hello"), 0)
		assert.ErrorIs(t, err, ErrNotPDF)
	})
}

// failing fails every read and write.
type failing struct{}

func (failing) Read([]byte) (int, error)  { return 0, errors.New("broken pipe") }
func (failing) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// The helper's side: the text, or why not, as JSON; a status of 1 when
// the answer can't be written.
func TestServe(t *testing.T) {
	data := makePDF(t, "hello")
	for name, tc := range map[string]struct {
		in        io.Reader
		out       io.Writer
		status    int
		wantText  string
		wantError string
	}{
		"text":       {in: bytes.NewReader(data), wantText: "hello"},
		"unreadable": {in: strings.NewReader("%PDF-1.7\n1 0 obj\n"), wantError: "unreadable PDF"},
		"no input":   {in: failing{}, wantError: "broken pipe"},
		"no output":  {in: bytes.NewReader(data), out: failing{}, status: 1},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			out := tc.out
			if out == nil {
				out = &buf
			}
			require.Equal(t, tc.status, serve(tc.in, out, 0))
			if tc.out != nil {
				return
			}
			var res helperResult
			require.NoError(t, json.Unmarshal(buf.Bytes(), &res))
			assert.Contains(t, res.Text, tc.wantText)
			if tc.wantError != "" {
				assert.Contains(t, res.Error, tc.wantError)
			} else {
				assert.Empty(t, res.Error)
			}
		})
	}
}

// UseHelper runs this program as the helper; a crash's last line of
// output is what's said.
func TestUseHelperAndLastLine(t *testing.T) {
	UseHelper()
	t.Cleanup(func() { helperExe.Store("") })
	exe, err := os.Executable()
	require.NoError(t, err)
	assert.Equal(t, exe, helperExe.Load())

	assert.Equal(t, "fatal error: out of memory", lastLine("goroutine 1 [running]:\nfatal error: out of memory"))
	assert.Len(t, lastLine(strings.Repeat("x", 500)), 200)
}

// emptyPDF is a well-formed PDF with no pages.
func emptyPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [] /Count 0 >>"}
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// A PDF with no pages has nothing to count or read.
func TestNoPages(t *testing.T) {
	_, err := Pages(emptyPDF())
	assert.ErrorContains(t, err, "no pages")
}
