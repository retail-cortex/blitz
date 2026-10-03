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

// Package pdftext reads PDF files: whether data is one, its page count and
// its text, page by page (spec_images_011). Models that can't read a PDF
// get its text; read_file shows it. Reading is defensive: the parser's
// panics become errors and a deadline bounds a pathological file.
package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

// MIME is a PDF's media type.
const MIME = "application/pdf"

// Timeout bounds reading one file's text.
const Timeout = 20 * time.Second

// ErrNotPDF is returned for data that isn't a PDF.
var ErrNotPDF = errors.New("not a PDF")

// IsPDF reports whether data starts like a PDF: the "%PDF-" header within
// its first kilobyte, as readers allow.
func IsPDF(data []byte) bool {
	return bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-"))
}

// IsPDFPath reports whether a file name ends in .pdf.
func IsPDFPath(p string) bool {
	return strings.HasSuffix(strings.ToLower(p), ".pdf")
}

func open(data []byte) (r *pdf.Reader, err error) {
	if !IsPDF(data) {
		return nil, ErrNotPDF
	}
	defer func() {
		if x := recover(); x != nil {
			r, err = nil, fmt.Errorf("unreadable PDF: %v", x)
		}
	}()
	r, err = pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if strings.Contains(err.Error(), "encrypt") {
			return nil, errors.New("the PDF is password-protected")
		}
		return nil, fmt.Errorf("unreadable PDF: %w", err)
	}
	return r, nil
}

// Pages counts a PDF's pages.
func Pages(data []byte) (n int, err error) {
	r, err := open(data)
	if err != nil {
		return 0, err
	}
	defer func() {
		if x := recover(); x != nil {
			n, err = 0, fmt.Errorf("unreadable PDF: %v", x)
		}
	}()
	n = r.NumPage()
	if n <= 0 {
		return 0, errors.New("unreadable PDF: no pages")
	}
	return n, nil
}

// Text is a PDF's text, each page headed "--- Page N ---", and its page
// count. A scanned page has no text to give. maxChars (> 0) cuts the text
// short with a note saying so.
func Text(ctx context.Context, data []byte, maxChars int) (string, int, error) {
	r, err := open(data)
	if err != nil {
		return "", 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	type result struct {
		text  string
		pages int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		text, pages, err := pageTexts(ctx, r, maxChars)
		done <- result{text, pages, err}
	}()
	select {
	case res := <-done:
		return res.text, res.pages, res.err
	case <-ctx.Done():
		return "", 0, fmt.Errorf("reading the PDF's text: %w", ctx.Err())
	}
}

var blankLines = regexp.MustCompile(`\n[ \t]*(\n[ \t]*)+`)

func pageTexts(ctx context.Context, r *pdf.Reader, maxChars int) (text string, pages int, err error) {
	defer func() {
		if x := recover(); x != nil {
			text, pages, err = "", 0, fmt.Errorf("unreadable PDF: %v", x)
		}
	}()
	pages = r.NumPage()
	var b strings.Builder
	for i := 1; i <= pages; i++ {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		t, err := r.Page(i).GetPlainText(nil)
		if err != nil {
			t = "(this page's text couldn't be read)"
		}
		t = strings.TrimSpace(blankLines.ReplaceAllString(t, "\n"))
		if t == "" {
			t = "(no text on this page: it may be a scan or a picture)"
		}
		fmt.Fprintf(&b, "--- Page %d ---\n%s\n\n", i, t)
		if maxChars > 0 && b.Len() > maxChars {
			cut := []rune(b.String())
			if len(cut) > maxChars {
				return string(cut[:maxChars]) + fmt.Sprintf("\n\n[text cut short at %d characters, in page %d of %d]", maxChars, i, pages), pages, nil
			}
		}
	}
	return strings.TrimSpace(b.String()), pages, nil
}
