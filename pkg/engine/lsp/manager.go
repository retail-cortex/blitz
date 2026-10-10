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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// Server is how to run a language's server, and which files it's for.
type Server struct {
	Language   string
	Command    []string
	Extensions []string // with the dot: .go
}

// ErrNoServer means no language server is configured for the file.
var ErrNoServer = errors.New("no language server for this kind of file")

// Manager starts language servers as files need them, one per language,
// and keeps the files it asks about in sync with the disk.
type Manager struct {
	root    string
	launch  Launcher
	servers []Server

	// life ends the servers' starts when the manager closes.
	life    context.Context
	end     context.CancelFunc
	mu      sync.Mutex
	running map[string]*server   // by language
	start   map[string]*starting // starts under way, by language
	failed  map[string]failure   // why a language's server couldn't start
	since   map[string]time.Time // when each language's server started, or failed to
	closed  bool

	docs documents // the editor's (documents.go)
}

// starting is a server's start, which every caller for its language waits
// on: s or err once done is closed.
type starting struct {
	done chan struct{}
	s    *server
	err  error
}

// failure is a start that failed, and when: it's tried again after
// failedRetry.
type failure struct {
	err error
	at  time.Time
}

// failedRetry is how long a server that couldn't start isn't tried again
// (a fixed install or a flaky start recovers without reopening).
var failedRetry = time.Minute

// NewManager is a manager for the workspace root.
func NewManager(root string, servers []Server, launch Launcher) *Manager {
	life, end := context.WithCancel(context.Background())
	return &Manager{root: root, launch: launch, servers: servers, life: life, end: end,
		running: map[string]*server{}, start: map[string]*starting{}, failed: map[string]failure{}, since: map[string]time.Time{}}
}

type server struct {
	lang string
	c    *conn

	mu      sync.Mutex
	text    map[string]string // what was sent, by URI: the open documents
	version map[string]int
	opened  []string                  // the agent's open documents' URIs, least recently synced first
	held    map[string]int            // the editor's documents, by URI: how many hold each
	diags   map[string]publishedDiags // for open documents only
	changed chan struct{}             // a diagnostics notification came
	// onDiags is told of each URI whose diagnostics came (the editor's
	// watchers); nil for none.
	onDiags func(uri string)
	// timeouts counts the editor's requests in a row the server didn't
	// answer in time (VE-45).
	timeouts int
}

type publishedDiags struct {
	at      time.Time
	version int // the document's version they're about (0: unsaid)
	items   []wireDiagnostic
}

// startTimeout bounds starting a server (initialize).
const startTimeout = 60 * time.Second

// For finds the server for path, starting it if need be. A start runs on
// its own (startTimeout, or until Close), not on ctx: callers wait for it,
// each giving up with its own ctx, and other languages go on meanwhile.
func (m *Manager) For(ctx context.Context, path string) (*server, error) {
	ext := strings.ToLower(filepath.Ext(path))
	var cfg *Server
	for i := range m.servers {
		for _, e := range m.servers[i].Extensions {
			if strings.EqualFold(e, ext) {
				cfg = &m.servers[i]
			}
		}
	}
	if cfg == nil {
		return nil, fmt.Errorf("%w (%s)", ErrNoServer, ext)
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errClosed
	}
	if s := m.running[cfg.Language]; s != nil {
		select {
		case <-s.c.done: // it died: start again
		default:
			m.mu.Unlock()
			return s, nil
		}
	}
	if f, ok := m.failed[cfg.Language]; ok {
		if time.Since(f.at) < failedRetry {
			m.mu.Unlock()
			return nil, f.err
		}
		delete(m.failed, cfg.Language)
	}
	st := m.start[cfg.Language]
	if st == nil {
		st = &starting{done: make(chan struct{})}
		m.start[cfg.Language] = st
		go m.startFor(*cfg, st)
	}
	m.mu.Unlock()
	select {
	case <-st.done:
		return st.s, st.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// startFor starts cfg's server for st and records how it went. A server
// that started after Close is shut down at once.
func (m *Manager) startFor(cfg Server, st *starting) {
	s, err := m.startServer(m.life, cfg)
	if err != nil {
		err = fmt.Errorf("starting the %s language server (%s): %w", cfg.Language, strings.Join(cfg.Command, " "), err)
	}
	m.mu.Lock()
	delete(m.start, cfg.Language)
	m.since[cfg.Language] = time.Now()
	switch {
	case m.closed:
		if s != nil {
			defer s.shutdown()
		}
		s, err = nil, errClosed
	case err != nil:
		m.failed[cfg.Language] = failure{err: err, at: time.Now()}
	default:
		m.running[cfg.Language] = s
	}
	st.s, st.err = s, err
	close(st.done)
	m.mu.Unlock()
}

func (m *Manager) startServer(ctx context.Context, cfg Server) (*server, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("no command")
	}
	proc, err := m.launch(context.Background(), cfg.Command) // lives past ctx
	if err != nil {
		return nil, err
	}
	s := &server{lang: cfg.Language, text: map[string]string{}, version: map[string]int{}, held: map[string]int{}, diags: map[string]publishedDiags{}, changed: make(chan struct{}, 1), onDiags: m.diagnosed}
	s.c = newConn(proc, s.notified)
	ictx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	root := fileURI(m.root)
	err = s.c.call(ictx, "initialize", map[string]any{
		"processId":        os.Getpid(),
		"rootUri":          root,
		"workspaceFolders": []any{map[string]any{"uri": root, "name": filepath.Base(m.root)}},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{"didSave": false},
				// The editor's completion (spec_visual_editor_037 VE-32).
				"completion": map[string]any{
					"contextSupport": true,
					"completionItem": map[string]any{
						"snippetSupport":          true,
						"documentationFormat":     []string{"markdown", "plaintext"},
						"insertReplaceSupport":    true,
						"labelDetailsSupport":     true,
						"deprecatedSupport":       true,
						"preselectSupport":        false,
						"insertTextModeSupport":   map[string]any{"valueSet": []int{1}},
						"commitCharactersSupport": false,
					},
				},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": true},
				"references":         map[string]any{},
				"publishDiagnostics": map[string]any{"relatedInformation": false, "versionSupport": true},
			},
			"workspace": map[string]any{"symbol": map[string]any{}, "configuration": true, "workspaceFolders": true},
		},
	}, nil)
	if err != nil {
		proc.Stop()
		return nil, err
	}
	if err := s.c.send("initialized", map[string]any{}); err != nil {
		proc.Stop()
		return nil, err
	}
	// pyright waits for this before it asks for its settings and answers
	// anything (a client with workspace folders and configuration, as
	// ours, is expected to send it, as VS Code does).
	if err := s.c.send("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{}}); err != nil {
		proc.Stop()
		return nil, err
	}
	return s, nil
}

func (s *server) notified(method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string           `json:"uri"`
		Version     int              `json:"version"`
		Diagnostics []wireDiagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	uri := normalURI(p.URI)
	s.mu.Lock()
	// Servers report on files nobody opened (gopls, a whole package's):
	// only open documents' are asked for, so only theirs are kept.
	_, open := s.text[uri]
	if open {
		s.diags[uri] = publishedDiags{at: time.Now(), version: p.Version, items: p.Diagnostics}
	}
	held, onDiags := s.held[uri] > 0, s.onDiags
	s.mu.Unlock()
	if open && held && onDiags != nil {
		onDiags(uri)
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// maxOpen is how many documents a server has open at once.
var maxOpen = 32

// sync sends path's content as it is on disk: opened the first time,
// changed after, nothing when it's the same. It returns the content.
func (s *server) sync(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	uri := fileURI(path)
	s.mu.Lock()
	if s.held[uri] > 0 { // the editor's text, unsaved edits included (VE-41)
		held := s.text[uri]
		s.mu.Unlock()
		return held, nil
	}
	prev, open := s.text[uri]
	if open && prev == text {
		s.mu.Unlock()
		return text, nil
	}
	s.version[uri]++
	v := s.version[uri]
	s.text[uri] = text
	delete(s.diags, uri) // stale
	s.opened = slices.DeleteFunc(s.opened, func(u string) bool { return u == uri })
	s.opened = append(s.opened, uri)
	// The least recently synced documents beyond maxOpen are closed: their
	// text isn't kept here, nor by the server.
	var closing []string
	for len(s.opened) > maxOpen {
		old := s.opened[0]
		s.opened = s.opened[1:]
		delete(s.text, old)
		delete(s.version, old)
		delete(s.diags, old)
		closing = append(closing, old)
	}
	s.mu.Unlock()
	for _, old := range closing {
		if err := s.c.send("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": old}}); err != nil {
			return "", err
		}
	}
	if !open {
		return text, s.c.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": languageID(s.lang, path), "version": v, "text": text,
		}})
	}
	return text, s.c.send("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": v},
		"contentChanges": []any{map[string]any{"text": text}},
	})
}

// Location is a place in a file, for people: the path (relative to the
// workspace when inside it), 1-based line and column, and the line.
type Location struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text,omitempty"`
}

// Diagnostic is a problem a server reports.
type Diagnostic struct {
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

// Symbol is a workspace symbol.
type Symbol struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Container string   `json:"container,omitempty"`
	Location  Location `json:"location"`
}

type wirePosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type wireRange struct {
	Start wirePosition `json:"start"`
	End   wirePosition `json:"end"`
}

type wireLocation struct {
	URI         string     `json:"uri"`
	Range       wireRange  `json:"range"`
	TargetURI   string     `json:"targetUri"`
	TargetRange *wireRange `json:"targetSelectionRange"`
}

type wireDiagnostic struct {
	Range    wireRange `json:"range"`
	Severity int       `json:"severity"`
	Message  string    `json:"message"`
	Source   string    `json:"source"`
	// Code is a number or a string.
	Code json.RawMessage `json:"code,omitempty"`
}

// position is (1-based line, 1-based column in characters) in text as
// LSP's (0-based line, UTF-16 offset).
func position(text string, line, col int) (wirePosition, error) {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		return wirePosition{}, fmt.Errorf("line %d: the file has %d", line, len(lines))
	}
	runes := []rune(strings.TrimSuffix(lines[line-1], "\r"))
	if col < 1 {
		col = 1
	}
	if col > len(runes)+1 {
		col = len(runes) + 1
	}
	return wirePosition{Line: line - 1, Character: len(utf16.Encode(runes[:col-1]))}, nil
}

// readable turns a wire location into one for people.
func (m *Manager) readable(uri string, pos wirePosition) Location {
	path := uriPath(uri)
	loc := Location{Path: path, Line: pos.Line + 1, Column: pos.Character + 1}
	if rel, err := filepath.Rel(m.root, path); err == nil && !strings.HasPrefix(rel, "..") {
		loc.Path = filepath.ToSlash(rel)
	}
	if data, err := os.ReadFile(path); err == nil {
		lines := strings.Split(string(data), "\n")
		if pos.Line < len(lines) {
			line := strings.TrimSuffix(lines[pos.Line], "\r")
			units := utf16.Encode([]rune(line))
			if pos.Character <= len(units) {
				loc.Column = len(utf16.Decode(units[:pos.Character])) + 1
			}
			loc.Text = strings.TrimSpace(line)
			if len(loc.Text) > 200 {
				loc.Text = loc.Text[:200] + "…"
			}
		}
	}
	return loc
}

// at prepares a request about path at (line, col).
func (m *Manager) at(ctx context.Context, path string, line, col int) (*server, map[string]any, error) {
	s, err := m.For(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	text, err := s.sync(path)
	if err != nil {
		return nil, nil, err
	}
	pos, err := position(text, line, col)
	if err != nil {
		return nil, nil, err
	}
	return s, map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}, "position": pos}, nil
}

// maxResults bounds the locations and symbols returned.
const maxResults = 100

// Definition is where the symbol at (line, col) of path is defined.
func (m *Manager) Definition(ctx context.Context, path string, line, col int) ([]Location, error) {
	s, params, err := m.at(ctx, path, line, col)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := s.c.call(ctx, "textDocument/definition", params, &raw); err != nil {
		return nil, err
	}
	return m.locations(raw), nil
}

// References are where the symbol at (line, col) of path is used.
func (m *Manager) References(ctx context.Context, path string, line, col int) ([]Location, error) {
	s, params, err := m.at(ctx, path, line, col)
	if err != nil {
		return nil, err
	}
	params["context"] = map[string]any{"includeDeclaration": true}
	var raw json.RawMessage
	if err := s.c.call(ctx, "textDocument/references", params, &raw); err != nil {
		return nil, err
	}
	return m.locations(raw), nil
}

func (m *Manager) locations(raw json.RawMessage) []Location {
	var many []wireLocation
	if json.Unmarshal(raw, &many) != nil {
		var one wireLocation
		if json.Unmarshal(raw, &one) != nil || (one.URI == "" && one.TargetURI == "") {
			return nil
		}
		many = []wireLocation{one}
	}
	var out []Location
	for _, l := range many {
		uri, r := l.URI, l.Range
		if l.TargetURI != "" { // a LocationLink
			uri = l.TargetURI
			if l.TargetRange != nil {
				r = *l.TargetRange
			}
		}
		out = append(out, m.readable(uri, r.Start))
		if len(out) == maxResults {
			break
		}
	}
	return out
}

// Hover is what the server says about the symbol at (line, col).
func (m *Manager) Hover(ctx context.Context, path string, line, col int) (string, error) {
	s, params, err := m.at(ctx, path, line, col)
	if err != nil {
		return "", err
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := s.c.call(ctx, "textDocument/hover", params, &h); err != nil {
		return "", err
	}
	return hoverText(h.Contents), nil
}

// hoverText reads MarkupContent, a MarkedString, or a list of them.
func hoverText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var mc struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &mc) == nil && mc.Value != "" {
		return mc.Value
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		var parts []string
		for _, item := range list {
			if t := hoverText(item); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

var symbolKinds = []string{"", "file", "module", "namespace", "package", "class", "method", "property", "field", "constructor",
	"enum", "interface", "function", "variable", "constant", "string", "number", "boolean", "array", "object", "key",
	"null", "enum member", "struct", "event", "operator", "type parameter"}

// Symbols are the workspace's symbols matching query, from the server for
// files like path.
func (m *Manager) Symbols(ctx context.Context, path, query string) ([]Symbol, error) {
	s, err := m.For(ctx, path)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name      string       `json:"name"`
		Kind      int          `json:"kind"`
		Container string       `json:"containerName"`
		Location  wireLocation `json:"location"`
	}
	if err := s.c.call(ctx, "workspace/symbol", map[string]any{"query": query}, &raw); err != nil {
		return nil, err
	}
	var out []Symbol
	for _, r := range raw {
		kind := ""
		if r.Kind > 0 && r.Kind < len(symbolKinds) {
			kind = symbolKinds[r.Kind]
		}
		out = append(out, Symbol{Name: r.Name, Kind: kind, Container: r.Container, Location: m.readable(r.Location.URI, r.Location.Range.Start)})
		if len(out) == maxResults {
			break
		}
	}
	return out, nil
}

var severities = []string{"", "error", "warning", "information", "hint"}

// Diagnostics are the problems the server reports for path as it is on
// disk, waiting up to wait for them.
func (m *Manager) Diagnostics(ctx context.Context, path string, wait time.Duration) ([]Diagnostic, error) {
	s, err := m.For(ctx, path)
	if err != nil {
		return nil, err
	}
	if _, err := s.sync(path); err != nil {
		return nil, err
	}
	uri := fileURI(path)
	deadline := time.After(wait)
	for {
		s.mu.Lock()
		d, ok := s.diags[uri]
		s.mu.Unlock()
		if ok {
			out := make([]Diagnostic, 0, len(d.items))
			for _, w := range d.items {
				sev := "error"
				if w.Severity > 0 && w.Severity < len(severities) {
					sev = severities[w.Severity]
				}
				loc := m.readable(uri, w.Range.Start)
				out = append(out, Diagnostic{Line: loc.Line, Column: loc.Column, Severity: sev, Message: w.Message, Source: w.Source})
			}
			sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
			return out, nil
		}
		select {
		case <-s.changed:
		case <-deadline:
			return nil, nil // nothing reported in time
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Running reports whether a server runs for files like path (edits then
// report diagnostics without starting one).
func (m *Manager) Running(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cfg := range m.servers {
		for _, e := range cfg.Extensions {
			if strings.EqualFold(e, ext) && m.running[cfg.Language] != nil {
				return true
			}
		}
	}
	return false
}

// Close shuts the servers down, and ends the starts under way, waiting
// for them.
func (m *Manager) Close() {
	m.mu.Lock()
	servers := m.running
	m.running = map[string]*server{}
	m.closed = true
	starts := make([]*starting, 0, len(m.start))
	for _, st := range m.start {
		starts = append(starts, st)
	}
	m.mu.Unlock()
	m.end()
	for _, st := range starts {
		<-st.done
	}
	for _, s := range servers {
		s.shutdown()
	}
}

// shutdown asks the server to exit, then stops its process.
func (s *server) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	s.c.call(ctx, "shutdown", nil, nil)
	s.c.send("exit", nil)
	cancel()
	s.c.proc.Stop()
	select {
	case <-s.c.done:
	case <-time.After(2 * time.Second):
	}
}

func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

func normalURI(uri string) string { return fileURI(uriPath(uri)) }

func uriPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}

// languageID is LSP's name for a file's language.
func languageID(lang, path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".go":
		return "go"
	}
	return lang
}
