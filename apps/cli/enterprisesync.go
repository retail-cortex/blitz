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

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

func newEnterpriseSyncCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Save this workspace's changes to your organization's service now, and say how its sync stands",
		Long: `Blitz saves an organization workspace by itself, a moment after you change
it. This saves the workspace you're in now: it commits what's changed, takes
others' changes, and pushes to the workspace's main on your organization's
service. "blitz enterprise sync status" only says how it stands.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return syncCmd(cmd, g, true)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "How this workspace's sync stands: saved, saving, offline, signed out, refused, or both versions of files kept",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return syncCmd(cmd, g, false)
		},
	})
	return cmd
}

func syncCmd(cmd *cobra.Command, g *globalFlags, save bool) error {
	s, err := orgSettings(cmd)
	if err != nil {
		return err
	}
	dir, err := orgWorkspace(g)
	if err != nil {
		return err
	}
	st, err := s.OrgSync(cmd.Context(), dir)
	if err != nil {
		return err
	}
	if st.State == "" {
		return withCode(exitUsage, errors.New("this folder isn't kept in step with your organization: share it (blitz enterprise share), or open one of its workspaces (blitz enterprise open)"))
	}
	if save {
		if st, err = s.SaveOrgSync(cmd.Context(), dir); err != nil {
			return err
		}
	}
	printOrgSync(cmd.OutOrStdout(), st)
	if st.Failed {
		return withCode(exitFailure, errors.New("not saved to the service yet"))
	}
	return nil
}

func printOrgSync(w io.Writer, st client.OrgSync) {
	switch st.State {
	case "saved":
		fmt.Fprintln(w, "✓ "+i18n.T("org.sync_saved"))
	case "saving":
		fmt.Fprintln(w, i18n.T("org.sync_saving"))
	case "offline":
		fmt.Fprintln(w, i18n.T("org.sync_offline"))
	case "signed_out":
		fmt.Fprintln(w, i18n.T("org.sync_signed_out"))
	case "refused":
		fmt.Fprintln(w, i18n.T("org.sync_refused"))
	case "secrets":
		fmt.Fprintln(w, i18n.T("org.sync_secrets"))
	case "conflicts":
		fmt.Fprintln(w, i18n.T("org.sync_conflicts"))
		for _, p := range st.Pairs {
			fmt.Fprintln(w, "  "+i18n.T("org.sync_pair", "path", p[0], "yours", p[1]))
		}
	default:
		fmt.Fprintln(w, st.State)
	}
	if st.Error != "" {
		fmt.Fprintln(w, "  "+st.Error)
	}
}
