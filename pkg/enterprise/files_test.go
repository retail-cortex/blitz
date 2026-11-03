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

package enterprise

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
)

// readOnly makes dir read-only until the test ends.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

func TestFileFailures(t *testing.T) {
	t.Run("not an envelope", func(t *testing.T) {
		assert.ErrorIs(t, openSigned([]byte("not JSON"), nil, &Branding{}), ErrSignature)
	})
	t.Run("a read-only folder", func(t *testing.T) {
		dir := t.TempDir()
		readOnly(t, dir)
		assert.Error(t, writeFile(dir, "x", []byte("x")))
	})
	t.Run("a folder in the file's place", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "x", "y"), 0o700))
		assert.Error(t, writeFile(dir, "x", []byte("x")))
	})
	t.Run("forgetting a catalog that's a folder", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, catalogFile, "x"), 0o700))
		assert.Error(t, ForgetCatalog(dir))
	})
	t.Run("forgetting layers it can't remove", func(t *testing.T) {
		dir := t.TempDir()
		layers := filepath.Join(dir, layersDir)
		require.NoError(t, os.MkdirAll(filepath.Join(layers, "x"), 0o700))
		readOnly(t, layers)
		assert.Error(t, ForgetCatalog(dir))
	})
}

func TestLayOutFailures(t *testing.T) {
	keep := func(t *testing.T, dir string, data string) string {
		sum := sha256.Sum256([]byte(data))
		h := hex.EncodeToString(sum[:])
		require.NoError(t, writeFile(filepath.Join(dir, filesDir), h, []byte(data)))
		return h
	}
	t.Run("a file not kept", func(t *testing.T) {
		assert.Error(t, layOut(t.TempDir(), Manifest{Files: []CatalogFile{{Path: "a.md", SHA256: "00"}}}))
	})
	t.Run("a file where a folder goes", func(t *testing.T) {
		dir := t.TempDir()
		h := keep(t, dir, "A.")
		assert.Error(t, layOut(dir, Manifest{Files: []CatalogFile{{Path: "a", SHA256: h}, {Path: "a/b", SHA256: h}}}))
	})
	t.Run("a folder where a file goes", func(t *testing.T) {
		dir := t.TempDir()
		h := keep(t, dir, "A.")
		assert.Error(t, layOut(dir, Manifest{Files: []CatalogFile{{Path: "a/b", SHA256: h}, {Path: "a", SHA256: h}}}))
	})
	t.Run("a last build it can't remove", func(t *testing.T) {
		dir := t.TempDir()
		next := filepath.Join(dir, layersDir+".next")
		require.NoError(t, os.MkdirAll(filepath.Join(next, "x"), 0o700))
		readOnly(t, next)
		assert.Error(t, layOut(dir, Manifest{}))
	})
}

func TestFetchFailures(t *testing.T) {
	ctx := context.Background()
	sum := sha256.Sum256([]byte("A."))
	files := []CatalogFile{{Path: "a.md", SHA256: hex.EncodeToString(sum[:])}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/short/api/v1/catalog/files/" + files[0].SHA256, "/short/api/v1/branding":
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("A."))
		case "/api/v1/branding":
			_, _ = w.Write([]byte("{}"))
		case "/api/v1/catalog/files/" + files[0].SHA256:
			_, _ = w.Write([]byte("A."))
		default:
			http.Error(w, "no", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	client := func(url string) *Client { return &Client{url: url, hc: srv.Client()} }

	t.Run("a bad address", func(t *testing.T) {
		assert.Error(t, client("http://\x7f").fetchFiles(ctx, "t", t.TempDir(), files))
		_, err := client("http://\x7f").RefreshBranding(ctx, t.TempDir())
		assert.Error(t, err)
	})
	t.Run("a reply cut short", func(t *testing.T) {
		assert.Error(t, client(srv.URL+"/short").fetchFiles(ctx, "t", t.TempDir(), files))
		_, err := client(srv.URL+"/short").RefreshBranding(ctx, t.TempDir())
		assert.Error(t, err)
	})
	t.Run("a service that's gone", func(t *testing.T) {
		gone := httptest.NewServer(http.NotFoundHandler())
		gone.Close()
		assert.Error(t, client(gone.URL).fetchFiles(ctx, "t", t.TempDir(), files))
	})
	t.Run("no keys to check with", func(t *testing.T) {
		_, err := client(srv.URL).RefreshBranding(ctx, t.TempDir())
		assert.Error(t, err)
	})
	t.Run("a store it can't write", func(t *testing.T) {
		dir := t.TempDir()
		store := filepath.Join(dir, filesDir)
		require.NoError(t, os.MkdirAll(store, 0o700))
		readOnly(t, store)
		assert.Error(t, client(srv.URL).fetchFiles(ctx, "t", dir, files))
	})
}

// A job's run gets its access tokens for its secret, renews them near
// their end, and says how it ended, once.
func TestRun(t *testing.T) {
	ctx := context.Background()
	_, s, _, _, now := setup(t)
	s.Runs["r1"] = "s3cret"
	c := NewRun(s.URL, "r1", "s3cret", Options{Now: func() time.Time { return *now }})
	assert.True(t, c.Status().SignedIn, "as the job's owner, by its secret")
	assert.False(t, New(s.URL, nil, Options{}).Status().SignedIn, "no keychain: not signed in")
	tok, err := c.AccessToken(ctx)
	require.NoError(t, err)
	again, err := c.AccessToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, tok, again, "kept while it's good")
	*now = now.Add(10 * time.Minute)
	renewed, err := c.AccessToken(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, tok, renewed)

	require.NoError(t, c.KeepArtifact(ctx, "out/sales by week.csv", strings.NewReader("week,sales\n"), "text/csv"))
	assert.Equal(t, enterprisetest.Artifact{MediaType: "text/csv", Body: "week,sales\n"}, s.Kept["r1"]["out/sales by week.csv"])
	s.KeepRefused = true
	var refused *Refusal
	require.ErrorAs(t, c.KeepArtifact(ctx, "x.txt", strings.NewReader("x"), ""), &refused)
	assert.Equal(t, "no_store", refused.Code)
	s.KeepRefused = false
	assert.Error(t, NewRun(s.URL, "r1", "wrong", Options{}).KeepArtifact(ctx, "x.txt", strings.NewReader("x"), ""))
	assert.Error(t, NewRun("http://127.0.0.1:1", "r1", "s3cret", Options{}).KeepArtifact(ctx, "x.txt", strings.NewReader("x"), ""))
	assert.Error(t, New(s.URL, nil, Options{}).KeepArtifact(ctx, "x.txt", strings.NewReader("x"), ""), "not a run")

	require.ErrorContains(t, c.EndRun(ctx, RunEnd{Status: "won"}), "succeeded, failed, stopped or paused")
	require.ErrorContains(t, c.EndRun(ctx, RunEnd{Status: RunPaused}), "what it asks for")
	require.NoError(t, c.EndRun(ctx, RunEnd{Status: "succeeded", Output: "Done.", Branch: "blitz/jobs/digest/r1", CostUSD: 0.25}))
	assert.Equal(t, map[string]any{"status": "succeeded", "output": "Done.", "branch": "blitz/jobs/digest/r1", "cost_usd": 0.25}, s.Ends["r1"])
	assert.Error(t, c.EndRun(ctx, RunEnd{Status: "succeeded"}), "once")
	*now = now.Add(10 * time.Minute)
	_, err = c.AccessToken(ctx)
	assert.Error(t, err, "the secret's no good after")

	_, err = NewRun(s.URL, "r2", "nope", Options{}).AccessToken(ctx)
	assert.Error(t, err)
	s.Runs["r3"], s.Tokenless = "s", true
	_, err = NewRun(s.URL, "r3", "s", Options{}).AccessToken(ctx)
	assert.ErrorContains(t, err, "no access token")
	assert.Error(t, New(s.URL, nil, Options{}).EndRun(ctx, RunEnd{}), "not a run")
}
