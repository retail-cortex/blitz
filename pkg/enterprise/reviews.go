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
	"net/http"
	"net/url"
	"time"
)

// Review is a change to the organization's catalog an Admin published,
// waiting for an Admin (its author too), or answered (spec_053 EN-36; the service's
// CA-03, CA-04).
type Review struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	ByEmail  string    `json:"by_email"`
	Branch   string    `json:"branch"`
	Paths    []string  `json:"paths"`
	Problems []string  `json:"problems"`
	State    string    `json:"state"` // open, approved or rejected
	Note     string    `json:"note"`
	Version  *int      `json:"version"`
	Created  time.Time `json:"created"`
}

// PublishCatalog sends files (by their paths in the catalog) to the
// service as a review, for an Admin (its author too) to approve.
func (c *Client) PublishCatalog(ctx context.Context, title string, files map[string][]byte) (Review, error) {
	type file struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	in := struct {
		Title string `json:"title"`
		Files []file `json:"files"`
	}{Title: title}
	for p, data := range files {
		in.Files = append(in.Files, file{p, string(data)})
	}
	var r Review
	err := c.signedCall(ctx, http.MethodPost, "/api/v1/admin/catalog/reviews", in, &r)
	return r, err
}

// CatalogReviews are the catalog's reviews, the newest first; state ""
// for all.
func (c *Client) CatalogReviews(ctx context.Context, state string) ([]Review, error) {
	var out struct {
		Reviews []Review `json:"reviews"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/admin/catalog/reviews?state="+url.QueryEscape(state), nil, &out)
	return out.Reviews, err
}
