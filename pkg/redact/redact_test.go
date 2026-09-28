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

package redact

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactPatterns(t *testing.T) {
	r := New()
	secrets := []string{
		"sk-proj-abcdefghijklmnopqrstuvwxyz123456",
		"sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"AKIAIOSFODNN7EXAMPLE",
		"AIzaSyA1234567890abcdefghijklmnopqrstuv",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"xoxb-1234567890-abcdefghij",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----",
	}
	for _, s := range secrets {
		t.Run(s, func(t *testing.T) {
			out := r.String("value: " + s + " end")
			assert.NotContains(t, out, s, "secret not masked: %q -> %q", s, out)
			assert.Contains(t, out, mask, "secret not masked: %q -> %q", s, out)
		})
	}
	// Assignment style keeps the key, masks the value.
	got := r.String(`export DB_PASSWORD="hunter2secret"`)
	assert.Equal(t, `export DB_PASSWORD="`+mask+`"`, got, "assignment masking = %q", got)
	// Negative: ordinary text is untouched.
	for _, s := range []string{"go test ./...", "the token count is 12", "password=", "sk-short", ""} {
		t.Run(s, func(t *testing.T) {
			got := r.String(s)
			assert.Equal(t, s, got, "false positive: %q -> %q", s, got)
		})
	}
}

func TestRedactValuesAndEnv(t *testing.T) {
	t.Setenv("MY_SERVICE_API_KEY", "custom-secret-value-123")
	t.Setenv("SHORT_API_KEY", "abc") // too short to redact safely
	r := FromEnv([]string{"*_API_KEY"}, "another-literal-secret")

	in := "a custom-secret-value-123 b another-literal-secret c abc"
	out := r.String(in)
	assert.NotContains(t, out, "custom-secret-value-123", "literal secrets leaked: %q", out)
	assert.NotContains(t, out, "another-literal-secret", "literal secrets leaked: %q", out)
	assert.True(t, strings.HasSuffix(out, " abc"), "short value should not be redacted: %q", out)

	nested := r.Value(map[string]any{"cmd": "curl -H custom-secret-value-123", "n": 3, "list": []any{"x", "another-literal-secret"}}).(map[string]any)
	assert.NotContains(t, nested["cmd"].(string), "custom-secret", "nested redaction failed: %v", nested)
	assert.Equal(t, 3, nested["n"], "nested redaction failed: %v", nested)
	assert.Equal(t, mask, nested["list"].([]any)[1], "nested redaction failed: %v", nested)
	var nilR *Redactor
	got := nilR.String("key sk-proj-abcdefghijklmnopqrstuvwxyz123456")
	assert.NotContains(t, got, "sk-proj", "nil redactor should still apply built-in patterns")
}
