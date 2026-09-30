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
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	defaultShellTimeout = 120 * time.Second
	maxShellTimeout     = 30 * time.Minute
	shellOutputLimit    = 100 * 1024
	// shellWaitDelay bounds how long Wait blocks on pipes held open by
	// descendants after the process has been killed.
	shellWaitDelay = 2 * time.Second
)

// ShellConfig configures the run_shell_command tool.
type ShellConfig struct {
	Workspace      *Workspace
	Hooks          *Hooks
	Processes      *ProcessManager
	Policy         *CommandPolicy // nil: every command needs approval
	Exec           *ExecEnv       // nil: no OS sandbox
	DefaultTimeout time.Duration
}

// RunShellCommandInput defines arguments for executing shell commands.
type RunShellCommandInput struct {
	Command        string `json:"command" jsonschema:"The shell command to execute"`
	Cwd            string `json:"cwd,omitempty" jsonschema:"Optional working directory inside the workspace"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Timeout in seconds (default 120, max 1800)"`
	Background     bool   `json:"background,omitempty" jsonschema:"Run asynchronously; inspect or stop it with manage_background_process"`
	// Notify and NotifyPattern (background only) tell the agent about the
	// process as it runs (spec_parity_027 PAR-TOOL-04).
	Notify        bool   `json:"notify,omitempty" jsonschema:"Background only: tell you when the process exits (with its last output), during your turn or in a new one"`
	NotifyPattern string `json:"notify_pattern,omitempty" jsonschema:"Background only: also tell you each output line matching this regular expression (up to 20), e.g. ERROR|listening on"`
}

// RunShellCommandOutput holds command execution results.
type RunShellCommandOutput struct {
	Output       string `json:"output"`
	ExitCode     int    `json:"exit_code"`
	DurationMs   int64  `json:"duration_ms"`
	IsBackground bool   `json:"is_background"`
	ProcessID    int    `json:"process_id,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
	Error        string `json:"error,omitempty"`
}

func backgroundStarted(id int, notify bool) string {
	s := fmt.Sprintf("Process started in background with ID %d; use manage_background_process to read output or kill it", id)
	if notify {
		s += "; you'll be told when it exits (and about matching lines)"
	}
	return s
}

// NewRunShellCommandTool creates an ADK tool for running shell commands.
func NewRunShellCommandTool(cfg ShellConfig) (tool.Tool, error) {
	if cfg.Workspace == nil {
		return nil, errors.New("shell tool requires a workspace")
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaultShellTimeout
	}

	return functiontool.New(
		functiontool.Config{
			Name:        "run_shell_command",
			Description: "Execute a shell command with timeout, output capture, and optional background execution",
		},
		func(ctx agent.Context, input RunShellCommandInput) (RunShellCommandOutput, error) {
			return runShellCommand(ctx, cfg, input), nil
		},
	)
}

func runShellCommand(ctx context.Context, cfg ShellConfig, input RunShellCommandInput) RunShellCommandOutput {
	if input.Command == "" {
		return RunShellCommandOutput{Error: "command cannot be empty"}
	}

	cwdRel := "."
	if input.Cwd != "" {
		rel, err := cfg.Workspace.Rel(input.Cwd)
		if err != nil {
			return RunShellCommandOutput{Error: err.Error()}
		}
		cwdRel = rel
	}
	cwd, err := cfg.Workspace.Abs(cwdRel)
	if err != nil {
		return RunShellCommandOutput{Error: fmt.Sprintf("invalid cwd: %v", err)}
	}

	decision := cfg.Policy.Evaluate(input.Command)
	if decision.Verdict == VerdictDeny {
		return RunShellCommandOutput{Error: "blocked by command policy: " + decision.Reason}
	}
	if decision.Verdict != VerdictAutoApprove {
		detail := input.Command
		if cwdRel != "." {
			detail = fmt.Sprintf("(in %s) %s", cwdRel, input.Command)
		}
		if input.Background {
			detail = "[background] " + detail
		}
		if decision.Reason != "" {
			detail += "\n(" + decision.Reason + ")"
		}
		if err := cfg.Hooks.Approve(ctx, api.ApprovalRequest{
			Tool: "run_shell_command", Kind: api.ActionCommand, Detail: detail,
			Key: "cmd:" + cfg.Workspace.Dir() + "\x00" + input.Command, KeyLabel: "this exact command in this workspace",
			Targets: []string{input.Command}, MustAsk: decision.MustAsk,
		}); err != nil {
			return RunShellCommandOutput{Error: err.Error()}
		}
	}

	if input.Background {
		if cfg.Processes == nil {
			return RunShellCommandOutput{Error: "background execution is not available"}
		}
		var watch *Watch
		if input.Notify || input.NotifyPattern != "" {
			watch = &Watch{Exit: true}
			if input.NotifyPattern != "" {
				re, err := regexp.Compile(input.NotifyPattern)
				if err != nil {
					return RunShellCommandOutput{Error: fmt.Sprintf("notify_pattern: %v", err)}
				}
				watch.Lines = re
			}
		}
		bp, err := cfg.Processes.StartWatched(sessionOf(ctx), input.Command, cwd, watch)
		if err != nil {
			return RunShellCommandOutput{Error: fmt.Sprintf("failed to start background command: %v", err)}
		}
		return RunShellCommandOutput{
			Output:       backgroundStarted(bp.ID, input.Notify || input.NotifyPattern != ""),
			IsBackground: true,
			ProcessID:    bp.ID,
		}
	}

	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaultShellTimeout
	}
	timeout := min(cfg.DefaultTimeout, maxShellTimeout)
	if input.TimeoutSeconds > 0 {
		// Clamp before converting: large values overflow time.Duration.
		timeout = time.Duration(min(int64(input.TimeoutSeconds), int64(maxShellTimeout/time.Second))) * time.Second
	}

	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, err := cfg.Exec.command(cmdCtx, []string{"bash", "-c", input.Command})
	if err != nil {
		return RunShellCommandOutput{Error: fmt.Sprintf("failed to prepare command: %v", err)}
	}
	cmd.Dir = cwd

	// A single writer for both streams keeps stdout/stderr interleaving and
	// enforces the cap while the command runs rather than after the fact.
	out := newCappedBuffer(shellOutputLimit)
	cmd.Stdout = out
	cmd.Stderr = out

	runErr := cmd.Run()
	result := RunShellCommandOutput{
		Output:     out.String(),
		DurationMs: time.Since(start).Milliseconds(),
		Truncated:  out.Truncated(),
	}

	// Check the context first: a killed process also surfaces as an ExitError.
	switch {
	case errors.Is(cmdCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		result.ExitCode = -1
		result.Error = fmt.Sprintf("command timed out after %s", timeout)
	case ctx.Err() != nil:
		result.ExitCode = -1
		result.Error = "command cancelled"
	case runErr != nil:
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
			result.Error = runErr.Error()
		}
	}
	return result
}

type ownerSessionKey struct{}

// WithOwnerSession makes session the one a sub-agent's tool calls count
// for (their background processes and tasks): the session whose turn
// started the sub-agent, not its own.
func WithOwnerSession(ctx context.Context, session string) context.Context {
	if session == "" {
		return ctx
	}
	return context.WithValue(ctx, ownerSessionKey{}, session)
}

// sessionOf is the session a tool call counts for: the owner a sub-agent
// was given, else the one an agent's context says; "" for any other.
func sessionOf(ctx context.Context) string {
	if s, ok := ctx.Value(ownerSessionKey{}).(string); ok {
		return s
	}
	if c, ok := ctx.(interface{ SessionID() string }); ok {
		return c.SessionID()
	}
	return ""
}
