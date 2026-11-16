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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
)

// blitz hook run (spec_043 WF-32, spec_053 EN-90): what the git hooks an
// organization's workflows install run: the workflows that run on the
// event, on the changes; it exits 1 when one fails, which stops the
// commit or push.

// maxHookInput is the most of a diff a hook's workflows read: past it, it
// is cut, and says so.
const maxHookInput = 200_000

func newHookCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Run the workflows that run on a git hook",
	}
	run := &cobra.Command{
		Use:   "run <pre-commit|pre-push>",
		Short: "Run the workflows that run on a git hook, on the changes",
		Long: `Run the workspace's workflows that run on a git hook (triggers:
git.pre_commit or git.pre_push), your organization's among them: each on
the changes (the staged diff for pre-commit; for pre-push, the commits
pushed, as git hands them on standard input), unattended. Exits 1 when
one fails, which stops the commit or push. The hooks your organization's
workflows install run this.`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"pre-commit", "pre-push"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHook(cmd, g, args[0])
		},
	}
	cmd.AddCommand(run)
	return cmd
}

func runHook(cmd *cobra.Command, g *globalFlags, event string) error {
	ctx := cmd.Context()
	input, err := hookInput(ctx, g.dir, event, cmd.InOrStdin())
	if err != nil {
		return err
	}
	cfg, err := loadConfig(g)
	if err != nil {
		return err
	}
	warn := func(s string) { fmt.Fprintln(cmd.ErrOrStderr(), s) }
	b, _, _, err := openBackend(ctx, cfg, backendOptions{trustProject: g.trustProject, flags: g}, warn)
	if err != nil {
		return err
	}
	defer b.Close()
	results, err := b.RunGitHook(ctx, event, input)
	if err != nil {
		return err
	}
	if failed := printHookResults(cmd.ErrOrStderr(), results); failed > 0 {
		return withCode(exitFailure, errors.New(i18n.T("hook.failed", "event", event, "count", failed)))
	}
	return nil
}

// printHookResults prints each workflow's ✓ or ✗ (on stderr, which git
// shows) with why, and says how many failed.
func printHookResults(out io.Writer, results []api.HookResult) int {
	failed := 0
	for _, r := range results {
		name := r.Workflow
		if r.Version != "" {
			name += "@" + r.Version
		}
		mark := "✓ "
		if !r.Passed {
			failed++
			mark = "✗ "
		}
		fmt.Fprintln(out, mark+name)
		for _, line := range []string{r.Error, r.Output} {
			if line = strings.TrimSpace(line); line != "" {
				fmt.Fprintln(out, "    "+strings.ReplaceAll(line, "\n", "\n    "))
			}
		}
	}
	return failed
}

// hookInput is what event's workflows run on: the staged diff for a
// commit; for a push, each ref's commits not yet on the remote, as git
// names them on in, one "<local ref> <local sha> <remote ref> <remote sha>"
// line each; git run in dir ("": here, where git runs a hook).
func hookInput(ctx context.Context, dir, event string, in io.Reader) (string, error) {
	var b strings.Builder
	switch event {
	case "pre-commit":
		diff, err := gitOutput(ctx, dir, "diff", "--cached")
		if err != nil {
			return "", err
		}
		b.WriteString(diff)
	case "pre-push":
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) != 4 || strings.Trim(f[1], "0") == "" {
				continue // a ref being deleted
			}
			args := []string{"log", "-p", f[1], "--not", "--remotes"}
			if strings.Trim(f[3], "0") != "" {
				args = []string{"diff", f[3] + ".." + f[1]}
			}
			diff, err := gitOutput(ctx, dir, args...)
			if err != nil {
				return "", err
			}
			b.WriteString(diff)
		}
		if err := sc.Err(); err != nil {
			return "", err
		}
	default:
		return "", withCode(exitUsage, api.ErrUnknownHook)
	}
	s := b.String()
	if len(s) > maxHookInput {
		s = s[:maxHookInput] + "\n" + i18n.T("hook.cut", "count", len(s)-maxHookInput)
	}
	return s, nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
