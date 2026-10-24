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

// Package secretscan finds API keys and other secrets in files about to
// be committed, for the pre-commit hook a harness gets
// (spec_secret_hooks_051): known key formats anywhere, and long random
// values assigned to key-like names in .env and config files. A line
// with "blitz:allow-secret" is skipped (a false alarm, a test fixture).
package secretscan

import (
	"bytes"
	"math"
	"path"
	"regexp"
	"strings"
)

// AllowMarker on a line skips it.
const AllowMarker = "blitz:allow-secret"

// maxFileBytes is the largest file scanned; larger ones are data, not
// configuration.
const maxFileBytes = 1 << 20

// A Finding is a secret on a line of a file.
type Finding struct {
	Path string
	Line int
	// Kind names what it looks like ("AWS access key").
	Kind string
	// Masked is the value with all but its first four characters hidden.
	Masked string
}

// known are key formats that are secrets wherever they appear.
var known = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
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
}

// assignment is NAME=value with a key-like name, in env and config files.
var assignment = regexp.MustCompile(`(?i)\b[A-Za-z0-9_.\-]*(?:password|passwd|secret|api[_\-]?key|access[_\-]?key|token|private[_\-]?key|credential)[A-Za-z0-9_.\-]*["']?\s*[:=]\s*["']?([^\s"',;#]{16,})`)

// placeholders are values that stand in for a secret, not one.
var placeholders = regexp.MustCompile(`(?i)^(?:\$\{|\$\(|<|%|\{\{|your[_\-]|xxx|changeme|example|placeholder|dummy|test|fake|redacted|keychain:|env:|op://)`)

// configFile reports whether name holds settings, where a key-like name
// with a long random value is a secret.
func configFile(name string) bool {
	base := strings.ToLower(path.Base(name))
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return !strings.HasSuffix(base, ".example") && !strings.HasSuffix(base, ".sample") && !strings.HasSuffix(base, ".template")
	}
	switch path.Ext(base) {
	case ".toml", ".yaml", ".yml", ".ini", ".cfg", ".conf", ".properties", ".json", ".tfvars":
		return true
	}
	return base == ".npmrc" || base == ".pypirc" || base == "credentials"
}

// entropy is the Shannon entropy of s in bits a character.
func entropy(s string) float64 {
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	var h float64
	n := float64(len([]rune(s)))
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// mixed reports whether v has letters and digits, as generated keys do
// and words (production-database-password) don't.
func mixed(v string) bool {
	return strings.ContainsAny(v, "0123456789") && strings.IndexFunc(v, func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }) >= 0
}

// mask keeps the first four characters of v.
func mask(v string) string {
	if len(v) <= 4 {
		return strings.Repeat("•", len(v))
	}
	return v[:4] + strings.Repeat("•", min(len(v)-4, 12))
}

// Scan finds the secrets in a file's content (binary and very large
// files are skipped).
func Scan(name string, data []byte) []Finding {
	if len(data) > maxFileBytes || bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	config := configFile(name)
	var out []Finding
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, AllowMarker) {
			continue
		}
		found := false
		for _, k := range known {
			if m := k.re.FindString(line); m != "" {
				out = append(out, Finding{Path: name, Line: i + 1, Kind: k.kind, Masked: mask(m)})
				found = true
				break
			}
		}
		if found || !config {
			continue
		}
		if m := assignment.FindStringSubmatch(line); m != nil {
			v := m[1]
			if !placeholders.MatchString(v) && entropy(v) >= 3.5 && mixed(v) {
				out = append(out, Finding{Path: name, Line: i + 1, Kind: "secret value", Masked: mask(v)})
			}
		}
	}
	return out
}
