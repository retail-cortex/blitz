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

package config

import (
	"errors"
	"fmt"
	"time"
)

// DefaultStallTimeoutSeconds is llm.stall_timeout_seconds' default, the
// one place it is defined (spec_015_models MDL-51).
const DefaultStallTimeoutSeconds = 600

// The background tasks' defaults (spec_032_background_agents BGA-06),
// the one place they are defined.
const (
	// DefaultMaxBackgroundAgents is tools.max_background_agents' default.
	DefaultMaxBackgroundAgents = 4
	// DefaultBackgroundAgentTimeout is tools.background_agent_timeout's
	// default.
	DefaultBackgroundAgentTimeout = "30m"
	// DefaultBackgroundAgentMaxTurns is tools.background_agent_max_turns'
	// default.
	DefaultBackgroundAgentMaxTurns = 50
)

// BackgroundLimits are the limits background tasks run under.
type BackgroundLimits struct {
	// Agents is how many may run at once in a workspace.
	Agents int
	// Timeout stops one that runs longer.
	Timeout time.Duration
	// MaxTurns caps one's model calls, unless its agent sets max_turns.
	MaxTurns int
}

// BackgroundLimits reads the background tasks' settings. A value that
// isn't one (0, negative, a timeout that isn't a length of time) is an
// error naming its key, rather than quietly the default; an empty
// timeout is the default.
func (t ToolsConfig) BackgroundLimits() (BackgroundLimits, error) {
	l := BackgroundLimits{Agents: t.MaxBackgroundAgents, MaxTurns: t.BackgroundAgentMaxTurns}
	var errs []error
	if l.Agents <= 0 {
		errs = append(errs, fmt.Errorf("tools.max_background_agents is how many tasks may run at once, 1 or more (default %d), not %d", DefaultMaxBackgroundAgents, l.Agents))
	}
	timeout := t.BackgroundAgentTimeout
	if timeout == "" {
		timeout = DefaultBackgroundAgentTimeout
	}
	d, err := time.ParseDuration(timeout)
	if err != nil || d <= 0 {
		errs = append(errs, fmt.Errorf("tools.background_agent_timeout is a length of time like 30m or 2h (default %s), not %q", DefaultBackgroundAgentTimeout, t.BackgroundAgentTimeout))
	}
	l.Timeout = d
	if l.MaxTurns <= 0 {
		errs = append(errs, fmt.Errorf("tools.background_agent_max_turns is a task's model calls, 1 or more (default %d), not %d", DefaultBackgroundAgentMaxTurns, l.MaxTurns))
	}
	return l, errors.Join(errs...)
}
