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
	"regexp"
	"strings"
)

// Personal data a guardrail can mask in answers (spec_gates_041): email
// addresses, phone numbers, payment card numbers that pass the Luhn
// check, and US social security numbers. Each is replaced by a label
// saying what it was.
var (
	emailRE = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	// A card: 13 to 19 digits, in groups split by spaces or hyphens.
	cardRE = regexp.MustCompile(`\b(?:\d[ \-]?){12,18}\d\b`)
	ssnRE  = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	// A phone number: a + and country code, or a (area) code, then groups;
	// at least 10 digits in all (checked after matching).
	phoneRE = regexp.MustCompile(`(?:\+\d{1,3}[ .\-]?)?(?:\(\d{2,4}\)[ .\-]?|\b\d{2,4}[ .\-])\d{3,4}[ .\-]\d{3,4}\b`)
)

// PII masks the personal data in s and reports whether it found any.
func PII(s string) (string, bool) {
	found := false
	mark := func(label string) func(string) string {
		return func(string) string {
			found = true
			return "[" + label + " REDACTED]"
		}
	}
	s = emailRE.ReplaceAllStringFunc(s, mark("EMAIL"))
	s = ssnRE.ReplaceAllStringFunc(s, mark("SSN"))
	s = cardRE.ReplaceAllStringFunc(s, func(m string) string {
		if !luhn(digits(m)) {
			return m
		}
		return mark("CARD")(m)
	})
	s = replacePhones(s, mark("PHONE"))
	return s, found
}

// replacePhones replaces the phone numbers in s that stand alone: at least
// 10 digits, and no more digit groups right before or after (a longer
// number isn't a phone number).
func replacePhones(s string, with func(string) string) string {
	var b strings.Builder
	last := 0
	for _, m := range phoneRE.FindAllStringIndex(s, -1) {
		start, end := m[0], m[1]
		if len(digits(s[start:end])) < 10 || touchesDigits(s, start, end) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(with(s[start:end]))
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// touchesDigits reports whether s[start:end] has a digit next to it, or
// one past a single separator.
func touchesDigits(s string, start, end int) bool {
	isDigit := func(i int) bool { return i >= 0 && i < len(s) && s[i] >= '0' && s[i] <= '9' }
	isSep := func(i int) bool { return i >= 0 && i < len(s) && strings.ContainsRune(" .-", rune(s[i])) }
	return isDigit(start-1) || (isSep(start-1) && isDigit(start-2)) || isDigit(end) || (isSep(end) && isDigit(end+1))
}

// digits is s without anything but its digits.
func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// luhn reports whether the digits pass the Luhn check payment cards use.
func luhn(d string) bool {
	if len(d) < 13 || len(d) > 19 {
		return false
	}
	sum, double := 0, false
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if double {
			if n *= 2; n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	return sum%10 == 0
}
