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
	"os"
	"path/filepath"
	"regexp"
	"time"

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

// PolicyEnvelope is what the service signs at /api/v1/policy (its
// spec_012_policy PO-20): the person's effective policy, why each setting
// is what it is, their rules and guardrails, and until when it holds.
type PolicyEnvelope struct {
	Schema     int                              `json:"schema"`
	Policy     Policy                           `json:"policy"`
	Explain    map[string]config.PolicyDecision `json:"explain"`
	Rules      []PolicyEntry                    `json:"rules"`
	Guardrails []PolicyEntry                    `json:"guardrails"`
	Expires    time.Time                        `json:"expires"`
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
	if tag, err := os.ReadFile(filepath.Join(dir, policyETag)); err == nil {
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
	if err := layOutPolicy(dir, env); err != nil {
		return false, err
	}
	// What the settings read as they load: the policy, why, the expiry.
	state, _ := json.Marshal(struct {
		Policy  Policy                           `json:"policy"`
		Explain map[string]config.PolicyDecision `json:"explain"`
		Expires time.Time                        `json:"expires"`
	}{env.Policy, env.Explain, env.Expires})
	for _, f := range []struct {
		name string
		data []byte
	}{{PolicyFile, state}, {policyETag, []byte(res.Header.Get("ETag"))}} {
		if err := writeFile(dir, f.name, f.data); err != nil {
			return true, err
		}
	}
	return true, nil
}

// layOutPolicy writes env's rules and guardrails to dir/policy as a
// workspace's .agents folder (rules/<name>.md, guardrails/<name>.md),
// built beside the old ones and swapped in whole.
func layOutPolicy(dir string, env PolicyEnvelope) error {
	next := filepath.Join(dir, policyDir+".next")
	if err := os.RemoveAll(next); err != nil {
		return err
	}
	for _, set := range []struct {
		sub     string
		entries []PolicyEntry
	}{{"rules", env.Rules}, {"guardrails", env.Guardrails}} {
		if err := os.MkdirAll(filepath.Join(next, set.sub), 0o700); err != nil {
			return err
		}
		for _, e := range set.entries {
			if !entryName.MatchString(e.Name) || len(e.Name) > 64 {
				return fmt.Errorf("the organization's policy names a %s that can't be a file: %q", set.sub, e.Name)
			}
			if err := os.WriteFile(filepath.Join(next, set.sub, e.Name+".md"), []byte(e.Body), 0o600); err != nil {
				return err
			}
		}
	}
	old := filepath.Join(dir, policyDir+".old")
	_ = os.RemoveAll(old)
	cur := filepath.Join(dir, policyDir)
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
