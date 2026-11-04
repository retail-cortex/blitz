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
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// runCreds are a job's run's: its ID and its own secret.
type runCreds struct{ id, secret string }

// NewRun is a run's connection to the organization's service, for a job
// that runs on it (spec_009_jobs JB-10, JB-11): its access tokens, as the
// job's owner, come for the run's secret instead of a person's sign-in.
func NewRun(u, run, secret string, o Options) *Client {
	c := New(u, nil, o)
	c.run = &runCreds{id: run, secret: secret}
	return c
}

// runToken renews a run's access token; c.mu is held.
func (c *Client) runToken(ctx context.Context) (string, error) {
	if c.access != "" && c.now().Before(c.renewAt()) {
		return c.access, nil
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := call(ctx, c.hc, http.MethodPost, c.runURL("token"), "", map[string]string{"secret": c.run.secret}, &t); err != nil {
		c.access = ""
		return "", err
	}
	if t.AccessToken == "" {
		return "", errors.New("the organization's service sent no access token for the run")
	}
	c.access, c.life = t.AccessToken, time.Duration(t.ExpiresIn)*time.Second
	c.expires = c.now().Add(c.life)
	return c.access, nil
}

func (c *Client) runURL(what string) string {
	return c.url + "/api/v1/runs/" + url.PathEscape(c.run.id) + "/" + what
}

// RunEnd is how a run ended, for the service.
type RunEnd struct {
	// Status is succeeded, failed or stopped.
	Status string `json:"status"`
	// Note says why it failed or stopped.
	Note string `json:"note,omitempty"`
	// Output is the run's last message.
	Output string `json:"output,omitempty"`
	// Branch is where its changes went ("" for none).
	Branch string `json:"branch,omitempty"`
	// Merged is whether its changes are on main too; MergeNote why not,
	// when they're on its branch only (JB-10).
	Merged    bool    `json:"merged,omitempty"`
	MergeNote string  `json:"merge_note,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
	// A paused run's (RunPaused): what it asks for, and its session.
	Pending *Request `json:"pending,omitempty"`
	Session string   `json:"session,omitempty"`
}

// RunPaused is the status of a run that waits for its owner's answer
// (blitz-enterprise spec_009_jobs JB-13).
const RunPaused = "paused"

// Request is what a paused run asks for: a tool, its kind, and what
// exactly.
type Request struct {
	Tool   string `json:"tool"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// EndRun tells the service how the run ended; after it, the run's secret
// is good for nothing.
func (c *Client) EndRun(ctx context.Context, end RunEnd) error {
	if c.run == nil {
		return errors.New("not a job's run")
	}
	body := struct {
		Secret string `json:"secret"`
		RunEnd
	}{c.run.secret, end}
	return call(ctx, c.hc, http.MethodPost, c.runURL("end"), "", body, nil)
}

// KeepArtifact keeps one of the run's files at path (out/report.csv) as
// its artifact, urn:blitz:artifacts:<workspace>:<job>:<run>/<path>
// (spec_009_jobs JB-30): while the run goes, with its secret.
func (c *Client) KeepArtifact(ctx context.Context, path string, body io.Reader, mediaType string) error {
	if c.run == nil {
		return errors.New("not a job's run")
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.runURL("artifacts/"+strings.Join(segs, "/")), body)
	if err != nil {
		return err
	}
	req.Header.Set("Blitz-Run-Secret", c.run.secret)
	if mediaType != "" {
		req.Header.Set("Content-Type", mediaType)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return refusal(res)
	}
	return nil
}
