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

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGo makes the workspace's Go language server the fake one (this
// test binary, run again as lsptest's server).
func fakeGo(cfg *config.Config) {
	exe, _ := os.Executable()
	cfg.LSP = map[string]config.LSPServerConfig{
		"go": {Command: []string{"/bin/sh", "-c", `BLITZ_FAKE_LSP=1 exec "$0"`, exe}, Extensions: []string{".go"}},
	}
	cfg.Sandbox.Shell = "off" // the fake is a test binary, wherever Bazel put it
}

// languageClient is a LanguageService client of the service s serves.
func languageClient(t *testing.T, s *Server) pb.LanguageServiceClient {
	t.Helper()
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return pb.NewLanguageServiceClient(http.DefaultClient, srv.URL)
}

// The editor's round: open a document, complete, hover, find the
// definition, watch its problems go as it's fixed, and close it.
func TestLanguageService(t *testing.T) {
	_, s := serve(t, fakeGo)
	lc := languageClient(t, s)
	ctx := context.Background()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Foo() {}\n\nvar _ = Foo\n"), 0o644))

	opened, err := lc.OpenDocument(ctx, connect.NewRequest(&pb.OpenDocumentRequest{Workspace: dir, Client: "w1", Path: "main.go", Text: "package main\nfunc Foo() {}\n\nvar _ = Foo\n", Version: 1}))
	require.NoError(t, err)
	doc := opened.Msg.Document
	require.NotEmpty(t, doc)
	assert.Equal(t, "go", opened.Msg.Language)
	assert.Contains(t, []pb.ServerState{pb.ServerState_SERVER_STATE_READY, pb.ServerState_SERVER_STATE_STARTING}, opened.Msg.State)

	watch, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := lc.WatchDiagnostics(watch, connect.NewRequest(&pb.WatchDiagnosticsRequest{Workspace: dir, Client: "w1"}))
	require.NoError(t, err)
	defer stream.Close()
	next := func() *pb.WatchDiagnosticsResponse {
		t.Helper()
		got := make(chan *pb.WatchDiagnosticsResponse, 1)
		go func() {
			if stream.Receive() {
				got <- stream.Msg()
			}
			close(got)
		}()
		select {
		case m := <-got:
			require.NotNil(t, m, "the stream ended: %v", stream.Err())
			return m
		case <-time.After(10 * time.Second):
			t.Fatal("no diagnostics")
			return nil
		}
	}
	d := next()
	assert.Equal(t, doc, d.Document)
	require.Len(t, d.Diagnostics, 1)
	assert.Equal(t, "fake problem", d.Diagnostics[0].Message)
	assert.Equal(t, &pb.TextRange{Start: &pb.TextPosition{Line: 1, Column: 3}, End: &pb.TextPosition{Line: 1, Column: 6}}, d.Diagnostics[0].Range)

	comp, err := lc.Complete(ctx, connect.NewRequest(&pb.CompleteRequest{Workspace: dir, Document: doc, Version: 1, Position: &pb.TextPosition{Line: 4, Column: 9}}))
	require.NoError(t, err)
	require.Len(t, comp.Msg.Items, 1)
	assert.Equal(t, "Foo", comp.Msg.Items[0].Label)
	assert.Equal(t, "function", comp.Msg.Items[0].Kind)
	assert.Equal(t, "Foo does nothing.", comp.Msg.Items[0].Documentation)

	hover, err := lc.Hover(ctx, connect.NewRequest(&pb.HoverRequest{Workspace: dir, Document: doc, Version: 1, Position: &pb.TextPosition{Line: 2, Column: 6}}))
	require.NoError(t, err)
	assert.Contains(t, hover.Msg.Text, "func Foo()")
	def, err := lc.Definition(ctx, connect.NewRequest(&pb.DefinitionRequest{Workspace: dir, Document: doc, Version: 1, Position: &pb.TextPosition{Line: 4, Column: 9}}))
	require.NoError(t, err)
	require.Len(t, def.Msg.Locations, 1)
	assert.Equal(t, "main.go", def.Msg.Locations[0].Path)
	assert.False(t, def.Msg.Locations[0].Outside)
	refs, err := lc.References(ctx, connect.NewRequest(&pb.ReferencesRequest{Workspace: dir, Document: doc, Version: 1, Position: &pb.TextPosition{Line: 2, Column: 6}}))
	require.NoError(t, err)
	assert.Len(t, refs.Msg.Locations, 2)

	// Fixed in the editor, unsaved: the problem goes.
	_, err = lc.ChangeDocument(ctx, connect.NewRequest(&pb.ChangeDocumentRequest{Workspace: dir, Document: doc, Version: 2, Text: "package main // fixed\n"}))
	require.NoError(t, err)
	d = next()
	assert.Equal(t, int64(2), d.Version)
	assert.Empty(t, d.Diagnostics)

	// An older version, and an unknown document, are refused with reasons.
	_, err = lc.Hover(ctx, connect.NewRequest(&pb.HoverRequest{Workspace: dir, Document: doc, Version: 1, Position: &pb.TextPosition{Line: 1, Column: 1}}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeAborted, code)
	assert.Equal(t, "STALE", info.Reason)

	kept, err := lc.KeepDocuments(ctx, connect.NewRequest(&pb.KeepDocumentsRequest{Workspace: dir, Client: "w1"}))
	require.NoError(t, err)
	assert.Equal(t, []string{doc}, kept.Msg.Documents)

	status, err := lc.GetLanguageStatus(ctx, connect.NewRequest(&pb.GetLanguageStatusRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.False(t, status.Msg.Untrusted)
	var goServer *pb.LanguageServerStatus
	for _, st := range status.Msg.Servers {
		if st.Language == "go" {
			goServer = st
		}
	}
	require.NotNil(t, goServer)
	assert.Equal(t, pb.ServerState_SERVER_STATE_READY, goServer.State)
	assert.NotNil(t, goServer.Since)

	_, err = lc.RestartLanguageServer(ctx, connect.NewRequest(&pb.RestartLanguageServerRequest{Workspace: dir, Language: "go"}))
	require.NoError(t, err)
	_, err = lc.RestartLanguageServer(ctx, connect.NewRequest(&pb.RestartLanguageServerRequest{Workspace: dir, Language: "cobol"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NO_SERVER", info.Reason)
	// The next request starts it again, with the document's text.
	hover, err = lc.Hover(ctx, connect.NewRequest(&pb.HoverRequest{Workspace: dir, Document: doc, Version: 2, Position: &pb.TextPosition{Line: 1, Column: 1}}))
	require.NoError(t, err)
	assert.Contains(t, hover.Msg.Text, "func Foo()")

	_, err = lc.CloseDocument(ctx, connect.NewRequest(&pb.CloseDocumentRequest{Workspace: dir, Document: doc}))
	require.NoError(t, err)
	_, err = lc.CloseDocument(ctx, connect.NewRequest(&pb.CloseDocumentRequest{Workspace: dir, Document: doc}))
	require.NoError(t, err, "closing one already closed is fine")
	_, err = lc.ChangeDocument(ctx, connect.NewRequest(&pb.ChangeDocumentRequest{Workspace: dir, Document: doc, Version: 3, Text: ""}))
	_, info = errorReason(t, err)
	assert.Equal(t, "UNKNOWN_DOCUMENT", info.Reason)
}

// Files without a server, bad paths and a missing server (a workspace
// waiting for trust is the engine's test: TestOpenDocumentUntrusted).
func TestLanguageServiceStates(t *testing.T) {
	ctx := context.Background()
	t.Run("no server, bad path", func(t *testing.T) {
		_, s := serve(t, fakeGo)
		lc := languageClient(t, s)
		dir := t.TempDir()
		res, err := lc.OpenDocument(ctx, connect.NewRequest(&pb.OpenDocumentRequest{Workspace: dir, Client: "w", Path: "notes.txt"}))
		require.NoError(t, err)
		assert.Equal(t, pb.ServerState_SERVER_STATE_NONE, res.Msg.State)
		assert.Empty(t, res.Msg.Document)
		_, err = lc.OpenDocument(ctx, connect.NewRequest(&pb.OpenDocumentRequest{Workspace: dir, Client: "w", Path: "../out.go"}))
		_, info := errorReason(t, err)
		assert.Equal(t, "BAD_PATH", info.Reason)
	})
	t.Run("missing", func(t *testing.T) {
		_, s := serve(t, func(cfg *config.Config) {
			cfg.LSP = map[string]config.LSPServerConfig{"go": {Command: []string{"no-such-gopls"}, Extensions: []string{".go"}}}
		})
		lc := languageClient(t, s)
		dir := t.TempDir()
		var res *connect.Response[pb.OpenDocumentResponse]
		require.Eventually(t, func() bool {
			var err error
			res, err = lc.OpenDocument(ctx, connect.NewRequest(&pb.OpenDocumentRequest{Workspace: dir, Client: "w", Path: "a.go", Version: 1}))
			require.NoError(t, err)
			return res.Msg.State == pb.ServerState_SERVER_STATE_MISSING
		}, 10*time.Second, 20*time.Millisecond)
		assert.Contains(t, res.Msg.Detail, "no-such-gopls isn't installed")
		assert.Equal(t, "go install golang.org/x/tools/gopls@latest", res.Msg.Install)
		_, err := lc.Hover(ctx, connect.NewRequest(&pb.HoverRequest{Workspace: dir, Document: res.Msg.Document, Version: 1, Position: &pb.TextPosition{Line: 1, Column: 1}}))
		_, info := errorReason(t, err)
		assert.Equal(t, "SERVER_MISSING", info.Reason)
	})
}
