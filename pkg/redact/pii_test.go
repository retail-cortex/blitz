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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPII(t *testing.T) {
	tests := []struct {
		name, in, want string
		found          bool
	}{
		{"an email", "Write to ada@example.com today.", "Write to [EMAIL REDACTED] today.", true},
		{"a card that passes Luhn", "Card 4111 1111 1111 1111 on file", "Card [CARD REDACTED] on file", true},
		{"a number that fails Luhn", "Order 4111 1111 1111 1112", "Order 4111 1111 1111 1112", false},
		{"an SSN", "SSN 123-45-6789.", "SSN [SSN REDACTED].", true},
		{"a phone with a country code", "Call +1 415 555 0123 now", "Call [PHONE REDACTED] now", true},
		{"a phone with an area code", "Call (415) 555-0123", "Call [PHONE REDACTED]", true},
		{"a version, not a phone", "Go 1.27.2 and 2026-10-17", "Go 1.27.2 and 2026-10-17", false},
		{"nothing personal", "The build passed.", "The build passed.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := PII(tt.in)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.found, found)
		})
	}
}
