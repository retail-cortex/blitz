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

package tray

import (
	"testing"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLanguage(t *testing.T) {
	b, errs := i18n.NewBundle()
	require.Empty(t, errs)
	cases := []struct {
		name   string
		prefs  string
		system []string
		want   string
	}{
		{"the app's setting", `{"language":"es"}`, []string{"fr-CA"}, "es"},
		{"system: the system's first language with a catalog", `{"language":"system"}`, []string{"de-DE", "fr-CA", "es"}, "fr-CA"},
		{"no app settings yet", "", []string{"es-MX"}, "es-MX"},
		{"damaged settings", "{", []string{"es"}, "es"},
		{"nothing known: English", `{"language":"system"}`, []string{"de", "ja"}, "en-US"},
		{"no system languages", "", nil, "en-US"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Language(b, []byte(c.prefs), c.system).String())
		})
	}
}

func TestMenuInTheChosenLanguage(t *testing.T) {
	b, _ := i18n.NewBundle()
	old := i18n.Current()
	t.Cleanup(func() { i18n.SetCurrent(old) })
	i18n.SetCurrent(b.Localizer(Language(b, []byte(`{"language":"fr-CA"}`), nil)))
	m := MenuFor(Status{Installed: true})
	assert.Equal(t, "Service Blitz : arrêté", m.State.Title)
	assert.Equal(t, "Démarrer le service", m.Start.Title)
}

func TestEnvLanguages(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"LANG with an encoding", map[string]string{"LANG": "fr_CA.UTF-8"}, []string{"fr_CA"}},
		{"LC_ALL before LANG", map[string]string{"LC_ALL": "es_ES.UTF-8@euro", "LANG": "en_US.UTF-8"}, []string{"es_ES"}},
		{"LC_MESSAGES before LANG", map[string]string{"LC_MESSAGES": "es_MX", "LANG": "en_US"}, []string{"es_MX"}},
		{"LANGUAGE's list first", map[string]string{"LANGUAGE": "fr_CA:es", "LANG": "en_US.UTF-8"}, []string{"fr_CA", "es", "en_US"}},
		{"C isn't a language", map[string]string{"LANG": "C.UTF-8"}, nil},
		{"nothing set", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, envLanguages(func(k string) string { return c.env[k] }))
		})
	}
}

func TestParseAppleLanguages(t *testing.T) {
	assert.Equal(t, []string{"en-CA", "fr", "es-419"}, parseAppleLanguages("(\n    \"en-CA\",\n    fr,\n    \"es-419\"\n)\n"))
	assert.Empty(t, parseAppleLanguages(""))
}
