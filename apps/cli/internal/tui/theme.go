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
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// themes are the Markdown styles /theme offers (glamour's standard ones).
var themes = []string{"auto", "dark", "light", "dracula", "tokyo-night", "pink", "ascii", "notty"}

// plainDiffs: the theme has no colours, so diffs have none either.
var plainDiffs atomic.Bool

// applyTheme makes name the theme of the REPL's Markdown and diffs.
func applyTheme(app *App, name string) {
	app.Printer.Theme = name
	plainDiffs.Store(name == "ascii" || name == "notty")
}

// cmdTheme shows the themes or switches to one, keeping it with --save
// (spec_parity_027 PAR-UI-09).
func cmdTheme(args []string, app *App) {
	save := slices.Contains(args, "--save")
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--save" })
	current := app.Printer.Theme
	if current == "" {
		current = "auto"
	}
	if len(args) == 0 {
		fmt.Printf("\n%s%s%s\n", Bold, i18n.T("theme.title"), Reset)
		for _, t := range themes {
			marker := "  "
			if t == current {
				marker = "› "
			}
			fmt.Printf("%s%s\n", marker, t)
		}
		fmt.Printf("\n  %s%s%s\n\n", Dim, i18n.T("theme.hint"), Reset)
		return
	}
	name := strings.ToLower(args[0])
	if !slices.Contains(themes, name) {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("theme.unknown", "name", safe(name), "themes", strings.Join(themes, ", ")), Reset)
		return
	}
	applyTheme(app, name)
	fmt.Printf("%s✓ %s%s\n", Green, i18n.T("theme.set", "name", name), Reset)
	if save {
		path, err := config.SetValue(app.ConfigDir, "", "ui.theme", name)
		if err != nil {
			fmt.Printf("%s✗ %v%s\n", Red, err, Reset)
			return
		}
		fmt.Printf("%s%s%s\n", Dim, i18n.T("config.saved", "key", "ui.theme", "path", safe(path)), Reset)
	}
}
