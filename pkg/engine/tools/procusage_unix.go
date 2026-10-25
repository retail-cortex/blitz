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

//go:build unix

package tools

import (
	"context"
	"os/exec"
	"time"
)

// sampleGroups is every process group's usage, from one ps: each
// background process leads a group of its own (configureProcessGroup), so
// its group is it and its children.
func sampleGroups() (map[int]groupUsage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pgid=,time=,rss=").Output()
	if err != nil {
		return nil, err
	}
	return parseGroups(out), nil
}
