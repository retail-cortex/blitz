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

package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPosition(t *testing.T) {
	text := "héllo 🌍 world\nsecond"
	for _, tt := range []struct {
		line, col int
		want      wirePosition
		err       string
	}{
		{1, 1, wirePosition{0, 0}, ""},
		{1, 7, wirePosition{0, 6}, ""},
		{1, 9, wirePosition{0, 9}, ""}, // after the emoji: two UTF-16 units
		{2, 99, wirePosition{1, 6}, ""},
		{3, 1, wirePosition{}, "line 3"},
	} {
		t.Run(fmt.Sprintf("%d:%d", tt.line, tt.col), func(t *testing.T) {
			got, err := position(text, tt.line, tt.col)
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	assert.Equal(t, "hi", hoverText(json.RawMessage(`"hi"`)))
	assert.Equal(t, "a\n\nb", hoverText(json.RawMessage(`["a", {"language": "go", "value": "b"}]`)))
	assert.Equal(t, "typescriptreact", languageID("typescript", "a.tsx"))
}

// pipeProcess is a server the test plays, over pipes.
type pipeProcess struct {
	toServer   *io.PipeWriter
	fromServer *io.PipeReader
	stopped    bool
}

func (p *pipeProcess) Stdin() io.WriteCloser { return p.toServer }
func (p *pipeProcess) Stdout() io.ReadCloser { return p.fromServer }
func (p *pipeProcess) Stop()                 { p.stopped = true; p.toServer.Close(); p.fromServer.Close() }

func TestConnErrors(t *testing.T) {
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	p := &pipeProcess{toServer: clientOut, fromServer: clientIn}
	c := newConn(p, nil)
	go func() { // the server: an error for the first request, then gone
		r := bufio.NewReader(serverIn)
		body, err := readMessage(r)
		if err != nil {
			return
		}
		var m rpcMessage
		json.Unmarshal(body, &m)
		reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "no such method"}})
		fmt.Fprintf(serverOut, "Content-Length: %d\r\n\r\n%s", len(reply), reply)
		readMessage(r) // the cancelled request
		readMessage(r) // its cancellation
		serverOut.Close()
	}()
	ctx := context.Background()
	err := c.call(ctx, "nonsense", nil, nil)
	assert.ErrorContains(t, err, "nonsense: no such method (-32601)")

	cctx, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	assert.ErrorIs(t, c.call(cctx, "slow", nil, nil), context.Canceled)

	<-c.done
	assert.ErrorIs(t, c.call(ctx, "after", nil, nil), errClosed)
	p.Stop()
}

func TestReadMessage(t *testing.T) {
	for _, tt := range []struct{ in, want, err string }{
		{"Content-Length: 2\r\n\r\n{}", "{}", ""},
		{"Content-Type: x\r\nContent-Length: 2\r\n\r\n[]", "[]", ""},
		{"\r\n{}", "", "bad message length"},
		{"Content-Length: nope\r\n\r\n", "", "bad Content-Length"},
	} {
		t.Run(tt.in, func(t *testing.T) {
			got, err := readMessage(bufio.NewReader(strings.NewReader(tt.in)))
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}
