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
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RegistryAsset is one of the organization's published assets as its
// registry lists it (the service's spec_016 RG-10): for a person, its
// state for them (available or required) and whether they have it.
type RegistryAsset struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	State       string `json:"state,omitempty"`
	On          bool   `json:"on,omitempty"`
	URI         string `json:"uri"`
	// Tags are its own; Lifecycle is deprecated or obsolete, with why
	// (Note); Snippet its words around what matched (the service's RG-14).
	Tags      []string `json:"tags,omitempty"`
	Lifecycle string   `json:"lifecycle,omitempty"`
	Note      string   `json:"lifecycle_note,omitempty"`
	Snippet   string   `json:"snippet,omitempty"`
}

// Label is how a's life and tags read beside its name: "(deprecated:
// why) #tag", "" for none.
func (a RegistryAsset) Label() string {
	var parts []string
	if a.Lifecycle != "" {
		l := a.Lifecycle
		if a.Note != "" {
			l += ": " + a.Note
		}
		parts = append(parts, "("+l+")")
	}
	for _, t := range a.Tags {
		parts = append(parts, "#"+t)
	}
	return strings.Join(parts, " ")
}

// RegistrySearch searches the organization's registry as the person
// (EN-96): the published assets their policy offers them whose name or
// description holds q, of kind ("" for any).
func (c *Client) RegistrySearch(ctx context.Context, q, kind string) ([]RegistryAsset, error) {
	return (&Registry{Client: c}).Search(ctx, q, kind)
}

// Registry is an organization's registry for `blitz pkg` (spec_065
// PK-16; the service's spec_016): through the person's sign-in (Client),
// or, for CI and builders outside Blitz, a registry token (URL and
// Token, BLITZ_REGISTRY_TOKEN).
type Registry struct {
	Client *Client
	URL    string
	Token  string
	HTTP   *http.Client
}

// RegistryTokenEnv names the environment variable a registry token is in.
const RegistryTokenEnv = "BLITZ_REGISTRY_TOKEN"

func (r *Registry) do(ctx context.Context, method, path string, in, out any) error {
	if r.Client != nil && r.Token == "" {
		return r.Client.signedCall(ctx, method, path, in, out)
	}
	hc := r.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: time.Minute}
	}
	return call(ctx, hc, method, strings.TrimSuffix(r.URL, "/")+path, r.Token, in, out)
}

// host is the registry's host, as its packages' addresses name it.
func (r *Registry) host() string {
	u := r.URL
	if r.Client != nil && r.URL == "" {
		u = r.Client.url
	}
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// Fetch is the version rng resolves to of kind/name, and its files
// (packages.Registry): host must be this registry's.
func (r *Registry) Fetch(ctx context.Context, host, kind, name, rng string) (string, map[string]string, error) {
	if h := r.host(); host != h {
		return "", nil, fmt.Errorf("blitz://%s: your registry is %s", host, h)
	}
	if rng == "" {
		rng = "latest"
	}
	var out struct {
		Version string            `json:"version"`
		Files   map[string]string `json:"files"`
	}
	err := r.do(ctx, http.MethodGet, "/api/v1/registry/"+url.PathEscape(kind)+"/"+url.PathEscape(name)+"/"+url.PathEscape(rng), nil, &out)
	return out.Version, out.Files, err
}

// Search lists the published assets whose name or description holds q.
func (r *Registry) Search(ctx context.Context, q, kind string) ([]RegistryAsset, error) {
	var out struct {
		Assets []RegistryAsset `json:"assets"`
	}
	err := r.do(ctx, http.MethodGet, "/api/v1/registry/search?q="+url.QueryEscape(q)+"&kind="+url.QueryEscape(kind), nil, &out)
	return out.Assets, err
}

// Published is what a publish to the registry did: the version it
// published, or, while its evals run, that it's being evaluated.
type Published struct {
	Version    string `json:"version"`
	URI        string `json:"uri"`
	Evaluating any    `json:"evaluating,omitempty"`
}

// Publish publishes files as kind/name's next version (bump: patch,
// minor or major), through the library's evals.
func (r *Registry) Publish(ctx context.Context, kind, name string, files map[string]string, bump, note string) (Published, error) {
	var out Published
	err := r.do(ctx, http.MethodPost, "/api/v1/registry/"+url.PathEscape(kind)+"/"+url.PathEscape(name), map[string]any{"files": files, "bump": bump, "note": note}, &out)
	return out, err
}
