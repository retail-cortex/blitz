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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/agentpkg"
	"github.com/retail-cortex/blitz/pkg/config"
)

// PolicyEntry is one of the organization's rules or guardrails the
// person's policy switches on, with its text.
type PolicyEntry struct {
	Kind   string `json:"kind"` // rule or guardrail
	Name   string `json:"name"`
	Body   string `json:"body"`
	SHA256 string `json:"sha256"`
}

// PolicyItem is one of the library's assets as the service delivers it
// (its spec_014_library LB-21): what the person's Blitz lays out (EN-80),
// its files when they have it.
type PolicyItem struct {
	config.OrgAsset
	Files map[string]string `json:"files,omitempty"`
}

// PolicySchema is the newest policy envelope this client reads. A newer
// one may hold restrictions it doesn't know, so it isn't taken: the policy
// kept stays, and with none, the strictest applies (EN-33b; security
// review enterprise#6). 0 is an envelope from before the field.
const PolicySchema = 2

// ErrPolicySchema is a policy envelope newer than this client reads.
var ErrPolicySchema = errors.New("your organization's policy is newer than this version of Blitz reads: update Blitz")

// PolicyEnvelope is what the service signs at /api/v1/policy (its
// spec_012_policy PO-20): the person's effective policy, why each setting
// is what it is, their rules and guardrails, and until when it holds.
type PolicyEnvelope struct {
	Schema     int                              `json:"schema"`
	Policy     Policy                           `json:"policy"`
	Explain    map[string]config.PolicyDecision `json:"explain"`
	Rules      []PolicyEntry                    `json:"rules"`
	Guardrails []PolicyEntry                    `json:"guardrails"`
	// Items are the library's assets it offers them (EN-80); none from a
	// service before the library.
	Items   []PolicyItem `json:"items"`
	Expires time.Time    `json:"expires"`
}

// ErrNoPolicyAPI means the service predates /api/v1/policy: the policy
// the catalog's manifest carries stands (EN-38).
var ErrNoPolicyAPI = errors.New("the organization's service doesn't serve a policy of its own")

// The policy's files in the organization's folder (EN-38, EN-39).
const (
	policyETag = "policy.etag"
	policyDir  = "policy" // rules/ and guardrails/, as a workspace's .agents
)

// entryName is a rule's or guardrail's name, as the service allows.
var entryName = regexp.MustCompile(`^[a-z0-9]+([-_][a-z0-9]+)*$`)

// RefreshPolicy asks the service for the person's effective policy (with
// the ETag of the one kept), checks its signature, and writes it to dir:
// policy.json with why each setting is what it is, and the rules and
// guardrails it switches on, built beside the old ones and swapped in
// whole. It reports whether it changed. A service before /api/v1/policy
// answers 404: ErrNoPolicyAPI, and the catalog's policy stands.
func (c *Client) RefreshPolicy(ctx context.Context, dir string) (bool, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/v1/policy", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// The ETag only stands for a policy kept: with policy.json gone, the
	// service's 304 would leave none for ever (security review
	// enterprise#9).
	if tag, err := os.ReadFile(filepath.Join(dir, policyETag)); err == nil && fileExists(filepath.Join(dir, "policy.json")) {
		req.Header.Set("If-None-Match", string(tag))
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return false, fmt.Errorf("reaching the organization's service: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return false, err
	}
	switch res.StatusCode {
	case http.StatusNotModified:
		return false, nil
	case http.StatusNotFound:
		return false, ErrNoPolicyAPI
	case http.StatusOK:
	default:
		var r struct{ Code, Message string }
		_ = json.Unmarshal(data, &r)
		if res.StatusCode == http.StatusUnauthorized {
			c.Invalidate()
		}
		return false, &Refusal{Status: res.StatusCode, Code: r.Code, Message: r.Message}
	}
	var env PolicyEnvelope
	keys, err := c.catalogKeys(ctx, false)
	if err == nil {
		if err = openSigned(data, keys, &env); errors.Is(err, ErrSignature) {
			if keys, err = c.catalogKeys(ctx, true); err == nil {
				err = openSigned(data, keys, &env)
			}
		}
	}
	if err != nil {
		return false, err
	}
	if env.Schema > PolicySchema {
		return false, fmt.Errorf("%w (schema %d; this reads %d)", ErrPolicySchema, env.Schema, PolicySchema)
	}
	next, err := stagePolicy(dir, env)
	if err != nil {
		return false, err
	}
	// What the settings read as they load: the policy, why, the expiry.
	// Written before swapping policy/ into place so readers never see the
	// new rules or guardrails ahead of their matching policy.json.
	state, err := json.Marshal(struct {
		Policy  Policy                           `json:"policy"`
		Explain map[string]config.PolicyDecision `json:"explain"`
		Expires time.Time                        `json:"expires"`
	}{env.Policy, env.Explain, env.Expires})
	if err != nil {
		return false, fmt.Errorf("encoding policy state: %w", err)
	}
	prev, hadPrev := os.ReadFile(filepath.Join(dir, PolicyFile))
	if err := writeFile(dir, PolicyFile, state); err != nil {
		return false, err
	}
	if err := swapPolicy(dir, next); err != nil {
		// The old policy/ is back in place (swapPolicy): so is the
		// policy.json that goes with it (EN-33c, security review
		// enterprise#10).
		if hadPrev == nil {
			_ = writeFile(dir, PolicyFile, prev)
		} else {
			_ = os.Remove(filepath.Join(dir, PolicyFile))
		}
		return false, err
	}
	if err := writeFile(dir, policyETag, []byte(res.Header.Get("ETag"))); err != nil {
		return true, err
	}
	return true, nil
}

// stagePolicy writes env's rules, guardrails and library items to
// dir/policy.next (rules/<name>.md, guardrails/<name>.md), ready to be
// swapped into dir/policy whole.
func stagePolicy(dir string, env PolicyEnvelope) (string, error) {
	next := filepath.Join(dir, policyDir+".next")
	if err := os.RemoveAll(next); err != nil {
		return "", err
	}
	for _, set := range []struct {
		sub     string
		entries []PolicyEntry
	}{{"rules", env.Rules}, {"guardrails", env.Guardrails}} {
		if err := os.MkdirAll(filepath.Join(next, set.sub), 0o700); err != nil {
			return "", err
		}
		for _, e := range set.entries {
			if !entryName.MatchString(e.Name) || len(e.Name) > 64 {
				return "", fmt.Errorf("the organization's policy names a %s that can't be a file: %q", set.sub, e.Name)
			}
			if err := os.WriteFile(filepath.Join(next, set.sub, e.Name+".md"), []byte(e.Body), 0o600); err != nil {
				return "", err
			}
		}
	}
	if err := layOutItems(next, env.Items); err != nil {
		return "", err
	}
	return next, nil
}

// swapPolicy replaces dir/policy with the staged directory next.
func swapPolicy(dir, next string) error {
	old := filepath.Join(dir, policyDir+".old")
	_ = os.RemoveAll(old)
	cur := filepath.Join(dir, policyDir)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return err
		}
	}
	if err := os.Rename(next, cur); err != nil {
		// Never no policy/ at all: the old one goes back.
		if _, statErr := os.Stat(old); statErr == nil {
			_ = os.Rename(old, cur)
		}
		return err
	}
	return os.RemoveAll(old)
}

// policyTookOver reports whether dir keeps a policy from the policy API
// (/api/v1/policy, EN-38): its ETag, and the policy it stands for.
func policyTookOver(dir string) bool {
	return fileExists(filepath.Join(dir, policyETag)) && fileExists(filepath.Join(dir, PolicyFile))
}

// itemRoot is where an asset of kind named name keeps its files, as a
// workspace's .agents folder lays it out: its file, or its folder.
func itemRoot(kind, name string) (string, bool) {
	switch kind {
	case "agent":
		return "agents/" + name + ".md", false
	case "skill":
		return "skills/" + name + "/", true
	case "workflow":
		return "workflows/" + name + ".flow.yaml", false
	}
	return "", false
}

// layOutItems writes the library's assets the person has into next
// (EN-80): each under available/ or required/ as its state is, and each
// skill's version (theirs, and every one an asset of theirs locks to)
// under versions/skills/<name>@<version>/ (EN-82); and items.json, what's
// offered. Rules and guardrails are laid out from the envelope's own
// lists. An asset's files must be its own (an agent's one file, a skill's
// folder) and match its digest, or nothing is laid out.
func layOutItems(next string, items []PolicyItem) error {
	index := make([]config.OrgAsset, 0, len(items))
	for _, it := range items {
		index = append(index, it.OrgAsset)
		root, folder := itemRoot(it.Kind, it.Name)
		if !it.On || root == "" {
			continue
		}
		if !entryName.MatchString(it.Name) || len(it.Name) > 64 {
			return fmt.Errorf("the organization offers a %s that can't be a file: %q", it.Kind, it.Name)
		}
		// A skill's version names its folder under versions/ (packages#2).
		if it.Kind == "skill" && !agentpkg.ValidVersion(it.Version) {
			return fmt.Errorf("the organization offers skill %s with a version that can't be a folder: %q", it.Name, it.Version)
		}
		if got := agentpkg.Digest(it.Files); got != it.SHA256 {
			return fmt.Errorf("the organization's %s %s %s doesn't match its digest", it.Kind, it.Name, it.Version)
		}
		for p, text := range it.Files {
			clean := path.Clean(p)
			if clean != p || strings.Contains(p, "..") || (folder && !strings.HasPrefix(p, root)) || (!folder && p != root) {
				return fmt.Errorf("the organization's %s %s has a file that isn't its own: %q", it.Kind, it.Name, p)
			}
			var dests []string
			if it.State == config.OrgAvailable || it.State == config.OrgRequired {
				dests = append(dests, filepath.Join(next, it.State, filepath.FromSlash(p)))
			}
			if it.Kind == "skill" {
				dests = append(dests, filepath.Join(next, config.OrgVersions, "skills", it.Name+"@"+it.Version, filepath.FromSlash(strings.TrimPrefix(p, root))))
			}
			for _, d := range dests {
				if err := os.MkdirAll(filepath.Dir(d), 0o700); err != nil {
					return err
				}
				if err := os.WriteFile(d, []byte(text), 0o600); err != nil {
					return err
				}
			}
		}
	}
	b, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(next, config.OrgItemsFile), b, 0o600)
}

// SetItem switches one of the library's available assets on or off for
// the person (EN-83; the service's spec_014_library LB-22): kind is agent,
// skill, workflow, rule or guardrail. A required one can't be switched off
// (a Refusal, required).
func (c *Client) SetItem(ctx context.Context, kind, name string, on bool) (config.OrgAsset, error) {
	var out config.OrgAsset
	err := c.signedCall(ctx, http.MethodPut, "/api/v1/me/items/"+url.PathEscape(kind)+"/"+url.PathEscape(name), map[string]any{"on": on}, &out)
	return out, err
}

// fileExists reports whether a file is at path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
