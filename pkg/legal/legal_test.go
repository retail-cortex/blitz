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
	"strings"
	"testing"
)

// The embedded texts are the repository's, whole.
func TestTexts(t *testing.T) {
	for name, c := range map[string]struct{ text, want string }{
		"License":    {License, "Apache License"},
		"Notice":     {Notice, "Blitz\nCopyright 2026 Retail Cortex"},
		"ThirdParty": {ThirdParty, "THIRD-PARTY NOTICES"},
	} {
		if !strings.Contains(c.text, c.want) {
			t.Errorf("%s doesn't contain %q", name, c.want)
		}
	}
	if !strings.Contains(ThirdParty, "Go standard library") || !strings.Contains(ThirdParty, "react") {
		t.Error("third-party notices miss Go or the page's packages")
	}
	if s := Summary("blitz license"); !strings.Contains(s, "blitz license third-party") || !strings.Contains(s, "Code Puppy") {
		t.Errorf("summary:\n%s", s)
	}
}
