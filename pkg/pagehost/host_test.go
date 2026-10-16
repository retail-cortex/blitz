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

package pagehost

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/legal"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/pageserver"
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
	svc, beside, _ := fakeSystem(t)
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	h := Host{Service: svc, Beside: beside, Version: "1.2.3", Socket: sock, Tray: "/opt/blitz/blitz-tray"}.Handler()

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
		{"a path missing", "OpenFolder", nil, http.StatusInternalServerError, `{"error":"missing argument 1"}`},
		{"which license, missing", "License", nil, http.StatusInternalServerError, `{"error":"missing argument 1"}`},
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

	// The service's controls are the host's (the tray's Actions).
	code, body := call(t, h, "StopService", 0)
	assert.Equal(t, http.StatusOK, code, string(body))
	code, body = call(t, h, "RestartService", 0)
	assert.Equal(t, http.StatusOK, code, string(body))
	assert.Equal(t, []string{"stop", "restart"}, svc.calls)
	svc.err = errors.New("no blitzd")
	code, body = call(t, h, "StopService", 0)
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.JSONEq(t, `{"error":"no blitzd"}`, string(body))
}

// Calls must be POSTs of a JSON array, to a method the host has.
func TestHostRequests(t *testing.T) {
	h := Host{}.Handler()
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
	svc, beside, ran := fakeSystem(t)
	h := Host{Service: svc, Beside: beside}.Handler()
	code, body := call(t, h, "InstallService")
	assert.Equal(t, http.StatusInternalServerError, code, "no blitzd: %s", body)
	require.NoError(t, os.WriteFile(filepath.Join(beside, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
	code, body = call(t, h, "InstallService")
	assert.Equal(t, http.StatusOK, code, string(body))
	assert.NotEmpty(t, *ran, "the login item is loaded")
}

// The tray's start-at-login entry goes on and off from the page; the file
// manager is named.
func TestHostSetTrayAndFileManager(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the registry")
	}
	svc, beside, _ := fakeSystem(t)
	h := Host{Service: svc, Beside: beside, Tray: "/opt/blitz/blitz-tray"}.Handler()
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

// The preferences, the sandbox and (but on Windows) the blitz command are
// the host's too, the preferences only when it keeps them.
func TestHostPrefsSandboxAndCLI(t *testing.T) {
	names := func(h http.Handler) []string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/host/info", nil))
		var info struct{ Methods []string }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &info))
		return info.Methods
	}
	assert.NotContains(t, names(Host{}.Handler()), "GetPrefs", "no store: the page keeps its own")

	h := Host{Prefs: NewPrefsStore(filepath.Join(t.TempDir(), "desktop.json"))}.Handler()
	all := names(h)
	for _, m := range []string{"GetPrefs", "SavePrefs", "SandboxStatus", "FixSandbox"} {
		assert.Contains(t, all, m)
	}
	cli := []string{"CLIStatus", "InstallCLI", "ReinstallCLI"}
	for _, m := range cli {
		if goruntime.GOOS == "windows" {
			assert.NotContains(t, all, m)
		} else {
			assert.Contains(t, all, m)
		}
	}

	code, body := call(t, h, "GetPrefs")
	require.Equal(t, http.StatusOK, code, string(body))
	var p Prefs
	require.NoError(t, json.Unmarshal(body, &p))
	p.Theme = "dark"
	code, body = call(t, h, "SavePrefs", p)
	require.Equal(t, http.StatusOK, code, string(body))
	_, body = call(t, h, "GetPrefs")
	require.NoError(t, json.Unmarshal(body, &p))
	assert.Equal(t, "dark", p.Theme, "saved in the file")
	code, _ = call(t, h, "SavePrefs")
	assert.Equal(t, http.StatusInternalServerError, code, "no preferences to save")

	code, body = call(t, h, "SandboxStatus")
	require.Equal(t, http.StatusOK, code, string(body))
	assert.Contains(t, string(body), `"state"`)
	if goruntime.GOOS != "windows" {
		code, body = call(t, h, "CLIStatus")
		assert.Equal(t, http.StatusOK, code, string(body))
	}
}

// fakeService records what the host asks of the service.
type fakeService struct {
	calls   []string
	running bool
	err     error
}

func (f *fakeService) Start() error   { f.calls = append(f.calls, "start"); return f.err }
func (f *fakeService) Stop() error    { f.calls = append(f.calls, "stop"); return f.err }
func (f *fakeService) Restart() error { f.calls = append(f.calls, "restart"); return f.err }
func (f *fakeService) Running() bool  { return f.running }

// fakeSystem is a host's world for a test: a fake service, an empty
// directory beside it (no blitzd), no blitzd on PATH, a home of its own,
// and the login item's system commands recorded instead of run.
func fakeSystem(t *testing.T) (*fakeService, string, *[]string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	var ran []string
	old := loginitem.RunSystem
	loginitem.RunSystem = func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { loginitem.RunSystem = old })
	return &fakeService{}, t.TempDir(), &ran
}

// shortTemp is a directory short enough for a socket's path.
func shortTemp(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "host")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
