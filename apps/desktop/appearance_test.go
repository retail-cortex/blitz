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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetAppearance(t *testing.T) {
	tests := []struct {
		name  string
		theme string
		want  []appearance
	}{
		{"light", "light", []appearance{appearanceLight}},
		{"dark", "dark", []appearance{appearanceDark}},
		{"system follows the system", "system", []appearance{appearanceSystem}},
		{"unknown", "sepia", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []appearance
			old := setAppearance
			setAppearance = func(a appearance) { got = append(got, a) }
			t.Cleanup(func() { setAppearance = old })
			(&App{}).SetAppearance(tt.theme)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The platform call takes every appearance (on macOS it's queued for the
// main thread, which a test doesn't run).
func TestNativeAppearance(t *testing.T) {
	for _, a := range []appearance{appearanceSystem, appearanceLight, appearanceDark} {
		assert.NotPanics(t, func() { nativeAppearance(a) })
	}
}
