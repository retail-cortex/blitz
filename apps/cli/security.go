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
	"maps"
	"os"
	"slices"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/sandboxsetup"
	"github.com/retail-cortex/blitz/pkg/secretscan"
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
	cmd.AddCommand(newScanSecretsCommand(), newInstallHookCommand())
	return cmd
}

// newScanSecretsCommand is `blitz security scan-secrets`: the files a
// commit adds (--staged, what the pre-commit hook runs) or the files
// named, searched for API keys and other secrets (pkg/secretscan). It
// fails, naming each file and line with the value masked, when it finds
// one.
func newScanSecretsCommand() *cobra.Command {
	var staged bool
	cmd := &cobra.Command{
		Use:   "scan-secrets [file...]",
		Short: "Look for API keys and other secrets in the files a commit adds (--staged) or the files named",
		RunE: func(cmd *cobra.Command, paths []string) error {
			if staged == (len(paths) > 0) {
				return errors.New(i18n.T("security.scan_usage"))
			}
			files := map[string][]byte{}
			if staged {
				var err error
				if files, err = secretscan.Staged(cmd.Context(), "."); err != nil {
					return err
				}
			} else {
				for _, p := range paths {
					data, err := os.ReadFile(p)
					if err != nil {
						return err
					}
					files[p] = data
				}
			}
			var found []secretscan.Finding
			for _, name := range slices.Sorted(maps.Keys(files)) {
				found = append(found, secretscan.Scan(name, files[name])...)
			}
			if len(found) == 0 {
				return nil
			}
			errOut := cmd.ErrOrStderr()
			fmt.Fprintln(errOut, i18n.T("security.secrets_found"))
			for _, f := range found {
				fmt.Fprintf(errOut, "  %s:%d: %s (%s)\n", f.Path, f.Line, f.Kind, f.Masked)
			}
			fmt.Fprintln(errOut, i18n.T("security.secrets_how", "marker", secretscan.AllowMarker))
			cmd.SilenceUsage = true
			return withCode(exitFailure, errors.New(i18n.T("security.secrets_stopped", "n", len(found))))
		},
	}
	cmd.Flags().BoolVar(&staged, "staged", false, "Scan what the commit being made adds (the pre-commit hook's)")
	return cmd
}

// newInstallHookCommand is `blitz security install-hook`: the pre-commit
// hook that stops commits adding secrets, in the repository dir (".") is
// in, unless it uses other hooks.
func newInstallHookCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install-hook [dir]",
		Short: "Add a pre-commit hook that stops commits adding API keys or other secrets (.githooks/pre-commit)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			r, err := secretscan.Install(cmd.Context(), dir)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("security.hook."+r.Status, "path", r.Path, "detail", r.Detail))
			return nil
		},
	}
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
