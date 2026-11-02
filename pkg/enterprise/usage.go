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
	"time"
)

// Used is what was used of the organization's models: requests, tokens
// and cost.
type Used struct {
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	Cached       int64   `json:"cached_input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// Limit is a budget (dollars) or a quota (tokens) that applies to the
// person, what they used of it, and when it starts again.
type Limit struct {
	// ScopeKind is person, group or org: whose it is.
	ScopeKind string `json:"scope_kind"`
	Period    string `json:"period"` // day or month
	Unit      string `json:"unit"`   // usd or tokens
	// Model is the model it limits ("" for every model together).
	Model        string    `json:"model"`
	AmountUSD    float64   `json:"amount_usd"`
	Tokens       int64     `json:"tokens"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	Used         Used      `json:"used"`
	Resets       time.Time `json:"resets"`
}

// RateLimit is how many requests and tokens a minute one of the models
// takes: everyone's together, and each person's.
type RateLimit struct {
	Model     string `json:"model"`
	PersonRPM int    `json:"person_rpm"`
	PersonTPM int    `json:"person_tpm"`
	RPM       int    `json:"rpm"`
	TPM       int    `json:"tpm"`
}

// Usage is the person's own use of the organization's models (spec_053
// EN-24; the gateway's GW-27): today's and this month's by model, the
// limits that apply with what's left, and whether full prompt logs keep
// their requests.
type Usage struct {
	Today      map[string]Used `json:"today"`
	Month      map[string]Used `json:"month"`
	Limits     []Limit         `json:"limits"`
	RateLimits []RateLimit     `json:"rate_limits"`
	PromptLogs bool            `json:"prompt_logs"`
}

// Usage asks the service for the person's usage.
func (c *Client) Usage(ctx context.Context) (Usage, error) {
	var u Usage
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/usage", nil, &u)
	return u, err
}

// Notices are how the person hears of their jobs' runs on the service
// (the enterprise spec's JB-31): by email, by a webhook (https), and for
// every run or only failures and runs waiting for their answer.
type Notices struct {
	Email    bool   `json:"email"`
	Webhook  string `json:"webhook"`
	EveryRun bool   `json:"every_run"`
	// EmailAvailable says the service sends email at all.
	EmailAvailable bool `json:"email_available,omitempty"`
}

// Notices are the person's notice settings.
func (c *Client) Notices(ctx context.Context) (Notices, error) {
	var n Notices
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/notices", nil, &n)
	return n, err
}

// SetNotices keeps the person's notice settings, answered as kept.
func (c *Client) SetNotices(ctx context.Context, n Notices) (Notices, error) {
	var out Notices
	err := c.signedCall(ctx, http.MethodPut, "/api/v1/notices", map[string]any{"email": n.Email, "webhook": n.Webhook, "every_run": n.EveryRun}, &out)
	return out, err
}
