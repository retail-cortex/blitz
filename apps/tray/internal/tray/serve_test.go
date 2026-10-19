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
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/pageserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Serve writes a link that opens the page, a new one when asked, and
// stops when its input ends.
func TestServe(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>Blitz</title>"), 0o600))
	page, err := PageDir(dir)
	require.NoError(t, err)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Serve(pageserver.Options{Page: page, Socket: filepath.Join(shortTemp(t), "none.sock")}, inR, outW)
		outW.Close()
	}()
	lines := bufio.NewScanner(outR)
	next := func() Link {
		t.Helper()
		require.True(t, lines.Scan(), "a link")
		var l Link
		require.NoError(t, json.Unmarshal(lines.Bytes(), &l))
		require.True(t, strings.HasPrefix(l.URL, "https://127.0.0.1:"), l.URL)
		require.NotEmpty(t, l.Fingerprint)
		return l
	}

	link := next()
	first := link.URL
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	// The window's way: trust the certificate the line names, and no other.
	client := &http.Client{Jar: jar, Transport: pinned(link.Fingerprint)}
	res, err := client.Get(first)
	require.NoError(t, err)
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, 2, res.ProtoMajor, "HTTP/2")
	_, err = (&http.Client{Transport: pinned("00:11")}).Get(first)
	assert.Error(t, err, "another certificate isn't trusted")
	assert.Contains(t, string(body), "<title>Blitz</title>", "the page from the directory")

	_, err = inW.Write([]byte("something else\nlink\n"))
	require.NoError(t, err)
	again := next()
	assert.NotEqual(t, first, again.URL, "a new link each time")
	assert.Equal(t, link.Fingerprint, again.Fingerprint, "the same server")

	require.NoError(t, inW.Close())
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("still serving after its input ended")
	}
	_, err = client.Get(first)
	assert.Error(t, err, "no longer serving")
}

// pinned trusts only the certificate with fingerprint, as the Electron
// app's window does, and speaks only HTTP/2.
func pinned(fingerprint string) *http.Transport {
	var p http.Protocols
	p.SetHTTP2(true)
	return &http.Transport{Protocols: &p, TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true, // checked below, by the fingerprint
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || pageserver.Fingerprint(raw[0]) != fingerprint {
				return errors.New("not the page server's certificate")
			}
			return nil
		},
	}}
}

// A directory without the page is refused.
func TestPageDirRefusesOtherDirectories(t *testing.T) {
	_, err := PageDir(t.TempDir())
	assert.ErrorContains(t, err, "isn't the desktop page")
}

// Serve says when it can't serve, or can't write its link.
func TestServeFailures(t *testing.T) {
	page, err := PageDir(writePage(t))
	require.NoError(t, err)
	assert.Error(t, Serve(pageserver.Options{Page: page, Addr: "example.com:80"}, strings.NewReader(""), io.Discard), "not a loopback address")
	assert.Error(t, Serve(pageserver.Options{Page: page}, strings.NewReader(""), failingWriter{}), "the link can't be written")
	var w limitedWriter
	assert.Error(t, Serve(pageserver.Options{Page: page}, strings.NewReader("link\n"), &w), "the second link can't be written")
}

// writePage is a folder with the page's index.html.
func writePage(t *testing.T) string {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>Blitz</title>"), 0o600))
	return dir
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// limitedWriter takes one write, then fails.
type limitedWriter struct{ n int }

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.n++; w.n > 1 {
		return 0, errors.New("closed")
	}
	return len(p), nil
}
