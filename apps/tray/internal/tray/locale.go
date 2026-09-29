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
	"encoding/json"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"golang.org/x/text/language"
)

// SetupLocale shows the tray in the desktop app's language (Settings ›
// Language), from the same catalogs, with ui.locales_dir's added.
func SetupLocale() {
	var dirs []string
	if cfg, err := config.Load(""); err == nil && cfg.UI.LocalesDir != "" {
		dirs = append(dirs, config.ExpandHome(cfg.UI.LocalesDir))
	}
	b, _ := i18n.NewBundle(dirs...)
	prefs, _ := os.ReadFile(config.ExpandHome("~/.blitz/desktop.json"))
	i18n.SetCurrent(b.Localizer(Language(b, prefs, SystemLanguages())))
}

// Language is the language the tray shows: the desktop app's setting
// (prefs, the content of ~/.blitz/desktop.json) unless it's "system"; else
// the first of the system's languages (most preferred first) with a
// catalog; else English. It chooses as the app's window does.
func Language(b *i18n.Bundle, prefs []byte, system []string) language.Tag {
	var p struct {
		Language string `json:"language"`
	}
	wanted := system
	if json.Unmarshal(prefs, &p) == nil && p.Language != "" && p.Language != "system" {
		wanted = []string{p.Language}
	}
	for _, w := range wanted {
		tag, err := b.Resolve(w)
		if err != nil {
			continue
		}
		if l := b.Localizer(tag); l.HasCatalog() {
			return tag
		}
	}
	tag, _ := b.Resolve(i18n.DefaultLocale)
	return tag
}

// SystemLanguages are the user's languages, most preferred first: macOS's
// list (programs started at login have no LANG there), then LANGUAGE,
// LC_ALL, LC_MESSAGES and LANG.
func SystemLanguages() []string {
	var out []string
	if goruntime.GOOS == "darwin" {
		if b, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
			out = append(out, parseAppleLanguages(string(b))...)
		}
	}
	return append(out, envLanguages(os.Getenv)...)
}

// envLanguages reads the POSIX locale variables: LANGUAGE (a list), then
// the first of LC_ALL, LC_MESSAGES and LANG, without encoding or modifier
// (fr_CA.UTF-8@euro is fr_CA). C and POSIX aren't languages.
func envLanguages(getenv func(string) string) []string {
	var out []string
	add := func(v string) {
		v, _, _ = strings.Cut(v, ".")
		v, _, _ = strings.Cut(v, "@")
		if v != "" && v != "C" && v != "POSIX" {
			out = append(out, v)
		}
	}
	for _, v := range strings.Split(getenv("LANGUAGE"), ":") {
		add(v)
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(name); v != "" {
			add(v)
			break
		}
	}
	return out
}

// parseAppleLanguages reads `defaults read -g AppleLanguages`:
//
//	(
//	    "en-CA",
//	    fr
//	)
func parseAppleLanguages(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		v := strings.Trim(strings.TrimSpace(line), `",`)
		if v != "" && v != "(" && v != ")" {
			out = append(out, v)
		}
	}
	return out
}
