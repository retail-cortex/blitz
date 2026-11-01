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
	"time"

	"github.com/retail-cortex/blitz/pkg/activity"
	"github.com/retail-cortex/blitz/pkg/config"
)

// activeUse counts the CLI's use, once a day (spec_active_installs_054
// AI-03): whether it's attached to the service or runs in-process. A
// variable for tests.
var activeUse = &activity.Recorder{Dir: config.ExpandHome("~/.blitz"), Version: version, On: config.CountsUsage}

// pinging is the day's ping in flight, if one is due.
var pinging <-chan struct{}

// pingGrace is the most the CLI waits for it before exiting: a ping not
// sent by then is kept and sent next time (AI-11, AI-12).
var pingGrace = time.Second

func markUse() { pinging = activeUse.Use(activity.CLI) }

// waitForPing gives the day's ping, if there is one, up to pingGrace to
// finish before the program exits.
func waitForPing() {
	if pinging == nil {
		return
	}
	select {
	case <-pinging:
	case <-time.After(pingGrace):
	}
}
