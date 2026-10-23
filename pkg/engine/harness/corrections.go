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

package harness

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// How a prompt that rejects the last answer starts, in each interface
// language (en-US, es, fr-CA), and phrases that say so anywhere in it.
// A short list, not a model call (HE-02): it misses some and catches a
// few that aren't, which the reflector weighs.
var (
	correctionStarts = []string{
		// English
		"no,", "no.", "no!", "nope", "wrong", "that's wrong", "that's not", "that isn't", "not what i",
		"you didn't", "you did not", "you broke", "you forgot", "you missed", "undo", "revert",
		"still not", "still broken", "still failing", "still fails", "it still", "that broke",
		"try again", "this is wrong", "this isn't", "this is not",
		// Spanish
		"no es", "eso no", "está mal", "esta mal", "eso está mal", "incorrecto", "te equivocaste",
		"no funciona", "no funcionó", "sigue fallando", "sigue sin", "deshaz", "revierte",
		"otra vez", "olvidaste", "rompiste",
		// French
		"non,", "non.", "non!", "c'est faux", "ce n'est pas", "c'est pas", "pas ça", "pas ca",
		"toujours pas", "ça ne marche pas", "ca ne marche pas", "ça marche pas", "annule",
		"recommence", "tu as oublié", "tu as cassé", "faux",
	}
	correctionPhrases = []string{
		"that's wrong", "that is wrong", "not what i asked", "not what i meant", "doesn't work",
		"didn't work", "does not work", "did not work", "still fails", "still failing", "still broken",
		"you broke", "i said",
		"no es lo que", "no funciona", "no funcionó", "sigue fallando", "te dije",
		"ce n'est pas ce que", "ne fonctionne pas", "ne marche pas", "toujours pas", "je t'ai dit",
	}
)

// IsCorrection reports whether a prompt rejects the last answer.
func IsCorrection(prompt string) bool {
	p := strings.ToLower(strings.TrimSpace(prompt))
	p = strings.NewReplacer("’", "'", " ", " ").Replace(p)
	p = strings.TrimLeftFunc(p, func(r rune) bool { return unicode.IsPunct(r) && r != '/' })
	if p == "" || strings.HasPrefix(p, "/") { // a command
		return false
	}
	if p == "no" || p == "non" {
		return true
	}
	for _, s := range correctionStarts {
		if hasWordPrefix(p, s) {
			return true
		}
	}
	for _, s := range correctionPhrases {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}

// hasWordPrefix reports whether p starts with s as whole words: "no
// worries" isn't "no", "nothing" isn't "no".
func hasWordPrefix(p, s string) bool {
	if !strings.HasPrefix(p, s) {
		return false
	}
	rest := p[len(s):]
	if rest == "" {
		return true
	}
	if last, _ := utf8.DecodeLastRuneInString(s); unicode.IsPunct(last) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
