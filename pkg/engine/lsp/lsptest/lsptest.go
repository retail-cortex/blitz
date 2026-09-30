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

// Package lsptest is a fake language server for tests: the test binary
// runs it when started with BLITZ_FAKE_LSP=1 (call MaybeServe first in
// TestMain), and Launcher starts that.
package lsptest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/lsp"
)

const envVar = "BLITZ_FAKE_LSP"

// MaybeServe runs the fake server and exits when this process was started
// as one.
func MaybeServe() {
	if os.Getenv(envVar) == "1" {
		Serve(os.Stdin, os.Stdout)
		os.Exit(0)
	}
}

// Launcher starts the fake server: this test binary, again.
func Launcher() lsp.Launcher {
	return func(ctx context.Context, argv []string) (lsp.Process, error) {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), envVar+"=1")
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &process{cmd: cmd, in: in, out: out}, nil
	}
}

type process struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out io.ReadCloser
}

func (p *process) Stdin() io.WriteCloser { return p.in }
func (p *process) Stdout() io.ReadCloser { return p.out }
func (p *process) Stop()                 { p.in.Close(); p.cmd.Process.Kill(); p.cmd.Wait() }

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Serve answers like a small language server: a problem ("fake problem")
// on line 1 of every file unless it says "fixed"; the symbol Foo defined
// on line 2, column 6, and used on line 4.
func Serve(in io.Reader, out io.Writer) {
	r := bufio.NewReader(in)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	for {
		body, err := read(r)
		if err != nil {
			return
		}
		var m message
		json.Unmarshal(body, &m)
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		uri := ""
		if td, ok := p["textDocument"].(map[string]any); ok {
			uri, _ = td["uri"].(string)
		}
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result}) }
		loc := func(line, char int) map[string]any {
			return map[string]any{"uri": uri, "range": map[string]any{"start": map[string]any{"line": line, "character": char}, "end": map[string]any{"line": line, "character": char + 3}}}
		}
		switch m.Method {
		case "initialize":
			// A request of the server's own, which the client must answer.
			send(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "workspace/configuration", "params": map[string]any{}})
			reply(map[string]any{"capabilities": map[string]any{}})
		case "textDocument/didOpen", "textDocument/didChange":
			text := ""
			if td, ok := p["textDocument"].(map[string]any); ok {
				text, _ = td["text"].(string)
			}
			if ch, ok := p["contentChanges"].([]any); ok && len(ch) > 0 {
				text, _ = ch[0].(map[string]any)["text"].(string)
			}
			diags := []any{}
			if !strings.Contains(text, "fixed") {
				diags = append(diags, map[string]any{"range": loc(0, 2)["range"], "severity": 1, "message": "fake problem", "source": "fake"})
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": uri, "diagnostics": diags}})
		case "textDocument/definition":
			reply([]any{map[string]any{"targetUri": uri, "targetSelectionRange": loc(1, 5)["range"], "targetRange": loc(1, 0)["range"]}})
		case "textDocument/references":
			reply([]any{loc(1, 5), loc(3, 4)})
		case "textDocument/hover":
			reply(map[string]any{"contents": map[string]any{"kind": "markdown", "value": "```go\nfunc Foo()\n```"}})
		case "workspace/symbol":
			reply([]any{map[string]any{"name": "Foo", "kind": 12, "containerName": "main", "location": loc(1, 5)}})
		case "shutdown":
			reply(nil)
		case "exit":
			return
		}
	}
}

func read(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	body := make([]byte, max(length, 0))
	_, err := io.ReadFull(r, body)
	return body, err
}
