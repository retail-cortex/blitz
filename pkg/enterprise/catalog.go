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
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// CatalogFile is one of the catalog's files for this person: what it is
// (an agent, rule, skill or skill's file, guardrail, command, MCP server
// or connector), its name, its path in the catalog, how it applies
// (locked or default) and its contents' SHA-256.
type CatalogFile struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Lock   string   `json:"lock"`
	Groups []string `json:"groups,omitempty"`
	SHA256 string   `json:"sha256"`
	Size   int      `json:"size"`
}

// Policy is the organization's policy (spec_053_enterprise EN-35), as far
// as Blitz applies it.
type Policy struct {
	// Refresh is how often to fetch the catalog when its stream isn't up,
	// as a Go duration ("15m" when unset).
	Refresh string `json:"refresh"`
	Models  struct {
		// OwnProviders lets people use their own providers beside the
		// organization's models.
		OwnProviders bool `json:"own_providers"`
	} `json:"models"`
	Tools struct {
		// Off are tools no agent may use.
		Off []string `json:"off"`
		// ShellCommands lets people run their own ! commands.
		ShellCommands bool `json:"shell_commands"`
	} `json:"tools"`
	MCP struct {
		// Own lets people add their own MCP servers.
		Own bool `json:"own"`
	} `json:"mcp"`
	Builtins struct {
		// Off are Blitz's built-in agents and skills switched off, by name.
		Off []string `json:"off"`
	} `json:"builtins"`
}

// Manifest is a version of the catalog for this person.
type Manifest struct {
	Version int           `json:"version"`
	Commit  string        `json:"commit"`
	Built   time.Time     `json:"built"`
	Files   []CatalogFile `json:"files"`
	Policy  Policy        `json:"policy"`
	// Expires is when Blitz stops working under it unrenewed (EN-33).
	Expires time.Time `json:"expires"`
}

// The catalog's files in the organization's folder (EN-33, EN-54).
const (
	catalogFile  = "catalog.json" // the signed manifest, as the service sent it
	catalogETag  = "catalog.etag"
	filesDir     = "files"   // the files by SHA-256
	layersDir    = "catalog" // default/ and locked/, laid out as .agents
	PolicyFile   = "policy.json"
	defaultLayer = "default"
	lockedLayer  = "locked"
)

// LoadCatalog is the manifest kept in dir, checked again against the keys
// kept with it; nil when there's none or it doesn't check.
func LoadCatalog(dir string) *Manifest {
	data, err := os.ReadFile(filepath.Join(dir, catalogFile))
	if err != nil {
		return nil
	}
	var keys map[string]string
	if raw, err := os.ReadFile(filepath.Join(dir, keysFile)); err != nil || json.Unmarshal(raw, &keys) != nil {
		return nil
	}
	var m Manifest
	if openSigned(data, keys, &m) != nil {
		return nil
	}
	return &m
}

// LayerDirs are the catalog's layers in dir: its default items, which a
// workspace or the person may override, and its locked ones, which apply
// over theirs (EN-31). Both are laid out as a workspace's .agents folder.
func LayerDirs(dir string) (defaults, locked string) {
	return filepath.Join(dir, layersDir, defaultLayer), filepath.Join(dir, layersDir, lockedLayer)
}

// ForgetCatalog removes the catalog from dir, keeping the branding.
func ForgetCatalog(dir string) error {
	for _, f := range []string{catalogFile, catalogETag, PolicyFile} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, d := range []string{layersDir, filesDir} {
		if err := os.RemoveAll(filepath.Join(dir, d)); err != nil {
			return err
		}
	}
	return nil
}

// safePath is p as a path inside a layer, or false for one that would
// leave it.
func safePath(p string) (string, bool) {
	clean := path.Clean(p)
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return filepath.FromSlash(clean), true
}

// RefreshCatalog asks the service for the person's catalog (with the ETag
// of the one kept), checks its signature and each file's SHA-256, and
// lays it out in dir: the default and locked layers, built beside the
// old ones and swapped in whole, and the policy. It returns the catalog
// in dir after, and whether it changed; nil when the service has none.
// A catalog that doesn't check is refused and the last one kept.
func (c *Client) RefreshCatalog(ctx context.Context, dir string) (*Manifest, bool, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return LoadCatalog(dir), false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/v1/catalog", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if tag, err := os.ReadFile(filepath.Join(dir, catalogETag)); err == nil && LoadCatalog(dir) != nil {
		req.Header.Set("If-None-Match", string(tag))
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return LoadCatalog(dir), false, fmt.Errorf("reaching the organization's service: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return LoadCatalog(dir), false, err
	}
	switch res.StatusCode {
	case http.StatusNotModified:
		return LoadCatalog(dir), false, nil
	case http.StatusNotFound:
		had := LoadCatalog(dir) != nil
		return nil, had, ForgetCatalog(dir)
	case http.StatusOK:
	default:
		var r struct{ Code, Message string }
		_ = json.Unmarshal(data, &r)
		if res.StatusCode == http.StatusUnauthorized {
			c.Invalidate()
		}
		return LoadCatalog(dir), false, &Refusal{Status: res.StatusCode, Code: r.Code, Message: r.Message}
	}
	var m Manifest
	keys, err := c.catalogKeys(ctx, false)
	if err == nil {
		if err = openSigned(data, keys, &m); errors.Is(err, ErrSignature) {
			if keys, err = c.catalogKeys(ctx, true); err == nil {
				err = openSigned(data, keys, &m)
			}
		}
	}
	if err != nil {
		return LoadCatalog(dir), false, err
	}
	if err := c.fetchFiles(ctx, token, dir, m.Files); err != nil {
		return LoadCatalog(dir), false, err
	}
	if err := layOut(dir, m); err != nil {
		return LoadCatalog(dir), false, err
	}
	rawKeys, _ := json.Marshal(keys)
	// What the settings read as they load: the policy and the expiry.
	policy, _ := json.Marshal(struct {
		Policy  Policy    `json:"policy"`
		Expires time.Time `json:"expires"`
	}{m.Policy, m.Expires})
	for _, f := range []struct {
		name string
		data []byte
	}{{keysFile, rawKeys}, {PolicyFile, policy}, {catalogFile, data}, {catalogETag, []byte(res.Header.Get("ETag"))}} {
		if err := writeFile(dir, f.name, f.data); err != nil {
			return &m, true, err
		}
	}
	return &m, true, nil
}

// fetchFiles fetches the files dir hasn't kept, checking each one's
// SHA-256, and drops the kept ones the catalog no longer has.
func (c *Client) fetchFiles(ctx context.Context, token, dir string, files []CatalogFile) error {
	store := filepath.Join(dir, filesDir)
	if err := os.MkdirAll(store, 0o700); err != nil {
		return err
	}
	want := map[string]bool{}
	for _, f := range files {
		want[f.SHA256] = true
		if _, err := os.Stat(filepath.Join(store, f.SHA256)); err == nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/v1/catalog/files/"+f.SHA256, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := c.hc.Do(req)
		if err != nil {
			return fmt.Errorf("reaching the organization's service: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
		res.Body.Close()
		if err != nil {
			return err
		}
		if res.StatusCode != http.StatusOK {
			return &Refusal{Status: res.StatusCode, Message: fmt.Sprintf("the organization's catalog file %s: %s", f.Path, res.Status)}
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.SHA256 {
			return fmt.Errorf("%w: %s doesn't match its hash", ErrSignature, f.Path)
		}
		if err := writeFile(store, f.SHA256, data); err != nil {
			return err
		}
	}
	entries, _ := os.ReadDir(store)
	for _, e := range entries {
		if !want[e.Name()] {
			_ = os.Remove(filepath.Join(store, e.Name()))
		}
	}
	return nil
}

// layOut builds the layers for m from the kept files beside the old ones,
// then swaps them in.
func layOut(dir string, m Manifest) error {
	next := filepath.Join(dir, layersDir+".next")
	if err := os.RemoveAll(next); err != nil {
		return err
	}
	for _, f := range m.Files {
		p, ok := safePath(f.Path)
		if !ok {
			return fmt.Errorf("the organization's catalog names a file outside it: %s", f.Path)
		}
		layer := defaultLayer
		if f.Lock == "locked" {
			layer = lockedLayer
		}
		data, err := os.ReadFile(filepath.Join(dir, filesDir, f.SHA256))
		if err != nil {
			return err
		}
		to := filepath.Join(next, layer, p)
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o600); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(next, 0o700); err != nil {
		return err
	}
	old := filepath.Join(dir, layersDir+".old")
	_ = os.RemoveAll(old)
	cur := filepath.Join(dir, layersDir)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return err
		}
	}
	if err := os.Rename(next, cur); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

// WatchCatalog follows the service's stream of catalog versions, calling
// on for each one published, until ctx ends or the stream does (the
// error says why).
func (c *Client) WatchCatalog(ctx context.Context, on func()) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/v1/catalog/watch", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the organization's service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return &Refusal{Status: res.StatusCode}
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "event: catalog") {
			on()
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("the organization's service ended the catalog's stream")
}
