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
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Document is one of the organization's documents: its terms of service
// (terms) or privacy notice (privacy), as text or a link, with a version.
type Document struct {
	Document  string    `json:"document"`
	Version   string    `json:"version"`
	Text      string    `json:"text,omitempty"`
	URL       string    `json:"url,omitempty"`
	Effective time.Time `json:"effective,omitzero"`
}

// Branding is how the organization's Blitz looks (spec_053_enterprise
// EN-50): its name, accents for the light and dark themes, images as data
// URLs (logo.svg, logo-dark.svg, icon.png, by file name), a support link,
// an about text, and its documents.
type Branding struct {
	Name       string            `json:"name"`
	ShortName  string            `json:"short_name"`
	Accent     string            `json:"accent"`
	AccentDark string            `json:"accent_dark"`
	Support    string            `json:"support"`
	About      string            `json:"about"`
	Images     map[string]string `json:"images"`
	Terms      []Document        `json:"terms"`
}

// ErrSignature means what the service sent (its branding, its catalog)
// doesn't check against the keys it published.
var ErrSignature = errors.New("this didn't come signed by the organization's service")

// envelope is a signed payload, as the service seals it: the payload's
// JSON in base64url, and an Ed25519 signature of "blitz-enterprise/v1",
// NUL, the key's ID, NUL and the payload, so an envelope can't be
// relabelled with another key's ID.
type envelope struct {
	KeyID     string `json:"kid"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// openSigned checks data's signature against keys (key ID → the public
// key, base64) and decodes its payload into v.
func openSigned(data []byte, keys map[string]string, v any) error {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%w: not a signed envelope", ErrSignature)
	}
	pub, err := base64.StdEncoding.DecodeString(keys[env.KeyID])
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: unknown key %q", ErrSignature, env.KeyID)
	}
	payload, err := base64.RawURLEncoding.DecodeString(env.Payload)
	if err != nil {
		return ErrSignature
	}
	sig, err := base64.RawURLEncoding.DecodeString(env.Signature)
	signed := append([]byte("blitz-enterprise/v1\x00"+env.KeyID+"\x00"), payload...)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), signed, sig) {
		return ErrSignature
	}
	return json.Unmarshal(payload, v)
}

// The files a service's branding is kept in, in its own folder (EN-54).
const (
	brandingFile = "branding.json" // the signed copy, as the service sent it
	etagFile     = "branding.etag"
	keysFile     = "keys.json" // the keys it was checked with
)

// writeFile writes data to name in dir, owner-only, through a temporary
// file renamed into place.
func writeFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// LoadBranding is the branding kept in dir, checked again against the keys
// kept with it; nil when there's none, or it doesn't check (Blitz's own
// look until the service answers).
func LoadBranding(dir string) *Branding {
	data, err := os.ReadFile(filepath.Join(dir, brandingFile))
	if err != nil {
		return nil
	}
	var keys map[string]string
	if raw, err := os.ReadFile(filepath.Join(dir, keysFile)); err != nil || json.Unmarshal(raw, &keys) != nil {
		return nil
	}
	var b Branding
	if openSigned(data, keys, &b) != nil {
		return nil
	}
	return &b
}

// KeptName is the organization's name from the branding kept in dir; ""
// for none (Blitz's own name).
func KeptName(dir string) string {
	if dir == "" {
		return ""
	}
	if b := LoadBranding(dir); b != nil {
		return b.Name
	}
	return ""
}

// ForgetBranding removes dir, the service's folder: on disconnecting, the
// app is Blitz again (EN-54).
func ForgetBranding(dir string) error {
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

// catalogKeys are the keys the service signs with, from what it last said
// about itself, asked again when there are none.
func (c *Client) catalogKeys(ctx context.Context, fresh bool) (map[string]string, error) {
	c.mu.Lock()
	keys := c.info.CatalogKeys
	c.mu.Unlock()
	if len(keys) > 0 && !fresh {
		return keys, nil
	}
	info, err := Discover(ctx, c.hc, c.url)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.info = info
	c.mu.Unlock()
	return info.CatalogKeys, nil
}

// RefreshBranding asks the service for its branding (with the ETag of the
// copy kept) and keeps a new one in dir, after checking its signature.
// It needs no sign-in. It returns what's in dir after: the service's
// branding, or nil when it has none (the folder's copy is then removed).
// A copy that doesn't check is refused and the last one kept.
func (c *Client) RefreshBranding(ctx context.Context, dir string) (*Branding, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/v1/branding", nil)
	if err != nil {
		return nil, err
	}
	if tag, err := os.ReadFile(filepath.Join(dir, etagFile)); err == nil && LoadBranding(dir) != nil {
		req.Header.Set("If-None-Match", string(tag))
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return LoadBranding(dir), fmt.Errorf("reaching the organization's service: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return LoadBranding(dir), err
	}
	switch res.StatusCode {
	case http.StatusNotModified:
		return LoadBranding(dir), nil
	case http.StatusNotFound:
		// None published, or the license doesn't carry it: Blitz's look.
		for _, f := range []string{brandingFile, etagFile, keysFile} {
			_ = os.Remove(filepath.Join(dir, f))
		}
		return nil, nil
	case http.StatusOK:
	default:
		return LoadBranding(dir), &Refusal{Status: res.StatusCode}
	}
	var b Branding
	keys, err := c.catalogKeys(ctx, false)
	if err == nil {
		if err = openSigned(data, keys, &b); errors.Is(err, ErrSignature) {
			// The key may have changed since: asked once more.
			if keys, err = c.catalogKeys(ctx, true); err == nil {
				err = openSigned(data, keys, &b)
			}
		}
	}
	if err != nil {
		return LoadBranding(dir), err
	}
	rawKeys, _ := json.Marshal(keys)
	for _, f := range []struct {
		name string
		data []byte
	}{{keysFile, rawKeys}, {brandingFile, data}, {etagFile, []byte(res.Header.Get("ETag"))}} {
		if err := writeFile(dir, f.name, f.data); err != nil {
			return &b, err
		}
	}
	return &b, nil
}

// Terms is the version of the organization's terms in force and whether
// the person has accepted it (EN-53), with the text to read or its link.
type Terms struct {
	Version   string    `json:"version"`
	Accepted  bool      `json:"accepted"`
	Text      string    `json:"text"`
	URL       string    `json:"url"`
	Effective time.Time `json:"effective"`
}

// Terms asks the service for its terms in force.
func (c *Client) Terms(ctx context.Context) (Terms, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return Terms{}, err
	}
	var t Terms
	if err := call(ctx, c.hc, http.MethodGet, c.url+"/api/v1/terms", token, nil, &t); err != nil {
		return Terms{}, err
	}
	c.mu.Lock()
	c.terms = ""
	if !t.Accepted {
		c.terms = t.Version
	}
	c.mu.Unlock()
	return t, nil
}

// AcceptTerms records that the person accepted version of the terms.
func (c *Client) AcceptTerms(ctx context.Context, version string) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	if err := call(ctx, c.hc, http.MethodPost, c.url+"/api/v1/terms/accept", token, map[string]string{"version": version}, nil); err != nil {
		return err
	}
	c.mu.Lock()
	if c.terms == version {
		c.terms = ""
	}
	c.mu.Unlock()
	return nil
}
