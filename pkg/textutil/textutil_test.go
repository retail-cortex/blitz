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

package textutil

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncateUTF8(t *testing.T) {
	// Positive: ASCII shorter than limit is untouched; longer is cut exactly.
	got := TruncateUTF8("hello", 10)
	assert.Equal(t, "hello", got, "expected unchanged, got %q", got)
	got = TruncateUTF8("hello", 3)
	assert.Equal(t, "hel", got, "expected 'hel', got %q", got)

	// Negative: cutting inside a multi-byte rune must back up to a rune boundary.
	s := "ab🐶cd" // 🐶 is 4 bytes at offset 2
	for n := 3; n <= 5; n++ {
		got := TruncateUTF8(s, n)
		assert.True(t, utf8.ValidString(got), "n=%d produced invalid UTF-8 %q", n, got)
		assert.Equal(t, "ab", got, "n=%d expected 'ab', got %q", n, got)
	}
	got = TruncateUTF8(s, 0)
	assert.Equal(t, "", got, "expected empty for n=0, got %q", got)
}

func TestEllipsize(t *testing.T) {
	assert.Equal(t, "short", Ellipsize("short", 10), "expected unchanged, got")
	got := Ellipsize("🐶🐶🐶🐶🐶", 10)
	assert.True(t, utf8.ValidString(got), "bad ellipsized value %q", got)
	assert.LessOrEqual(t, len(got), 10, "bad ellipsized value %q", got)
	assert.Equal(t, "...", got[len(got)-3:], "bad ellipsized value %q", got)
}

func TestSanitizeTerminal(t *testing.T) {
	// Positive: ordinary text, newlines, tabs and emoji survive untouched.
	plain := "line1\n\tline2 🐶 ✅"
	got := SanitizeTerminal(plain)
	assert.Equal(t, plain, got, "expected plain text unchanged, got %q", got)

	// Negative: escape sequences and control characters are stripped.
	cases := map[string]string{
		"\x1b]52;c;ZXZpbA==\x07ok": "]52;c;ZXZpbA==ok", // OSC 52 clipboard write
		"\x1b[2J\x1b[Hcleared":     "[2J[Hcleared",
		"fake\rreal":               "fakereal",
		"bell\x07":                 "bell",
		"c1\u009bcontrol":          "c1control",
		"del\x7f":                  "del",
		"bad\xffbyte":              "bad�byte",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got := SanitizeTerminal(in)
			assert.Equal(t, want, got, "SanitizeTerminal(%q) = %q, want %q", in, got, want)
		})
	}
}

func TestTrimPartialRune(t *testing.T) {
	// Positive: complete strings are untouched.
	for _, s := range []string{"", "abc", "ab🐶", "é"} {
		t.Run(s, func(t *testing.T) {
			got := TrimPartialRune(s)
			assert.Equal(t, s, got, "TrimPartialRune(%q) = %q, want unchanged", s, got)
		})
	}
	// Negative: a rune cut mid-sequence is removed.
	dog := "ab🐶"
	for cut := 3; cut < len(dog); cut++ {
		got := TrimPartialRune(dog[:cut])
		assert.Equal(t, "ab", got, "TrimPartialRune(%q) = %q, want 'ab'", dog[:cut], got)
	}
}
