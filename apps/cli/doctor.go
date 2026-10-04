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
	"errors"
	"os/exec"

	"github.com/spf13/cobra"
)

// newDoctorCommand is blitz doctor: blitzd doctor runs the checks (they
// build models, sandboxes and MCP clients, which only the service
// carries), with this run's settings, and prints them here. It needn't
// have a service running.
func newDoctorCommand(g *globalFlags) *cobra.Command {
	var online bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration, credentials, sandbox and integrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bin, err := serviceBinary()
			if err != nil {
				return withCode(exitUsage, err)
			}
			c := exec.CommandContext(cmd.Context(), bin, doctorArgs(g, online)...)
			c.Stdin, c.Stdout, c.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := c.Run(); err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					return withCode(exit.ExitCode(), errors.New("some checks failed"))
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&online, "online", false, "Also send a tiny request to the model and connect to MCP servers")
	return cmd
}

// doctorArgs are blitzd doctor's flags for this run's settings.
func doctorArgs(g *globalFlags, online bool) []string {
	args := []string{"doctor"}
	add := func(flag, v string) {
		if v != "" {
			args = append(args, flag, v)
		}
	}
	add("--config", g.config)
	add("--dir", g.dir)
	add("--model", g.model)
	add("--agent", g.agent)
	add("--agency", g.agency)
	for _, d := range g.pluginDirs {
		add("--plugin-dir", d)
	}
	for _, d := range g.addDirs {
		add("--add-dir", d)
	}
	if online {
		args = append(args, "--online")
	}
	return args
}
