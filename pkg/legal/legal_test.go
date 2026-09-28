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

package legal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The embedded texts are the repository's, whole.
func TestTexts(t *testing.T) {
	for name, c := range map[string]struct{ text, want string }{
		"License":    {License, "Apache License"},
		"Notice":     {Notice, "Blitz\nCopyright 2026 Retail Cortex"},
		"ThirdParty": {ThirdParty, "THIRD-PARTY NOTICES"},
	} {
		assert.Contains(t, c.text, c.want, "%s doesn't contain %q", name, c.want)
	}
	assert.Contains(t, ThirdParty, "Go standard library", "third-party notices miss Go or the page's packages")
	assert.Contains(t, ThirdParty, "react", "third-party notices miss Go or the page's packages")
	s := Summary("blitz license")
	assert.Contains(t, s, "blitz license third-party", "summary:\n%s", s)
	assert.Contains(t, s, "Code Puppy", "summary:\n%s", s)
}
