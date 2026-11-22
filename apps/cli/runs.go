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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// newRunsCommand is blitz runs: the background runs (blitz --bg) by the
// name the desktop app gives them, so "agents" can mean the agents, as
// /agents does (spec_067_cli_parity CLP-50): runs (or runs list), runs
// logs, runs stop, runs attach.
func newRunsCommand(o *rootOptions) *cobra.Command {
	list := newAgentsCommand()
	list.Use, list.Short = "list", "List the background runs in the Blitz service (blitz --bg), in every workspace"
	runs := &cobra.Command{
		Use:   "runs",
		Short: "The background runs (blitz --bg): list them, show one's log, stop or attach to one",
		Args:  cobra.NoArgs,
		RunE:  list.RunE,
	}
	runs.Flags().AddFlagSet(list.Flags())
	runs.AddCommand(list, newLogsCommand(), newStopCommand(), newAttachCommand(o))
	return runs
}

// movedToRuns marks an old top-level run command: it works for a release,
// hidden, saying its new name (CLP-50).
func movedToRuns(c *cobra.Command) *cobra.Command {
	c.Deprecated = "use blitz runs " + c.Name()
	return c
}

// agentsForNow is blitz agents as it was: the background runs, saying it
// moves to blitz runs, and that blitz agents will list the agents.
func agentsForNow() *cobra.Command {
	c := newAgentsCommand()
	c.Short = "List the background runs (now blitz runs; blitz agents will list your agents)"
	run := c.RunE
	c.RunE = func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("runs.agents_moved"))
		return run(cmd, args)
	}
	return c
}
