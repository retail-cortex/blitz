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

package runtime

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/genai"
)

// QuotaError is a request refused because a quota is used up: Google's
// RESOURCE_EXHAUSTED (Vertex AI, the Gemini API), which also comes as a
// 429 but, unlike a rate limit, waiting a moment rarely helps. Message is
// Google's, which names the quota.
type QuotaError struct {
	Message string
	Err     error
}

// Error says it's a quota, what Google said, and where to raise it.
func (e *QuotaError) Error() string {
	msg := "quota exceeded"
	if e.Message != "" {
		msg += ": " + strings.TrimSpace(e.Message)
	}
	return msg + " (raise the quota in the Google Cloud console, IAM & Admin › Quotas, or wait for it to reset)"
}

// Unwrap is the provider's error.
func (e *QuotaError) Unwrap() error { return e.Err }

// asQuota returns err as a *QuotaError when it's a quota refusal, else err.
func asQuota(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*QuotaError](err); ok {
		return err
	}
	if g, ok := errors.AsType[genai.APIError](err); ok {
		if g.Status == "RESOURCE_EXHAUSTED" {
			return &QuotaError{Message: g.Message, Err: err}
		}
		return err
	}
	if a, ok := errors.AsType[*anthropic.Error](err); ok {
		if a.StatusCode == 429 {
			if msg, ok := googleExhausted(a.RawJSON()); ok {
				return &QuotaError{Message: msg, Err: err}
			}
		}
		return err
	}
	return err
}

// googleExhausted reads a Google API error body, alone or in a list (as
// Vertex AI sends them), and returns its message when its status is
// RESOURCE_EXHAUSTED.
func googleExhausted(body string) (string, bool) {
	type googleError struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	var list []googleError
	if json.Unmarshal([]byte(body), &list) != nil {
		var one googleError
		if json.Unmarshal([]byte(body), &one) != nil {
			return "", false
		}
		list = []googleError{one}
	}
	for _, e := range list {
		if e.Error.Status == "RESOURCE_EXHAUSTED" {
			return e.Error.Message, true
		}
	}
	return "", false
}
