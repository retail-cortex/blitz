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

package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocaleCommand(t *testing.T) {
	defer i18n.SetCurrent(nil)
	i18n.SetCurrent(nil)
	app := newTestApp(t, nil)
	ctx := context.Background()

	out := captureStdout(t, func() { HandleCommand(ctx, "/locale", app) })
	for _, want := range []string{"Interface language: English (US) (en-US)", "es (Español)", "fr-CA (Français (Canada))", local(app).Config().UI.LocalesDir} {
		assert.Contains(t, out, want, "/locale output lacks %q:\n%s", want, out)
	}

	// The user's example: "/locale ES-sp" means Spanish.
	out = captureStdout(t, func() { HandleCommand(ctx, "/locale ES-sp", app) })
	saved := savedConfig(t).UI.Locale
	require.Equal(t, "es", saved, "saved %q, current %v", saved, i18n.Current().Tag())
	require.Equal(t, "es", i18n.Current().Tag().String(), "saved %q, current %v", saved, i18n.Current().Tag())
	assert.Contains(t, out, "Idioma de la interfaz: Español (es).", "confirmation should already be in Spanish:\n%s", out)
	assert.Contains(t, out, "Guardado en ", "confirmation should already be in Spanish:\n%s", out)
	out = captureStdout(t, func() { HandleCommand(ctx, "/help", app) })
	assert.Contains(t, out, "Comandos de Blitz", "/help not translated:\n%s", out)
	assert.Contains(t, out, "/locale [code]", "/help not translated:\n%s", out)

	// A language without a catalog: model replies change, menus don't.
	out = captureStdout(t, func() { HandleCommand(ctx, "/locale japanese", app) })
	assert.Contains(t, out, "No translation catalog for Japanese", "ja output:\n%s", out)
	assert.Equal(t, "ja", i18n.Current().Tag().String(), "ja output:\n%s", out)

	out = captureStdout(t, func() { HandleCommand(ctx, "/locale en-US", app) })
	assert.Contains(t, out, "Interface language set to English (US) (en-US).", "switching back:\n%s", out)

	// Unknown input changes nothing.
	out = captureStdout(t, func() { HandleCommand(ctx, "/locale zz-top-9", app) })
	saved = savedConfig(t).UI.Locale
	assert.Equal(t, "en-US", saved, "unknown locale was applied (saved %q):\n%s", saved, out)
	assert.Contains(t, out, "Unknown language", "unknown locale was applied (saved %q):\n%s", saved, out)

	// A save failure is reported but the session keeps the new language.
	notDir := filepath.Join(t.TempDir(), "file")
	os.WriteFile(notDir, nil, 0o600)
	t.Setenv("MODENV_PREFIX", notDir) // the config "directory" is a file
	out = captureStdout(t, func() { HandleCommand(ctx, "/locale fr", app) })
	assert.Equal(t, "fr", i18n.Current().Tag().String(), "save failure:\n%s", out)
	assert.Contains(t, out, "l’enregistrement a échoué", "save failure:\n%s", out)
}
