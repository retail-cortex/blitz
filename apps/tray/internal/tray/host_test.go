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

package tray

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/legal"
	"github.com/retail-cortex/blitz/pkg/pageserver"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// call sends one of the page's calls to the host and decodes its answer.
func call(t *testing.T, h http.Handler, method string, args ...any) (int, json.RawMessage) {
	t.Helper()
	body, err := json.Marshal(args)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, pageserver.HostPrefix+method, bytes.NewReader(body)))
	return rec.Code, json.RawMessage(rec.Body.Bytes())
}

// The host answers the desktop app's calls the page makes, and lists them.
func TestHost(t *testing.T) {
	f, a := newFake(t)
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	h := Host{Actions: a, Version: "1.2.3", Socket: sock, Tray: "/opt/blitz/blitz-tray"}.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/host/info", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var info struct{ Methods []string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &info))
	assert.Contains(t, info.Methods, "ServiceStatus")
	assert.Contains(t, info.Methods, "RevealPath")
	assert.NotContains(t, info.Methods, "ChooseWorkspace", "only what the host can do")

	file := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	for _, tc := range []struct {
		name   string
		method string
		args   []any
		code   int
		want   string
	}{
		{"the version", "Version", nil, http.StatusOK, `"1.2.3"`},
		{"a program that's there", "ProgramExists", []any{file}, http.StatusOK, "true"},
		{"one that isn't", "ProgramExists", []any{file + ".no"}, http.StatusOK, "false"},
		{"a missing argument", "ProgramExists", nil, http.StatusInternalServerError, `{"error":"missing argument 1"}`},
		{"the notice", "License", []any{"notice"}, http.StatusOK, mustJSON(t, legal.Notice)},
		{"an unknown license", "License", []any{"other"}, http.StatusInternalServerError, `{"error":"unknown license text \"other\""}`},
		{"a document that would run", "OpenDocument", []any{file}, http.StatusInternalServerError, ""},
		{"no folder there", "OpenFolder", []any{file + ".no"}, http.StatusInternalServerError, ""},
		{"not an absolute path", "RevealPath", []any{"a.txt"}, http.StatusInternalServerError, ""},
		{"a wrong argument", "SetTray", []any{"yes"}, http.StatusInternalServerError, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := call(t, h, tc.method, tc.args...)
			assert.Equal(t, tc.code, code, string(body))
			if tc.want != "" {
				assert.JSONEq(t, tc.want, string(body))
			}
		})
	}

	_, body := call(t, h, "ServiceStatus")
	var st ServiceStatus
	require.NoError(t, json.Unmarshal(body, &st))
	assert.Equal(t, ServiceStatus{Socket: sock, Tray: "/opt/blitz/blitz-tray"}, st)

	// The service's controls are the tray's own.
	f.running = true
	code, body := call(t, h, "StopService", 0)
	assert.Equal(t, http.StatusInternalServerError, code, "the fake never stops: %s", body)
	require.NoError(t, os.WriteFile(filepath.Join(a.Beside, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
	f.running = false
	code, body = call(t, h, "RestartService", 0)
	assert.Equal(t, http.StatusOK, code, string(body))
	assert.Contains(t, f.ran, "blitzd")
}

// Calls must be POSTs of a JSON array, to a method the host has.
func TestHostRequests(t *testing.T) {
	_, a := newFake(t)
	h := Host{Actions: a}.Handler()
	for _, tc := range []struct {
		name, method, path, body string
		code                     int
	}{
		{"a GET", http.MethodGet, "/host/Version", "", http.StatusMethodNotAllowed},
		{"no such method", http.MethodPost, "/host/ChooseWorkspace", "[]", http.StatusNotFound},
		{"not an array", http.MethodPost, "/host/Version", "{}", http.StatusInternalServerError},
		{"no body", http.MethodPost, "/host/Version", "", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			assert.Equal(t, tc.code, rec.Code, rec.Body.String())
		})
	}
}

// Installing the service: on Windows the tray starts at login; elsewhere
// the service's login item.
func TestHostInstallService(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the registry")
	}
	f, a := newFake(t)
	h := Host{Actions: a}.Handler()
	code, body := call(t, h, "InstallService")
	assert.Equal(t, http.StatusInternalServerError, code, "no blitzd: %s", body)
	require.NoError(t, os.WriteFile(filepath.Join(a.Beside, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
	code, body = call(t, h, "InstallService")
	assert.Equal(t, http.StatusOK, code, string(body))
	assert.NotEmpty(t, f.ran, "the login item is loaded")
}

// The tray's start-at-login entry goes on and off from the page; the file
// manager is named.
func TestHostSetTrayAndFileManager(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the registry")
	}
	_, a := newFake(t)
	h := Host{Actions: a, Tray: "/opt/blitz/blitz-tray"}.Handler()
	for _, on := range []bool{true, false} {
		code, body := call(t, h, "SetTray", on)
		require.Equal(t, http.StatusOK, code, string(body))
		_, body = call(t, h, "ServiceStatus")
		var st ServiceStatus
		require.NoError(t, json.Unmarshal(body, &st))
		assert.Equal(t, on, st.TrayInstalled)
	}
	code, body := call(t, h, "FileManager")
	assert.Equal(t, http.StatusOK, code, string(body))
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// Stop asks the service to stop before it signals it.
func TestStopAsksFirst(t *testing.T) {
	f, a := newFake(t)
	f.running = true
	asked := 0
	a.Shutdown = func() error { asked++; f.running = false; return nil }
	require.NoError(t, a.Stop())
	assert.Equal(t, 1, asked)

	// A service that can't be asked is signalled (here: no process, so it
	// stays up).
	f.running = true
	a.Shutdown = func() error { return errors.New("unimplemented") }
	assert.ErrorContains(t, a.Stop(), "still running")
}

// Shutdown reaches the service's Shutdown over its socket.
func TestShutdown(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	asked := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.Handle(pb.NewWorkspaceServiceHandler(shutdownService{asked: asked}))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	require.NoError(t, Shutdown(context.Background(), sock))
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("not asked")
	}
	assert.Error(t, Shutdown(context.Background(), filepath.Join(shortTemp(t), "none.sock")))
}

type shutdownService struct {
	pb.UnimplementedWorkspaceServiceHandler
	asked chan struct{}
}

func (s shutdownService) Shutdown(context.Context, *connect.Request[pb.ShutdownRequest]) (*connect.Response[pb.ShutdownResponse], error) {
	s.asked <- struct{}{}
	return connect.NewResponse(&pb.ShutdownResponse{}), nil
}
