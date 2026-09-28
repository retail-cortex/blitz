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

// The About view's texts are the embedded ones; others are refused.
func TestLicense(t *testing.T) {
	a := &App{}
	for which, want := range map[string]string{"notice": "Retail Cortex", "full": "Apache License", "third-party": "THIRD-PARTY NOTICES"} {
		t.Run(which, func(t *testing.T) {
			text, err := a.License(which)
			assert.NoError(t, err, "License(%q): %v, no %q", which, err, want)
			assert.Contains(t, text, want, "License(%q): %v, no %q", which, err, want)
		})
	}
	_, err := a.License("bogus")
	assert.Error(t, err, "bogus accepted")
}
