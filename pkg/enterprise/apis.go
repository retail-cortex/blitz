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
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// CallAPI calls one of the organization's API connectors through its
// service, as the signed-in person (spec_053 EN-91; the service's
// spec_013 CN-71): method on path under the connector, with query and
// body; header's Content-Type and Accept go along. The service adds the
// organization's token and logs the call. Its answer is the server's, as
// it gave it; the caller closes its body.
func (c *Client) CallAPI(ctx context.Context, connector, method, path, query string, body []byte, header http.Header) (*http.Response, error) {
	// The path stays under the connector: a ".." (or one escaped, %2e%2e)
	// would reach the person's other endpoints on the service with their
	// token (security review enterprise#5).
	if unescaped, err := url.PathUnescape(path); err != nil || slices.Contains(strings.Split(strings.ReplaceAll(unescaped, `\`, "/"), "/"), "..") {
		return nil, fmt.Errorf("an API step's path can't leave its connector: %q", path)
	}
	u := c.url + "/api/v1/apis/" + url.PathEscape(connector) + "/" + strings.TrimLeft(path, "/")
	if query != "" {
		u += "?" + query
	}
	send := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for _, k := range []string{"Content-Type", "Accept"} {
			if v := header.Get(k); v != "" {
				req.Header.Set(k, v)
			}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return c.hc.Do(req)
	}
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	res, err := send(token)
	if err != nil || res.StatusCode != http.StatusUnauthorized || c.run != nil {
		return res, err
	}
	// The service ended the token early: renewed once, and asked again.
	res.Body.Close()
	c.Invalidate()
	if token, err = c.AccessToken(ctx); err != nil {
		return nil, err
	}
	return send(token)
}

// Receipt is what a workflow a git hook ran found (EN-92): which
// workflow, its version (the organization's, "" for the workspace's own),
// the event, the tree it ran on, and whether it passed.
type Receipt struct {
	Workflow  string    `json:"workflow"`
	Version   string    `json:"version,omitempty"`
	Event     string    `json:"event"`
	Repo      string    `json:"repo,omitempty"`
	Tree      string    `json:"tree"`
	Passed    bool      `json:"passed"`
	Message   string    `json:"message,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	At        time.Time `json:"at"`
}

// Receipt tells the service what a git hook's workflow found (the
// service's spec_014 LB-40).
func (c *Client) Receipt(ctx context.Context, r Receipt) error {
	return c.signedCall(ctx, http.MethodPost, "/api/v1/me/receipts", r, nil)
}
