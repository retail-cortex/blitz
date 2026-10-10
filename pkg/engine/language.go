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

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/lsp"
)

// The editor's language servers (spec_visual_editor_037 §5–§6): the same
// servers as the agent's lsp tool, for the documents the editor has open.

// The language servers' types and errors, as the service sees them.
type (
	// DocumentInfo is what opening an editor document says.
	DocumentInfo = lsp.DocumentInfo
	// LanguageState is how a document's language server is.
	LanguageState = lsp.State
	// CompletionList is what Complete returns.
	CompletionList = lsp.CompletionList
	// TextEdit is a range of a document replaced by text.
	TextEdit = lsp.TextEdit
	// TextPosition is a 1-based line and column, in characters.
	TextPosition = lsp.Position
	// DocumentDiagnostics are a document's diagnostics for a version.
	DocumentDiagnostics = lsp.DocumentDiagnostics
	// LanguageServerStatus is a language's server, for the status list.
	LanguageServerStatus = lsp.ServerStatus
	// ServerNotInstalledError is a server whose command isn't installed.
	ServerNotInstalledError = lsp.NotInstalledError
)

// The states of a language server.
const (
	LanguageReady     = lsp.StateReady
	LanguageStarting  = lsp.StateStarting
	LanguageMissing   = lsp.StateMissing
	LanguageFailed    = lsp.StateFailed
	LanguageIdle      = lsp.StateIdle
	LanguageNone      = lsp.StateNone
	LanguageUntrusted = lsp.StateUntrusted
)

// Errors of the editor's language requests.
var (
	// ErrUnknownDocument means no open document has the ID.
	ErrUnknownDocument = lsp.ErrUnknownDocument
	// ErrStaleDocument means a request was about another version of the
	// document than the server has.
	ErrStaleDocument = lsp.ErrStale
	// ErrNoLanguageServer means no server is configured for a language.
	ErrNoLanguageServer = lsp.ErrNoServer
)

// ErrUntrusted means the workspace's project settings aren't trusted, so
// no language server starts for the editor (VE-43).
var ErrUntrusted = errors.New("the project's settings aren't trusted: language servers run its code, so they start once you trust it")

// errNoLanguageServers means the workspace has no language servers (a
// registry without them).
var errNoLanguageServers = errors.New("no language servers in this workspace")

// LanguageTrusted reports whether language servers may start for the
// editor: the project's settings are trusted, or none need trust.
func (w *Workspace) LanguageTrusted() bool {
	switch w.ProjectSettings().State {
	case "", "none", "trusted":
		return true
	}
	return false
}

// languages is the workspace's language servers.
func (w *Workspace) languages() (*lsp.Manager, error) {
	m := w.tools.LSP()
	if m == nil {
		return nil, errNoLanguageServers
	}
	return m, nil
}

// CodeBlock is a code block opened as a document (spec_visual_editor_037
// §7): its language as a file extension, and the client's ID for it.
type CodeBlock struct {
	Extension string
	ID        string
}

// extension is what a code block's extension may be: a dot and a word.
var extension = regexp.MustCompile(`^\.[A-Za-z0-9+_-]{1,16}$`)

// OpenDocument opens the workspace file p (relative) for client's editor,
// with its text as the editor has it; or, with block, a code block in the
// Markdown file p, as a file of its own that's never written. In an
// untrusted workspace the document isn't opened: the state says why.
func (w *Workspace) OpenDocument(client, p, text string, version int64, block *CodeBlock) (lsp.DocumentInfo, error) {
	rel, _, err := userPath(p)
	if err != nil {
		return lsp.DocumentInfo{}, err
	}
	if block != nil && (!extension.MatchString(block.Extension) || block.ID == "") {
		return lsp.DocumentInfo{}, fmt.Errorf("%w: a code block needs an extension (.go) and an ID", ErrBadPath)
	}
	m, err := w.languages()
	if err != nil {
		return lsp.DocumentInfo{State: lsp.StateNone}, nil
	}
	if !w.LanguageTrusted() {
		return lsp.DocumentInfo{State: lsp.StateUntrusted, Detail: ErrUntrusted.Error()}, nil
	}
	if block != nil {
		sum := sha256.Sum256([]byte(client + "\x00" + rel + "\x00" + block.ID))
		path := lsp.SnippetPath(w.Dir(), hex.EncodeToString(sum[:8]), strings.ToLower(block.Extension))
		return m.OpenSnippet(client, path, text, version), nil
	}
	return m.OpenDocument(client, filepath.Join(w.Dir(), rel), text, version), nil
}

// ChangeDocument replaces an editor document's text.
func (w *Workspace) ChangeDocument(ctx context.Context, id string, version int64, text string) error {
	m, err := w.languages()
	if err != nil {
		return err
	}
	return m.ChangeDocument(ctx, id, version, text)
}

// CloseDocument closes an editor document.
func (w *Workspace) CloseDocument(id string) error {
	m, err := w.languages()
	if err != nil {
		return err
	}
	return m.CloseDocument(id)
}

// KeepDocuments keeps client's documents open, returning those still open.
func (w *Workspace) KeepDocuments(client string) []string {
	m, err := w.languages()
	if err != nil {
		return nil
	}
	return m.KeepDocuments(client)
}

// Complete is the completion at (line, col) of an editor document.
func (w *Workspace) Complete(ctx context.Context, id string, version int64, line, col int, trigger string) (lsp.CompletionList, error) {
	m, err := w.languages()
	if err != nil {
		return lsp.CompletionList{}, err
	}
	return m.Complete(ctx, id, version, line, col, trigger)
}

// DocumentHover is what the server says about the symbol at (line, col).
func (w *Workspace) DocumentHover(ctx context.Context, id string, version int64, line, col int) (string, error) {
	m, err := w.languages()
	if err != nil {
		return "", err
	}
	return m.DocumentHover(ctx, id, version, line, col)
}

// SourceLocation is a definition or reference, outside marking a file
// outside the workspace.
type SourceLocation struct {
	lsp.Location
	Outside bool
}

// DocumentDefinition is where the symbol at (line, col) is defined.
func (w *Workspace) DocumentDefinition(ctx context.Context, id string, version int64, line, col int) ([]SourceLocation, error) {
	m, err := w.languages()
	if err != nil {
		return nil, err
	}
	locs, err := m.DocumentDefinition(ctx, id, version, line, col)
	return sourceLocations(locs), err
}

// DocumentReferences are where the symbol at (line, col) is used.
func (w *Workspace) DocumentReferences(ctx context.Context, id string, version int64, line, col int) ([]SourceLocation, error) {
	m, err := w.languages()
	if err != nil {
		return nil, err
	}
	locs, err := m.DocumentReferences(ctx, id, version, line, col)
	return sourceLocations(locs), err
}

// sourceLocations marks the locations outside the workspace (absolute
// paths: the manager makes those inside relative).
func sourceLocations(locs []lsp.Location) []SourceLocation {
	out := make([]SourceLocation, len(locs))
	for i, l := range locs {
		out[i] = SourceLocation{Location: l, Outside: filepath.IsAbs(l.Path) || strings.HasPrefix(l.Path, "..")}
	}
	return out
}

// WatchDiagnostics sends client's documents' diagnostics to send until
// ctx ends or send fails.
func (w *Workspace) WatchDiagnostics(ctx context.Context, client string, send func(lsp.DocumentDiagnostics) error) error {
	m, err := w.languages()
	if err != nil {
		return nil // no servers: nothing will ever come, so the stream ends
	}
	return m.WatchDiagnostics(ctx, client, send)
}

// LanguageStatus lists the workspace's language servers.
func (w *Workspace) LanguageStatus() []lsp.ServerStatus {
	m, err := w.languages()
	if err != nil {
		return nil
	}
	return m.Status()
}

// RestartLanguageServer stops language's server; the next request starts
// it again.
func (w *Workspace) RestartLanguageServer(language string) error {
	m, err := w.languages()
	if err != nil {
		return err
	}
	return m.Restart(language)
}
