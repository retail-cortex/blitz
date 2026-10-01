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

// Package configtest configures Blitz for tests.
package configtest

import "github.com/retail-cortex/blitz/pkg/config"

// RunTools lets the agent's tools run without asking, with or without the
// OS sandbox. Bypass mode (auto_approve) needs the sandbox; where there's
// none (Ubuntu restricting unprivileged user namespaces, a container) the
// session falls back to asking, which no test answers, so allow rules for
// shell commands, writes and deletes cover it.
func RunTools(c *config.Config) {
	c.Blitz.AutoApprove = true
	c.Permissions.Allow = append(c.Permissions.Allow, "shell(*)", "write(**)", "delete(**)")
}
