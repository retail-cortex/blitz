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

package tools

import (
	"context"
	"os/exec"
	goruntime "runtime"
)

// UserCommand is a command the user typed after "!", as their own shell
// would run it (bash, else sh; cmd on Windows) in dir: not an agent
// action, so neither the sandbox nor the command policy applies. It runs
// in its own process group, which ctx's end kills whole.
func UserCommand(ctx context.Context, dir, command string) *exec.Cmd {
	var cmd *exec.Cmd
	switch {
	case goruntime.GOOS == "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	default:
		sh := "sh"
		if _, err := exec.LookPath("bash"); err == nil {
			sh = "bash"
		}
		cmd = exec.CommandContext(ctx, sh, "-c", command)
	}
	cmd.Dir = dir
	configureProcessGroup(cmd)
	return cmd
}
