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

// Package redact masks secrets in text before it is written to disk.
package redact

import (
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

const mask = "[REDACTED]"

// Format is a credential format: what it is, and a pattern that matches
// one whole.
type Format struct {
	Kind    string
	Pattern *regexp.Regexp
}

// Formats are the credential formats Blitz knows, the one table the
// redactor, the secrets guardrail's description and the secret scan
// (pkg/secretscan, its hook's pattern too) read.
var Formats = []Format{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{"AWS access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"Anthropic API key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`)},
	{"OpenAI API key", regexp.MustCompile(`\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_\-]{20,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{60,}\b`)},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`)},
	{"Stripe key", regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]{20,}\b`)},
	{"Hugging Face token", regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}\b`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`)},
}

// assignment masks the value in "password=...", "api_key: ...", etc.
var assignment = regexp.MustCompile(`(?i)\b([A-Za-z0-9_\-]*(?:password|passwd|secret|api[_\-]?key|access[_\-]?key|token)[A-Za-z0-9_\-]*)(\s*[:=]\s*["']?)([^\s"',;]{6,})`)

// Redactor masks known secret formats plus specific secret values.
type Redactor struct {
	values []string // exact secrets, longest first
}

// New returns a redactor that also masks the given literal values (ignoring
// ones shorter than 8 characters, which would cause false positives).
func New(values ...string) *Redactor {
	r := &Redactor{}
	seen := map[string]bool{}
	for _, v := range values {
		if len(v) >= 8 && !seen[v] {
			seen[v] = true
			r.values = append(r.values, v)
		}
	}
	sort.Slice(r.values, func(i, j int) bool { return len(r.values[i]) > len(r.values[j]) })
	return r
}

// FromEnv returns a redactor for the values of environment variables whose
// names match any of the glob patterns (case-insensitive), e.g. "*_API_KEY".
func FromEnv(namePatterns []string, extra ...string) *Redactor {
	var values []string
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		for _, p := range namePatterns {
			if ok, _ := path.Match(strings.ToUpper(p), strings.ToUpper(name)); ok {
				values = append(values, value)
				break
			}
		}
	}
	return New(append(values, extra...)...)
}

// remembered are secret values learned while running ({secrets:key}
// values, as they're used): every Redactor masks them.
var remembered struct {
	sync.RWMutex
	values []string // longest first
}

// Remember adds values for every Redactor to mask from now on (ignoring
// ones shorter than 8 characters, as New does).
func Remember(values ...string) {
	remembered.Lock()
	defer remembered.Unlock()
	added := false
	for _, v := range values {
		if len(v) >= 8 && !slices.Contains(remembered.values, v) {
			remembered.values = append(remembered.values, v)
			added = true
		}
	}
	if added {
		sort.Slice(remembered.values, func(i, j int) bool { return len(remembered.values[i]) > len(remembered.values[j]) })
	}
}

// Mask masks only the remembered values in s (not the patterns): for text
// shown as it is, such as a model's answer.
func Mask(s string) string {
	remembered.RLock()
	defer remembered.RUnlock()
	for _, v := range remembered.values {
		s = strings.ReplaceAll(s, v, mask)
	}
	return s
}

// Longest is the length of the longest value r masks, its own or
// remembered (0 for none): text streamed in pieces is held at least this
// long so a value is whole when it's checked. A nil Redactor has the
// remembered ones.
func (r *Redactor) Longest() int {
	n := 0
	if r != nil && len(r.values) > 0 {
		n = len(r.values[0])
	}
	remembered.RLock()
	defer remembered.RUnlock()
	if len(remembered.values) > 0 {
		n = max(n, len(remembered.values[0]))
	}
	return n
}

// String masks secrets in s. A nil Redactor still applies the built-in
// patterns and the remembered values.
func (r *Redactor) String(s string) string {
	if s == "" {
		return s
	}
	if r != nil {
		for _, v := range r.values {
			s = strings.ReplaceAll(s, v, mask)
		}
	}
	s = Mask(s)
	for _, f := range Formats {
		s = f.Pattern.ReplaceAllString(s, mask)
	}
	return assignment.ReplaceAllString(s, "${1}${2}"+mask)
}

// Value masks secrets inside strings nested in maps and slices (e.g. tool
// arguments), returning a copy.
func (r *Redactor) Value(v any) any {
	switch t := v.(type) {
	case string:
		return r.String(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = r.Value(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = r.Value(x)
		}
		return out
	default:
		return v
	}
}
