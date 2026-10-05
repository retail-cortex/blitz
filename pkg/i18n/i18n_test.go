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

package i18n

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
)

func mustBundle(t *testing.T, dirs ...string) *Bundle {
	t.Helper()
	b, errs := NewBundle(dirs...)
	require.LessOrEqual(t, len(errs), 0, "NewBundle: %v", errs)
	return b
}

func TestEmbeddedCatalogs(t *testing.T) {
	b := mustBundle(t)
	var got []string
	for _, m := range b.Available() {
		t.Run(m.Name, func(t *testing.T) {
			got = append(got, m.Locale)
			assert.NotEqual(t, "", m.Name, "%s: missing names: %+v", m.Locale, m)
			assert.NotEqual(t, "", m.EnglishName, "%s: missing names: %+v", m.Locale, m)
		})
	}
	require.Equal(t, "en-US,es,fr-CA", strings.Join(got, ","), "available = %v", got)
}

// Shipped translations must be complete and keep every placeholder, or a
// value (a file name, an error) silently disappears from the message.
func TestShippedTranslationsComplete(t *testing.T) {
	b := mustBundle(t)
	for _, tag := range []string{"es", "fr-CA"} {
		t.Run(tag, func(t *testing.T) {
			m := b.Missing(tag)
			assert.LessOrEqual(t, len(m), 0, "%s is missing %d keys: %v", tag, len(m), m)
			p := b.Problems(tag)
			assert.LessOrEqual(t, len(p), 0, "%s problems:\n  %s", tag, strings.Join(p, "\n  "))
		})
	}
}

// Approval and exit answers are single letters the code matches; every
// translation must keep offering the same letters.
func TestTranslationsKeepAnswerLetters(t *testing.T) {
	b := mustBundle(t)
	keys := map[string][]string{
		"approve.options":     {"[y]", "[s]", "[a]"},
		"approve.yes":         {"[y]"},
		"approve.no":          {"[n]"},
		"approve.show_diff":   {"[d]"},
		"exit.choices":        {"[k", "[w"},
		"exit.choices_cancel": {"[c"},
		"approvals.none":      {"[s]", "[a]"},
		"project.ask":         {"[t]", "[d]", "[s]"},
	}
	for _, m := range b.Available() {
		t.Run(m.Name, func(t *testing.T) {
			c, _ := b.Catalog(m.Locale)
			for key, letters := range keys {
				for _, l := range letters {
					assert.Contains(t, c.Messages[key], l, "%s %s = %q lacks %s", m.Locale, key, c.Messages[key], l)
				}
			}
		})
	}
}

func TestResolve(t *testing.T) {
	b := mustBundle(t)
	cases := map[string]string{
		"es": "es", "ES": "es", "es-ES": "es-ES", "es_ES": "es-ES", "ES-sp": "es",
		"spanish": "es", "Español": "es", "español": "es",
		"fr-CA": "fr-CA", "fr_ca": "fr-CA", "French (Canada)": "fr-CA",
		"en-US": "en-US", "de": "de", "German": "de", "japanese": "ja", "日本語": "ja", "ja-JP": "ja-JP", "en-XA": "en-XA",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := b.Resolve(in)
			assert.NoError(t, err, "Resolve(%q) = %v, %v; want %s", in, got, err, want)
			assert.Equal(t, want, got.String(), "Resolve(%q) = %v, %v; want %s", in, got, err, want)
		})
	}
	for _, bad := range []string{"", "   ", "klingonese", "123", "x"} {
		t.Run(bad, func(t *testing.T) {
			_, err := b.Resolve(bad)
			assert.Error(t, err, "Resolve(%q) should fail", bad)
		})
	}
}

func TestFallbackChain(t *testing.T) {
	b := mustBundle(t)
	loc := func(s string) *Localizer { return b.Localizer(language.MustParse(s)) }

	got := loc("es-MX").T("recap.you")
	assert.Equal(t, "tú", got, "es-MX should use the es catalog, got %q", got)
	got = loc("fr").T("exit.cancelled")
	assert.Equal(t, "Fermeture annulée.", got, "fr should borrow fr-CA, got %q", got)
	de := loc("de")
	assert.False(t, de.HasCatalog(), "de should fall back to English: %v %q", de.HasCatalog(), de.T("exit.cancelled"))
	assert.Equal(t, "Exit cancelled.", de.T("exit.cancelled"), "de should fall back to English: %v %q", de.HasCatalog(), de.T("exit.cancelled"))
	assert.True(t, loc("en-GB").HasCatalog(), "English variants and es have catalogs")
	assert.True(t, loc("es").HasCatalog(), "English variants and es have catalogs")
	got = loc("es").T("no.such.key")
	assert.Equal(t, "no.such.key", got, "missing keys render as the key, got %q", got)
}

func TestPlaceholdersAndPlurals(t *testing.T) {
	b := mustBundle(t)
	en := b.Localizer(language.MustParse("en-US"))
	got := en.T("agent.current", "name", "Helios", "id", "helios")
	assert.Equal(t, "Current agent: Helios (helios)", got, "got %q", got)
	got = en.T("agent.current", "name", "X")
	assert.Equal(t, "Current agent: X ({id})", got, "an unfilled placeholder should stay visible, got %q", got)
	for n, want := range map[int]string{0: "0 messages", 1: "1 message", 2: "2 messages"} {
		t.Run(want, func(t *testing.T) {
			got := en.N("session.messages", n)
			assert.Equal(t, want, got, "en N(%d) = %q", n, got)
		})
	}
	// French treats 0 as singular.
	fr := b.Localizer(language.MustParse("fr-CA"))
	for n, want := range map[int]string{0: "0 règle révoquée.", 1: "1 règle révoquée.", 3: "3 règles révoquées."} {
		t.Run(want, func(t *testing.T) {
			got := fr.N("approvals.revoked", n)
			assert.Equal(t, want, got, "fr N(%d) = %q, want %q", n, got, want)
		})
	}
	// Japanese has no singular: always "other".
	got = pluralCategory(language.Japanese, 1)
	assert.Equal(t, "other", got, "ja plural = %s", got)
}

func TestPseudoLocale(t *testing.T) {
	b := mustBundle(t)
	l := b.Localizer(language.MustParse(PseudoLocale))
	got := l.T("agent.current", "name", "helios", "id", "x")
	assert.True(t, strings.HasPrefix(got, "⟦"), "pseudo = %q", got)
	assert.Contains(t, got, "helios", "pseudo = %q", got)
	assert.NotContains(t, got, "Current", "pseudo = %q", got)
	assert.Equal(t, "", ReplyInstruction(l), "pseudo-locale should not change the model's language")
}

func TestExternalCatalogs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	write("de.json", `{"meta":{"locale":"de"},"messages":{"exit.cancelled":"Beenden abgebrochen.","session.messages.one":"{count} Nachricht","session.messages.other":"{count} Nachrichten"}}`)
	write("es-fix.json", `{"meta":{"locale":"es"},"messages":{"recap.blitz":"perrito"}}`)
	write("broken.json", `{not json`)
	write("nolocale.json", `{"meta":{"locale":"???"},"messages":{"a":"b"}}`)
	write("notes.txt", "ignored")

	b, errs := NewBundle(dir, filepath.Join(dir, "missing"))
	require.Len(t, errs, 2, "want 2 errors (broken, nolocale), got %v", errs)
	de := b.Localizer(language.German)
	assert.True(t, de.HasCatalog(), "de catalog not used")
	assert.Equal(t, "Beenden abgebrochen.", de.T("exit.cancelled"), "de catalog not used")
	assert.Equal(t, "2 Nachrichten", de.N("session.messages", 2), "de catalog not used")
	assert.Equal(t, "Force quit.", de.T("exit.force_quit"), "untranslated keys should fall back to English")
	c, _ := b.Catalog("de")
	assert.Equal(t, "German", c.Meta.EnglishName, "names should default from CLDR: %+v", c.Meta)
	assert.Equal(t, "Deutsch", c.Meta.Name, "names should default from CLDR: %+v", c.Meta)
	es := b.Localizer(language.Spanish)
	assert.Equal(t, "perrito", es.T("recap.blitz"), "an external file should override single keys and keep the rest")
	assert.Equal(t, "tú", es.T("recap.you"), "an external file should override single keys and keep the rest")
	tag, err := b.Resolve("german")
	assert.NoError(t, err, "Resolve(german) = %v,", tag)
	assert.Equal(t, language.German, tag, "Resolve(german) = %v, %v", tag, err)
}

func TestProblemsDetectsBadTranslations(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "it.json"), []byte(`{"meta":{"locale":"it"},"messages":{
		"agent.current":"Agente: {nome}",
		"made.up":"x",
		"session.messages.one":"{count} messaggio"}}`), 0o600)
	b := mustBundle(t, dir)
	p := strings.Join(b.Problems("it"), "\n")
	assert.Contains(t, p, "agent.current: placeholders", "problems = %s", p)
	assert.Contains(t, p, "unknown key made.up", "problems = %s", p)
	assert.NotContains(t, p, "session.messages.one", "valid plural form flagged: %s", p)
}

func TestReplyInstruction(t *testing.T) {
	b := mustBundle(t)
	assert.Equal(t, "", ReplyInstruction(b.Localizer(language.MustParse("en-US"))), "English needs no reply instruction")
	assert.Equal(t, "", ReplyInstruction(nil), "English needs no reply instruction")
	got := ReplyInstruction(b.Localizer(language.MustParse("es")))
	assert.Contains(t, got, "Spanish")
	assert.Contains(t, got, "file paths")
	// Languages without a catalog still get replies in that language.
	got = ReplyInstruction(b.Localizer(language.MustParse("ja")))
	assert.Contains(t, got, "Japanese", "got %q", got)
}

func TestCurrentDefaultsToEnglish(t *testing.T) {
	defer SetCurrent(nil)
	SetCurrent(nil)
	assert.Equal(t, DefaultLocale, Current().Tag().String(), "default should be en-US")
	assert.Equal(t, "Exit cancelled.", T("exit.cancelled"), "default should be en-US")
	SetCurrent(Default().Localizer(language.Spanish))
	assert.Equal(t, "Salida cancelada.", T("exit.cancelled"), "SetCurrent not applied")
	assert.Equal(t, "1 mensaje", N("session.messages", 1), "SetCurrent not applied")
}

// CheckCatalog accepts what a Bundle loads and says why it refuses the rest.
func TestCheckCatalog(t *testing.T) {
	cases := map[string]struct {
		data string
		want string // "" means accepted
	}{
		"valid":       {data: `{"meta":{"locale":"de"},"messages":{"a":"b"}}`},
		"not JSON":    {data: `{`, want: "unexpected end"},
		"bad locale":  {data: `{"meta":{"locale":"???"},"messages":{"a":"b"}}`, want: "invalid meta.locale"},
		"no messages": {data: `{"meta":{"locale":"de"},"messages":{}}`, want: "no messages"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := CheckCatalog([]byte(tc.data))
			if tc.want == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.want)
			}
		})
	}
}

// An external catalog without messages is refused; one for a shipped
// language may rename it.
func TestExternalCatalogNames(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.json"), []byte(`{"meta":{"locale":"de"},"messages":{}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "es.json"), []byte(`{"meta":{"locale":"es","name":"Castellano","english_name":"Castilian"},"messages":{"recap.blitz":"perrito"}}`), 0o600))
	b, errs := NewBundle(dir)
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "no messages")
	c, ok := b.Catalog("es")
	require.True(t, ok)
	assert.Equal(t, "Castellano", c.Meta.Name)
	assert.Equal(t, "Castilian", c.Meta.EnglishName)
	assert.Equal(t, "Castellano", b.Localizer(language.Spanish).NativeName())
}

// NativeName prefers the catalog's own name, then CLDR's, then the English
// name.
func TestNativeName(t *testing.T) {
	b := mustBundle(t)
	assert.Equal(t, "日本語", b.Localizer(language.Japanese).NativeName(), "CLDR's name without a catalog")
	l := b.Localizer(language.MustParse("en-XA"))
	assert.Equal(t, l.LanguageName(), l.NativeName(), "no native name: the English one")
}

// A bad tag has no catalog; Problems and Missing report a missing catalog
// and the keys a partial one lacks.
func TestCatalogQueries(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "it.json"), []byte(`{"meta":{"locale":"it"},"messages":{"exit.cancelled":"Uscita annullata.","question.one":"Un'altra risposta…"}}`), 0o600))
	b := mustBundle(t, dir)
	_, ok := b.Catalog("not a tag!")
	assert.False(t, ok)
	assert.Equal(t, []string{"no catalog for xx"}, b.Problems("xx"))
	assert.Nil(t, b.Missing("xx"))
	assert.Empty(t, b.Problems("it"), "a plural form English lacks is compared with its .other")
	missing := b.Missing("it")
	assert.Contains(t, missing, "exit.force_quit")
	assert.NotContains(t, missing, "exit.cancelled")
}

// Plural forms follow the language's rules, and a key with only .other
// uses it for every count.
func TestPluralCategory(t *testing.T) {
	cases := []struct {
		tag  string
		n    int
		want string
	}{
		{"ja", 1, "other"},
		{"fr", 0, "one"},
		{"fr", 2, "other"},
		{"pt-BR", 0, "one"},
		{"pt-PT", 0, "other"},
		{"pt-PT", 1, "one"},
		{"pt", 2, "other"},
		{"en", 1, "one"},
		{"en", 0, "other"},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			assert.Equal(t, tc.want, pluralCategory(language.MustParse(tc.tag), tc.n), "n=%d", tc.n)
		})
	}
	l := mustBundle(t).Localizer(language.English)
	assert.Equal(t, "Another answer…", l.N("question", 1), "no .one: .other")
}

// Setup makes a locale current, and says when one isn't a language code.
func TestSetup(t *testing.T) {
	t.Cleanup(func() { Setup("", DefaultLocale, func(string) {}) })
	var warned []string
	warn := func(m string) { warned = append(warned, m) }
	b := Setup(t.TempDir(), "", warn)
	require.NotNil(t, b)
	assert.Empty(t, warned, "the default")
	Setup(t.TempDir(), "not a language!", warn)
	require.Len(t, warned, 1)
	assert.Contains(t, warned[0], "not a language code")
}
