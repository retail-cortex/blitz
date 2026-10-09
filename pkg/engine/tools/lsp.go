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

package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/lsp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Code intelligence (spec_parity_027 PAR-TOOL-03): the lsp tool asks the
// workspace's language servers, started when first needed, guarded and in
// the OS sandbox like shell commands; after an edit to a file whose
// language server runs, its new diagnostics come with the edit's result.

// diagnosticsWait is how long an edit waits for the file's diagnostics.
const diagnosticsWait = 3 * time.Second

// lspServers are cfg's language servers whose commands are installed.
func lspServers(cfg *config.Config) []lsp.Server {
	var out []lsp.Server
	for lang, s := range cfg.LSPServers() {
		out = append(out, lsp.Server{Language: lang, Command: s.Command, Extensions: s.Extensions})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	return out
}

// lspLauncher starts language servers as shell commands run: guarded, in
// the OS sandbox, with the scrubbed environment, in the workspace.
func lspLauncher(env *ExecEnv) lsp.Launcher {
	return func(ctx context.Context, argv []string) (lsp.Process, error) {
		if _, err := exec.LookPath(argv[0]); err != nil {
			return nil, &lsp.NotInstalledError{Command: argv[0]}
		}
		cmd, err := env.command(ctx, argv)
		if err != nil {
			return nil, err
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cmd.abandon()
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			stdin.Close()
			cmd.abandon()
			return nil, err
		}
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		p := &lspProcess{cmd: cmd, stdin: stdin, stdout: stdout, done: make(chan struct{})}
		go func() { cmd.Wait(); close(p.done) }()
		return p, nil
	}
}

type lspProcess struct {
	cmd    *guardedCmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	done   chan struct{}
}

func (p *lspProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *lspProcess) Stdout() io.ReadCloser { return p.stdout }

// Stop closes its stdin and, if it doesn't exit, kills its process group.
func (p *lspProcess) Stop() {
	p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		killProcessGroup(p.cmd.Cmd)
	}
}

// LSPInput is what the lsp tool takes.
type LSPInput struct {
	Operation string `json:"operation" jsonschema:"definition, references, hover, symbols (in the workspace, by query) or diagnostics (of a file)"`
	Path      string `json:"path" jsonschema:"The file: for symbols, any file of the language to ask"`
	Line      int    `json:"line,omitempty" jsonschema:"1-based line (definition, references, hover)"`
	Column    int    `json:"column,omitempty" jsonschema:"1-based column, in characters, on the symbol (definition, references, hover)"`
	Query     string `json:"query,omitempty" jsonschema:"symbols: the name, or part of it"`
}

// LSPOutput is what it returns.
type LSPOutput struct {
	Locations   []lsp.Location   `json:"locations,omitempty"`
	Hover       string           `json:"hover,omitempty"`
	Symbols     []lsp.Symbol     `json:"symbols,omitempty"`
	Diagnostics []lsp.Diagnostic `json:"diagnostics,omitempty"`
	Note        string           `json:"note,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// NewLSPTool is lsp: go to definitions, find references, hover, search
// symbols and read diagnostics through the workspace's language servers.
func NewLSPTool(ws *Workspace, m *lsp.Manager) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "lsp",
			Description: "Code intelligence from the language's server (gopls, typescript-language-server, pyright, rust-analyzer…): a symbol's definition, its references, hover documentation, workspace symbols by name, or a file's diagnostics. Positions are 1-based lines and columns, as read_file shows them.",
		},
		func(ctx agent.Context, in LSPInput) (LSPOutput, error) {
			return runLSP(ctx, ws, m, in), nil
		})
}

func runLSP(ctx context.Context, ws *Workspace, m *lsp.Manager, in LSPInput) (out LSPOutput) {
	fail := func(err error) LSPOutput { out.Error = err.Error(); return out }
	rel, err := ws.Rel(in.Path)
	if err != nil {
		return fail(err)
	}
	f, err := ws.Open(rel) // blocked, missing, outside the workspace
	if err != nil {
		return fail(err)
	}
	f.Close()
	path := filepath.Join(ws.Dir(), rel)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	switch strings.ToLower(in.Operation) {
	case "definition":
		out.Locations, err = m.Definition(ctx, path, in.Line, in.Column)
	case "references":
		out.Locations, err = m.References(ctx, path, in.Line, in.Column)
	case "hover":
		out.Hover, err = m.Hover(ctx, path, in.Line, in.Column)
	case "symbols":
		if strings.TrimSpace(in.Query) == "" {
			return fail(errors.New("symbols needs a query"))
		}
		out.Symbols, err = m.Symbols(ctx, path, in.Query)
	case "diagnostics":
		out.Diagnostics, err = m.Diagnostics(ctx, path, 10*time.Second)
		if err == nil && len(out.Diagnostics) == 0 {
			out.Note = "No problems reported."
		}
	default:
		return fail(fmt.Errorf("operation %q: definition, references, hover, symbols or diagnostics", in.Operation))
	}
	if err != nil {
		return fail(err)
	}
	if out.Locations == nil && out.Hover == "" && out.Symbols == nil && out.Diagnostics == nil && out.Note == "" {
		out.Note = "Nothing found."
	}
	return out
}

// editDiagnostics are the problems in an edited file, when its language
// server already runs (nil otherwise, or when there are none).
func editDiagnostics(ctx context.Context, m *lsp.Manager, ws *Workspace, rel string) []lsp.Diagnostic {
	if m == nil || rel == "" {
		return nil
	}
	path := filepath.Join(ws.Dir(), rel)
	if !m.Running(path) {
		return nil
	}
	d, err := m.Diagnostics(ctx, path, diagnosticsWait)
	if err != nil {
		return nil
	}
	var out []lsp.Diagnostic
	for _, x := range d {
		if x.Severity == "error" || x.Severity == "warning" {
			out = append(out, x)
		}
		if len(out) == 20 {
			break
		}
	}
	return out
}
