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
	"strings"
	"testing"
)

// /license shows the NOTICE, the full license, or the third-party
// notices; anything else is refused.
func TestLicenseCommand(t *testing.T) {
	app := newFullApp(t)
	ctx := context.Background()
	for arg, want := range map[string]string{
		"":             "Copyright 2026 Retail Cortex",
		" full":        "TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION",
		" third-party": "THIRD-PARTY NOTICES",
		" bogus":       "unknown license text",
	} {
		out := captureStdout(t, func() {
			if handled, err := HandleCommand(ctx, "/license"+arg, app); !handled || err != nil {
				t.Errorf("/license%s: handled %v, %v", arg, handled, err)
			}
		})
		if !strings.Contains(out, want) {
			t.Errorf("/license%s: no %q in\n%.300s", arg, want, out)
		}
	}
}
