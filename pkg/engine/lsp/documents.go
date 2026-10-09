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

// The editor's documents (spec_visual_editor_037 §5–§6): files the editor
// has open, with their text as the editor has it, sent to the same servers
// the agent uses. A document belongs to a client (a window) and closes when
// the client stops calling (a lease), so a window that crashed leaves
// nothing open.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// Errors of the editor's requests.
var (
	// ErrUnknownDocument means no open document has the ID (closed, or its
	// lease ran out).
	ErrUnknownDocument = errors.New("no such document: open it again")
	// ErrStale means a request was about another version of the document
	// than the one the server has.
	ErrStale = errors.New("the document has changed since")
)

// NotInstalledError is a server whose command isn't installed.
type NotInstalledError struct{ Command string }

// Error says which command is missing.
func (e *NotInstalledError) Error() string {
	return e.Command + " isn't installed (or not on PATH)"
}

// installHints say how to install each default server (VE-44).
var installHints = map[string]string{
	"go":         "go install golang.org/x/tools/gopls@latest",
	"typescript": "npm install -g typescript-language-server typescript",
	"python":     "npm install -g pyright",
	"rust":       "rustup component add rust-analyzer",
}

// InstallHint is what to run to install language's default server ("" when
// there's none to suggest).
func InstallHint(language string) string { return installHints[language] }

// State is how a document's server is.
type State string

// The states of a server.
const (
	StateReady    State = "ready"
	StateStarting State = "starting"
	StateMissing  State = "missing"
	StateFailed   State = "failed"
	StateIdle     State = "idle" // not started yet
	StateNone     State = "none" // no server for the language
	// StateUntrusted is the workspace's: its project isn't trusted, so the
	// editor's servers don't start (the engine decides).
	StateUntrusted State = "untrusted"
)

var (
	// leaseTTL is how long a client's documents last without a call.
	leaseTTL = 2 * time.Minute
	// leaseCheck is how often leases are checked.
	leaseCheck = 30 * time.Second
	// maxDocuments is how many editor documents a manager keeps.
	maxDocuments = 64
	// requestTimeout bounds an editor request; three in a row timing out
	// restart the server (VE-45).
	requestTimeout = 30 * time.Second
)

// maxCompletions bounds the completion items returned.
const maxCompletions = 200

// Document is an editor document.
type Document struct {
	ID       string
	Client   string
	Path     string // absolute
	Language string
	Version  int64

	uri      string
	text     string
	server   *server // the server it's open in (nil: not yet)
	lastSeen time.Time
	snippet  bool // a code block (snippet.go), not a file
}

// DocumentInfo is what opening a document says.
type DocumentInfo struct {
	ID       string // "" when its language has no server
	Language string
	State    State
	Detail   string // why it's missing or failed
	Install  string // what to install, when missing
}

// documents are a manager's editor documents and their watchers.
type documents struct {
	mu       sync.Mutex
	next     int
	byID     map[string]*Document
	watchers map[*watcher]bool
	sweeping bool
}

// watcher is a client's diagnostics stream: which documents' changed.
type watcher struct {
	client  string
	signal  chan struct{}
	mu      sync.Mutex
	pending map[string]bool // document IDs
}

// config is the server for path's kind of file (nil: none).
func (m *Manager) config(path string) *Server {
	ext := strings.ToLower(filepath.Ext(path))
	for i := range m.servers {
		for _, e := range m.servers[i].Extensions {
			if strings.EqualFold(e, ext) {
				return &m.servers[i]
			}
		}
	}
	return nil
}

// stateOf is how language's server is now, and why it failed.
func (m *Manager) stateOf(language string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.running[language]; s != nil {
		select {
		case <-s.c.done:
		default:
			return StateReady, nil
		}
	}
	if m.start[language] != nil {
		return StateStarting, nil
	}
	if f, ok := m.failed[language]; ok && time.Since(f.at) < failedRetry {
		var missing *NotInstalledError
		if errors.As(f.err, &missing) {
			return StateMissing, f.err
		}
		return StateFailed, f.err
	}
	return StateIdle, nil
}

// OpenDocument opens path (absolute) for client's editor with its text,
// starting the server in the background when it isn't running.
func (m *Manager) OpenDocument(client, path, text string, version int64) DocumentInfo {
	return m.open(client, path, text, version, false)
}

// OpenSnippet opens a code block as the file path (SnippetPath), which
// isn't on disk: a Go fragment is made a whole file for the server, and
// positions are the block's own.
func (m *Manager) OpenSnippet(client, path, text string, version int64) DocumentInfo {
	return m.open(client, path, text, version, true)
}

func (m *Manager) open(client, path, text string, version int64, snippet bool) DocumentInfo {
	cfg := m.config(path)
	if cfg == nil {
		return DocumentInfo{State: StateNone}
	}
	d := &m.docs
	d.mu.Lock()
	if d.byID == nil {
		d.byID = map[string]*Document{}
		d.watchers = map[*watcher]bool{}
	}
	d.next++
	doc := &Document{ID: "doc-" + strconv.Itoa(d.next), Client: client, Path: path, Language: cfg.Language,
		Version: version, uri: fileURI(path), text: text, lastSeen: time.Now(), snippet: snippet}
	d.byID[doc.ID] = doc
	evicted := d.evictLocked()
	if !d.sweeping {
		d.sweeping = true
		go m.sweep(leaseTTL, leaseCheck)
	}
	d.mu.Unlock()
	for _, old := range evicted {
		m.release(old)
	}

	info := DocumentInfo{ID: doc.ID, Language: cfg.Language}
	state, err := m.stateOf(cfg.Language)
	switch state {
	case StateReady:
		if err := m.attach(context.Background(), doc); err != nil {
			state = StateFailed
			info.Detail = err.Error()
		}
	case StateMissing, StateFailed:
		info.Detail = err.Error()
	default: // idle or starting: start it, and attach once it runs
		state = StateStarting
		go func() {
			ctx, cancel := context.WithTimeout(m.life, startTimeout)
			defer cancel()
			if _, err := m.For(ctx, path); err == nil {
				_ = m.attach(ctx, doc)
			}
		}()
	}
	info.State = state
	if state == StateMissing {
		info.Install = InstallHint(cfg.Language)
	}
	return info
}

// evictLocked closes the least recently seen documents beyond
// maxDocuments, returning them; d.mu must be held.
func (d *documents) evictLocked() []*Document {
	if len(d.byID) <= maxDocuments {
		return nil
	}
	all := make([]*Document, 0, len(d.byID))
	for _, doc := range d.byID {
		all = append(all, doc)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].lastSeen.Before(all[j].lastSeen) })
	out := all[:len(all)-maxDocuments]
	for _, doc := range out {
		delete(d.byID, doc.ID)
	}
	return out
}

// attach opens doc in its language's running server, if it isn't open
// there already (a server started again gets the editor's documents
// again).
func (m *Manager) attach(ctx context.Context, doc *Document) error {
	s, err := m.For(ctx, doc.Path)
	if err != nil {
		return err
	}
	d := &m.docs
	d.mu.Lock()
	if d.byID[doc.ID] != doc { // closed meanwhile
		d.mu.Unlock()
		return ErrUnknownDocument
	}
	fresh := doc.server != s
	doc.server = s
	text, _ := serverText(doc)
	d.mu.Unlock()
	return s.put(doc.uri, doc.Path, text, fresh)
}

// put sends the editor's text for uri: held from now on (hold: one more
// document holds it), opened in the server the first time, changed after.
func (s *server) put(uri, path, text string, hold bool) error {
	s.mu.Lock()
	if hold {
		s.held[uri]++
		s.opened = removeURI(s.opened, uri) // the agent's cap doesn't close it (VE-42)
	}
	prev, open := s.text[uri]
	if open && prev == text {
		s.mu.Unlock()
		return nil
	}
	s.version[uri]++
	v := s.version[uri]
	s.text[uri] = text
	delete(s.diags, uri)
	s.mu.Unlock()
	if !open {
		return s.c.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": languageID(s.lang, path), "version": v, "text": text,
		}})
	}
	return s.c.send("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": v},
		"contentChanges": []any{map[string]any{"text": text}},
	})
}

// unhold lets go of uri for one document; when none holds it, the server
// closes it (the agent's next sync opens it from disk).
func (s *server) unhold(uri string) error {
	s.mu.Lock()
	if s.held[uri] > 1 {
		s.held[uri]--
		s.mu.Unlock()
		return nil
	}
	delete(s.held, uri)
	_, open := s.text[uri]
	delete(s.text, uri)
	delete(s.version, uri)
	delete(s.diags, uri)
	s.mu.Unlock()
	if !open {
		return nil
	}
	return s.c.send("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
}

func removeURI(list []string, uri string) []string {
	out := list[:0]
	for _, u := range list {
		if u != uri {
			out = append(out, u)
		}
	}
	return out
}

// release lets go of a document that has left d.byID.
func (m *Manager) release(doc *Document) {
	m.docs.mu.Lock()
	s := doc.server
	doc.server = nil
	m.docs.mu.Unlock()
	if s != nil {
		_ = s.unhold(doc.uri)
	}
}

// document finds an open document, marking its client seen.
func (m *Manager) document(id string) (*Document, error) {
	d := &m.docs
	d.mu.Lock()
	defer d.mu.Unlock()
	doc := d.byID[id]
	if doc == nil {
		return nil, ErrUnknownDocument
	}
	doc.lastSeen = time.Now()
	return doc, nil
}

// ChangeDocument replaces a document's text with version's.
func (m *Manager) ChangeDocument(ctx context.Context, id string, version int64, text string) error {
	doc, err := m.document(id)
	if err != nil {
		return err
	}
	m.docs.mu.Lock()
	if version < doc.Version {
		m.docs.mu.Unlock()
		return ErrStale
	}
	doc.Version, doc.text = version, text
	s := doc.server
	m.docs.mu.Unlock()
	if s == nil {
		return nil // not open in a server yet: attach sends the text
	}
	if state, _ := m.stateOf(doc.Language); state != StateReady {
		return nil
	}
	return m.attach(ctx, doc)
}

// CloseDocument closes a document.
func (m *Manager) CloseDocument(id string) error {
	d := &m.docs
	d.mu.Lock()
	doc := d.byID[id]
	delete(d.byID, id)
	d.mu.Unlock()
	if doc == nil {
		return ErrUnknownDocument
	}
	m.release(doc)
	return nil
}

// KeepDocuments marks client seen, returning its documents still open.
func (m *Manager) KeepDocuments(client string) []string {
	d := &m.docs
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	now := time.Now()
	for id, doc := range d.byID {
		if doc.Client == client {
			doc.lastSeen = now
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// sweep closes documents whose client hasn't been seen for ttl and isn't
// watching, looking every check, until the manager closes.
func (m *Manager) sweep(ttl, check time.Duration) {
	t := time.NewTicker(check)
	defer t.Stop()
	for {
		select {
		case <-m.life.Done():
			return
		case <-t.C:
		}
		d := &m.docs
		d.mu.Lock()
		watching := map[string]bool{}
		for w := range d.watchers {
			watching[w.client] = true
		}
		var gone []*Document
		for id, doc := range d.byID {
			if !watching[doc.Client] && time.Since(doc.lastSeen) > ttl {
				delete(d.byID, id)
				gone = append(gone, doc)
			}
		}
		d.mu.Unlock()
		for _, doc := range gone {
			m.release(doc)
		}
	}
}

// request prepares an editor request about document id at version, line
// and col: its server and the LSP parameters.
func (m *Manager) request(ctx context.Context, id string, version int64, line, col int) (*server, *Document, map[string]any, error) {
	doc, err := m.document(id)
	if err != nil {
		return nil, nil, nil, err
	}
	m.docs.mu.Lock()
	stale := version != doc.Version
	text, w := serverText(doc)
	line += w.prefix
	m.docs.mu.Unlock()
	if stale {
		return nil, nil, nil, ErrStale
	}
	if err := m.attach(ctx, doc); err != nil {
		return nil, nil, nil, err
	}
	m.docs.mu.Lock()
	s := doc.server
	m.docs.mu.Unlock()
	pos, err := position(text, line, col)
	if err != nil {
		return nil, nil, nil, err
	}
	return s, doc, map[string]any{"textDocument": map[string]any{"uri": doc.uri}, "position": pos}, nil
}

// call makes an editor request, counting the server's timeouts: three in
// a row stop it, and the next request starts it again (VE-45).
func (m *Manager) call(ctx context.Context, s *server, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	err := s.c.call(ctx, method, params, result)
	s.mu.Lock()
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		s.timeouts++
	} else if err == nil {
		s.timeouts = 0
	}
	stuck := s.timeouts >= 3
	s.mu.Unlock()
	if stuck {
		m.stop(s)
	}
	return err
}

// TextEdit is a range of a document replaced by text.
type TextEdit struct {
	Start, End Position
	Text       string
}

// Position is a 1-based line and column, in characters.
type Position struct{ Line, Column int }

// Completion is one completion item.
type Completion struct {
	Label, Kind, Detail, Documentation string
	InsertText                         string
	Edit                               *TextEdit
	Snippet                            bool
	AdditionalEdits                    []TextEdit
	SortText, FilterText               string
}

// CompletionList is what Complete returns.
type CompletionList struct {
	Items      []Completion
	Incomplete bool
}

var completionKinds = []string{"", "text", "method", "function", "constructor", "field", "variable", "class",
	"interface", "module", "property", "unit", "value", "enum", "keyword", "snippet", "color", "file",
	"reference", "folder", "enum member", "constant", "struct", "event", "operator", "type parameter"}

type wireTextEdit struct {
	Range   *wireRange `json:"range"`
	Insert  *wireRange `json:"insert"` // InsertReplaceEdit
	NewText string     `json:"newText"`
}

type wireCompletion struct {
	Label               string          `json:"label"`
	Kind                int             `json:"kind"`
	Detail              string          `json:"detail"`
	Documentation       json.RawMessage `json:"documentation"`
	InsertText          string          `json:"insertText"`
	InsertTextFormat    int             `json:"insertTextFormat"`
	TextEdit            *wireTextEdit   `json:"textEdit"`
	AdditionalTextEdits []wireTextEdit  `json:"additionalTextEdits"`
	SortText            string          `json:"sortText"`
	FilterText          string          `json:"filterText"`
}

// Complete asks for completions at (line, col) of a document at version;
// trigger is the character typed that asked ("" when invoked).
func (m *Manager) Complete(ctx context.Context, id string, version int64, line, col int, trigger string) (CompletionList, error) {
	s, doc, params, err := m.request(ctx, id, version, line, col)
	if err != nil {
		return CompletionList{}, err
	}
	params["context"] = map[string]any{"triggerKind": 1}
	if trigger != "" {
		params["context"] = map[string]any{"triggerKind": 2, "triggerCharacter": trigger}
	}
	var raw json.RawMessage
	if err := m.call(ctx, s, "textDocument/completion", params, &raw); err != nil {
		return CompletionList{}, err
	}
	var list struct {
		Incomplete bool             `json:"isIncomplete"`
		Items      []wireCompletion `json:"items"`
	}
	if json.Unmarshal(raw, &list) != nil || list.Items == nil {
		list.Incomplete = false
		_ = json.Unmarshal(raw, &list.Items) // a bare array
	}
	m.docs.mu.Lock()
	text, w := serverText(doc)
	lines := strings.Count(doc.text, "\n") + 1
	m.docs.mu.Unlock()
	out := CompletionList{Incomplete: list.Incomplete}
	for _, item := range list.Items {
		c := Completion{Label: item.Label, Detail: item.Detail, Documentation: hoverText(item.Documentation),
			InsertText: item.InsertText, Snippet: item.InsertTextFormat == 2, SortText: item.SortText, FilterText: item.FilterText}
		if item.Kind > 0 && item.Kind < len(completionKinds) {
			c.Kind = completionKinds[item.Kind]
		}
		if c.InsertText == "" {
			c.InsertText = item.Label
		}
		if item.TextEdit != nil {
			if e, ok := textEdit(text, *item.TextEdit); ok && w.shift(&e, lines) {
				c.Edit = &e
			}
		}
		for _, a := range item.AdditionalTextEdits {
			// An edit in what was added (an import above a wrapped block) can't be made.
			if e, ok := textEdit(text, a); ok && w.shift(&e, lines) {
				c.AdditionalEdits = append(c.AdditionalEdits, e)
			}
		}
		out.Items = append(out.Items, c)
		if len(out.Items) == maxCompletions {
			out.Incomplete = true
			break
		}
	}
	return out, nil
}

// textEdit turns a wire edit into one in characters over text.
func textEdit(text string, w wireTextEdit) (TextEdit, bool) {
	r := w.Range
	if r == nil {
		r = w.Insert
	}
	if r == nil {
		return TextEdit{}, false
	}
	return TextEdit{Start: charPosition(text, r.Start), End: charPosition(text, r.End), Text: w.NewText}, true
}

// charPosition turns LSP's (0-based line, UTF-16 offset) in text into a
// 1-based line and column in characters.
func charPosition(text string, p wirePosition) Position {
	lines := strings.Split(text, "\n")
	out := Position{Line: p.Line + 1, Column: p.Character + 1}
	if p.Line >= 0 && p.Line < len(lines) {
		units := utf16.Encode([]rune(strings.TrimSuffix(lines[p.Line], "\r")))
		if p.Character <= len(units) {
			out.Column = len(utf16.Decode(units[:p.Character])) + 1
		}
	}
	return out
}

// DocumentHover is what the server says about the symbol at (line, col).
func (m *Manager) DocumentHover(ctx context.Context, id string, version int64, line, col int) (string, error) {
	s, _, params, err := m.request(ctx, id, version, line, col)
	if err != nil {
		return "", err
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := m.call(ctx, s, "textDocument/hover", params, &h); err != nil {
		return "", err
	}
	return hoverText(h.Contents), nil
}

// DocumentDefinition is where the symbol at (line, col) is defined.
func (m *Manager) DocumentDefinition(ctx context.Context, id string, version int64, line, col int) ([]Location, error) {
	s, doc, params, err := m.request(ctx, id, version, line, col)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := m.call(ctx, s, "textDocument/definition", params, &raw); err != nil {
		return nil, err
	}
	return m.inBlock(doc, m.locations(raw)), nil
}

// inBlock maps the locations in a code block's own file to the block:
// path "", the line its own; those in what was added are dropped.
func (m *Manager) inBlock(doc *Document, locs []Location) []Location {
	if !doc.snippet {
		return locs
	}
	m.docs.mu.Lock()
	_, w := serverText(doc)
	lines := strings.Count(doc.text, "\n") + 1
	m.docs.mu.Unlock()
	self := m.readable(doc.uri, wirePosition{}).Path
	var out []Location
	for _, l := range locs {
		if l.Path != self {
			out = append(out, l)
			continue
		}
		l.Path, l.Line = "", l.Line-w.prefix
		if l.Line >= 1 && l.Line <= lines {
			out = append(out, l)
		}
	}
	return out
}

// DocumentReferences are where the symbol at (line, col) is used.
func (m *Manager) DocumentReferences(ctx context.Context, id string, version int64, line, col int) ([]Location, error) {
	s, doc, params, err := m.request(ctx, id, version, line, col)
	if err != nil {
		return nil, err
	}
	params["context"] = map[string]any{"includeDeclaration": true}
	var raw json.RawMessage
	if err := m.call(ctx, s, "textDocument/references", params, &raw); err != nil {
		return nil, err
	}
	return m.inBlock(doc, m.locations(raw)), nil
}

// DocumentDiagnostic is one problem in an editor document.
type DocumentDiagnostic struct {
	Start, End                      Position
	Severity, Message, Source, Code string
}

// DocumentDiagnostics are a document's diagnostics for a version.
type DocumentDiagnostics struct {
	Document    string
	Version     int64
	Diagnostics []DocumentDiagnostic
}

// diagnosed is told by a server that uri's diagnostics came: each
// watcher with a document there hears of it.
func (m *Manager) diagnosed(uri string) {
	d := &m.docs
	d.mu.Lock()
	defer d.mu.Unlock()
	for w := range d.watchers {
		for id, doc := range d.byID {
			if doc.uri == uri && doc.Client == w.client {
				w.mu.Lock()
				w.pending[id] = true
				w.mu.Unlock()
				select {
				case w.signal <- struct{}{}:
				default:
				}
			}
		}
	}
}

// WatchDiagnostics sends client's documents' diagnostics to send, each
// document's latest first and then as they change, until ctx ends or send
// fails. Diagnostics about an older text than the server has are skipped:
// newer ones follow.
func (m *Manager) WatchDiagnostics(ctx context.Context, client string, send func(DocumentDiagnostics) error) error {
	w := &watcher{client: client, signal: make(chan struct{}, 1), pending: map[string]bool{}}
	d := &m.docs
	d.mu.Lock()
	if d.watchers == nil {
		d.byID = map[string]*Document{}
		d.watchers = map[*watcher]bool{}
	}
	d.watchers[w] = true
	for id, doc := range d.byID {
		if doc.Client == client {
			w.pending[id] = true
		}
	}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.watchers, w)
		d.mu.Unlock()
	}()
	w.signal <- struct{}{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.life.Done():
			return nil
		case <-w.signal:
		}
		w.mu.Lock()
		ids := make([]string, 0, len(w.pending))
		for id := range w.pending {
			ids = append(ids, id)
		}
		w.pending = map[string]bool{}
		w.mu.Unlock()
		sort.Strings(ids)
		for _, id := range ids {
			if dd, ok := m.diagnosticsOf(id); ok {
				if err := send(dd); err != nil {
					return err
				}
			}
		}
	}
}

// diagnosticsOf is a document's diagnostics now, if the server has
// published some for the text it has.
func (m *Manager) diagnosticsOf(id string) (DocumentDiagnostics, bool) {
	d := &m.docs
	d.mu.Lock()
	doc := d.byID[id]
	if doc == nil || doc.server == nil {
		d.mu.Unlock()
		return DocumentDiagnostics{}, false
	}
	s, uri, version := doc.server, doc.uri, doc.Version
	_, w := serverText(doc)
	lines := strings.Count(doc.text, "\n") + 1
	d.mu.Unlock()
	s.mu.Lock()
	pub, ok := s.diags[uri]
	text, current := s.text[uri], s.version[uri]
	s.mu.Unlock()
	if !ok || (pub.version != 0 && pub.version != current) {
		return DocumentDiagnostics{}, false
	}
	out := DocumentDiagnostics{Document: id, Version: version, Diagnostics: []DocumentDiagnostic{}}
	for _, item := range pub.items {
		sev := "error"
		if item.Severity > 0 && item.Severity < len(severities) {
			sev = severities[item.Severity]
		}
		e := TextEdit{Start: charPosition(text, item.Range.Start), End: charPosition(text, item.Range.End)}
		if !w.shift(&e, lines) {
			continue // about what was added, not the block
		}
		// A fragment's variables are often only shown, not used, and the
		// packages it calls aren't imported.
		if w.wrapped && (strings.Contains(item.Message, "declared and not used") || unimported(item.Message, text)) {
			sev = "hint"
		}
		out.Diagnostics = append(out.Diagnostics, DocumentDiagnostic{
			Start: e.Start, End: e.End,
			Severity: sev, Message: item.Message, Source: item.Source, Code: diagnosticCode(item.Code),
		})
	}
	return out, true
}

// diagnosticCode reads a code that's a number or a string.
func diagnosticCode(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// ServerStatus is a language's server, for the status list.
type ServerStatus struct {
	Language   string
	Command    []string
	Extensions []string
	State      State
	Since      time.Time
	Err        string
	Install    string
}

// Status lists the configured servers and how each is.
func (m *Manager) Status() []ServerStatus {
	out := make([]ServerStatus, 0, len(m.servers))
	for _, cfg := range m.servers {
		st := ServerStatus{Language: cfg.Language, Command: cfg.Command, Extensions: cfg.Extensions}
		state, err := m.stateOf(cfg.Language)
		st.State = state
		if err != nil {
			st.Err = err.Error()
		}
		m.mu.Lock()
		st.Since = m.since[cfg.Language]
		m.mu.Unlock()
		if state == StateMissing {
			st.Install = InstallHint(cfg.Language)
		}
		out = append(out, st)
	}
	return out
}

// Restart stops language's server; the next request starts it again, and
// the editor's documents are opened in it again.
func (m *Manager) Restart(language string) error {
	m.mu.Lock()
	s := m.running[language]
	delete(m.failed, language)
	m.mu.Unlock()
	if s == nil {
		if m.configFor(language) == nil {
			return fmt.Errorf("%w: %s", ErrNoServer, language)
		}
		return nil
	}
	m.stop(s)
	return nil
}

// configFor is language's server configuration (nil: none).
func (m *Manager) configFor(language string) *Server {
	for i := range m.servers {
		if m.servers[i].Language == language {
			return &m.servers[i]
		}
	}
	return nil
}

// stop shuts s down and forgets it; documents open in it are opened
// again in its successor by their next request.
func (m *Manager) stop(s *server) {
	m.mu.Lock()
	if m.running[s.lang] == s {
		delete(m.running, s.lang)
	}
	m.mu.Unlock()
	s.shutdown()
}
