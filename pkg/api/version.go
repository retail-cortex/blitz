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

package api

import "regexp"

// release is a stamped release version: 0.6.2, or 0.6.2-3-gabc1234 a few
// commits on.
var release = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// ShownVersion is a Blitz version as people see it: a release's with its
// "v" (v0.6.2), as the tags, the releases and the docs write it; anything
// else ("dev", a commit) as it is. The stamped version has no "v", since
// packages need it to start with a digit, and it's what the desktop app
// compares with the service's.
func ShownVersion(v string) string {
	if release.MatchString(v) {
		return "v" + v
	}
	return v
}
