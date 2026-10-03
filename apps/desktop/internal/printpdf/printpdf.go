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

// Package printpdf prints a web page to PDF with the system's own web
// engine, offscreen: WebKit through WKWebView on macOS, WebKitGTK on Linux
// (spec_desktop_024). The desktop app's Export as PDF prints the Markdown
// preview this way, so the PDF looks as the preview does, highlighted code
// and Mermaid diagrams included, with no browser to install.
//
// The engine runs on the UI thread: inside the app that's Wails's; a
// program without Wails (a test) starts one with InitUI and RunMainLoop.
package printpdf

import (
	"errors"
	"sync"
	"time"
)

// Page sizes, in points.
var pageSizes = map[string][2]float64{
	"A4":     {595.28, 841.89},
	"Letter": {612, 792},
}

// margin is the page's margin, in points (15 mm).
const margin = 42.52

// ErrUnsupported is returned where the system has no engine to print with.
var ErrUnsupported = errors.New("printing to PDF isn't supported on this system")

// ErrTimeout is returned when the page didn't print in time.
var ErrTimeout = errors.New("printing the page took too long")

type result struct {
	pdf []byte
	err error
}

var (
	mu   sync.Mutex
	seq  uintptr
	jobs = map[uintptr]chan result{}
)

// Print renders html (a whole page, its styles and pictures inline) on
// pages of pageSize ("A4" or "Letter"; A4 otherwise) and returns the PDF.
// Scripts don't run, and the page can't navigate anywhere.
func Print(html, pageSize string, timeout time.Duration) ([]byte, error) {
	size, ok := pageSizes[pageSize]
	if !ok {
		size = pageSizes["A4"]
	}
	ch := make(chan result, 1)
	mu.Lock()
	seq++
	h := seq
	jobs[h] = ch
	mu.Unlock()
	if err := start(html, size, h); err != nil {
		take(h)
		return nil, err
	}
	select {
	case r := <-ch:
		return r.pdf, r.err
	case <-time.After(timeout):
		take(h) // the engine's answer, if it comes, goes nowhere
		return nil, ErrTimeout
	}
}

// take removes job h, returning its channel (nil when it's gone).
func take(h uintptr) chan result {
	mu.Lock()
	defer mu.Unlock()
	ch := jobs[h]
	delete(jobs, h)
	return ch
}

// done hands job h its result.
func done(h uintptr, pdf []byte, err error) {
	if ch := take(h); ch != nil {
		ch <- result{pdf, err}
	}
}
