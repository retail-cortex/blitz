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

package printpdf

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The engine needs the UI loop on the main thread: init pins main to it,
// and TestMain runs the tests beside the loop. Without a display (Linux
// with no X or Wayland) the tests that print skip.
func init() { runtime.LockOSThread() }

var ui bool

func TestMain(m *testing.M) {
	ui = InitUI()
	if !ui {
		os.Exit(m.Run())
	}
	go func() { os.Exit(m.Run()) }()
	RunMainLoop()
}

const page = `<!doctype html><html><head><meta charset="utf-8"><style>
body { font: 14pt sans-serif; } .next { break-before: page; }
</style></head><body>
<h1>Week 1</h1><p>Gradient descent, θ ← θ − η∇L.</p>
<p><a href="https://example.com">a link</a></p>
<div class="next"><p>Second page</p></div>
</body></html>`

func TestPrint(t *testing.T) {
	if !ui {
		t.Skip("no display to print with")
	}
	for _, tc := range []struct {
		size  string
		width float64
	}{{"A4", 595}, {"Letter", 612}, {"", 595}} {
		t.Run(tc.size, func(t *testing.T) {
			data, err := Print(page, tc.size, time.Minute)
			require.NoError(t, err)
			require.True(t, bytes.HasPrefix(data, []byte("%PDF-")))
			text, pages, err := pdftext.Text(context.Background(), data, 0)
			require.NoError(t, err)
			assert.Equal(t, 2, pages, "the page break is kept")
			flat := strings.Join(strings.Fields(text), " ")
			assert.Contains(t, flat, "Week 1")
			assert.Contains(t, flat, "Second page")
			r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
			require.NoError(t, err)
			box := r.Page(1).V.Key("MediaBox")
			if box.Len() == 0 {
				box = r.Trailer().Key("Root").Key("Pages").Key("MediaBox")
			}
			assert.InDelta(t, tc.width, box.Index(2).Float64()-box.Index(0).Float64(), 1)
		})
	}
}

func TestPrintTimesOut(t *testing.T) {
	_, err := Print(page, "A4", time.Nanosecond)
	assert.ErrorIs(t, err, ErrTimeout)
	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, jobs, "a job that timed out is forgotten")
}

func TestDoneForAJobThatsGone(t *testing.T) {
	done(12345, []byte("x"), nil) // no one waits: nothing happens
}
