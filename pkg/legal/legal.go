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

// Package legal holds Blitz's license terms, embedded in every program:
// the Apache License 2.0, the NOTICE (Blitz's copyright and Code Puppy's
// MIT notice) and the notices of the third-party software the programs
// include. The CLI shows them with `blitz license` and /license, the
// service with `blitzd --license`, the desktop app in Settings › About.
//
// The texts are the repository's own LICENSE, NOTICE and
// THIRD_PARTY_NOTICES, copied in by Bazel when the package is built.
package legal

import (
	_ "embed"
	"strings"
)

// License is the Apache License, Version 2.0, under which Blitz is
// released.
//
//go:embed LICENSE
var License string

// Notice is Blitz's NOTICE: its copyright, and the notices of the
// software it began from.
//
//go:embed NOTICE
var Notice string

// ThirdParty lists the third-party software the programs include, with
// each one's license (tools/third_party_notices.sh writes it).
//
//go:embed THIRD_PARTY_NOTICES
var ThirdParty string

// Summary is what `license` shows first: the NOTICE, then where the full
// texts are. command is how to ask for them: "blitz license" (then a
// space and full or third-party), or "blitzd --license=".
func Summary(command string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(Notice))
	sep := " "
	if strings.HasSuffix(command, "=") {
		sep = ""
	}
	b.WriteString("\n\nThe full license:    " + command + sep + "full\n")
	b.WriteString("Third-party notices: " + command + sep + "third-party\n")
	return b.String()
}
