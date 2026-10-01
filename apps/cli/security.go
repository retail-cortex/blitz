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
	"fmt"
	"io"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/sandboxsetup"
	"github.com/spf13/cobra"
)

// newSecurityCommand is `blitz security`: whether the OS sandbox for the
// agent's commands works here, and `blitz security fix-apparmor`, which
// lets bubblewrap past AppArmor's restriction on Ubuntu 24.04 and later
// with an AppArmor profile for bwrap alone (pkg/sandboxsetup). Neither
// opens a workspace, which a required sandbox that can't run stops.
func newSecurityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Show whether the OS sandbox for the agent's commands works here",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			printSandboxStatus(cmd.OutOrStdout(), sandboxsetup.Check(cmd.Context()))
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "fix-apparmor",
		Short: "Let bubblewrap, the sandbox for the agent's commands, past AppArmor's restriction (Linux; asks for your password)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			st := sandboxsetup.Check(cmd.Context())
			if st.State != sandboxsetup.Restricted {
				printSandboxStatus(out, st)
				if st.State == sandboxsetup.NoBwrap || st.State == sandboxsetup.Broken {
					return errors.New(i18n.T("security.cant_fix"))
				}
				return nil
			}
			fmt.Fprintln(out, i18n.T("security.fixing", "bwrap", st.Bwrap, "profile", st.Profile))
			st, err := sandboxsetup.Fix(cmd.Context(), sandboxsetup.Elevation{Stdin: cmd.InOrStdin(), Stdout: out, Stderr: cmd.ErrOrStderr()})
			if err != nil {
				fmt.Fprintln(out, i18n.T("security.by_hand"))
				fmt.Fprint(out, sandboxsetup.Commands(st))
				return err
			}
			fmt.Fprintln(out, i18n.T("security.fixed"))
			return nil
		},
	})
	return cmd
}

// printSandboxStatus says what Check found.
func printSandboxStatus(out io.Writer, st sandboxsetup.Status) {
	switch st.State {
	case sandboxsetup.Ready:
		fmt.Fprintln(out, i18n.T("security.ready", "bwrap", st.Bwrap))
	case sandboxsetup.NoBwrap:
		fmt.Fprintln(out, i18n.T("security.no_bwrap"))
	case sandboxsetup.Restricted:
		fmt.Fprintln(out, i18n.T("security.restricted", "bwrap", st.Bwrap, "detail", st.Detail))
	case sandboxsetup.Broken:
		fmt.Fprintln(out, i18n.T("security.broken", "bwrap", st.Bwrap, "detail", st.Detail))
	default:
		fmt.Fprintln(out, i18n.T("security.unsupported"))
	}
}
