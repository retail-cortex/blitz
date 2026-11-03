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
	"os"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/client"
)

func newEnterpriseSyncCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Save this workspace's changes to your organization's service now, and say how its sync stands",
		Long: `Blitz saves an organization workspace by itself, a moment after you change
it. This saves the workspace you're in now: it commits what's changed, takes
others' changes, and pushes to the workspace's main on your organization's
service. "blitz enterprise sync status" only says how it stands.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return syncCmd(cmd, true)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "How this workspace's sync stands: saved, saving, offline, signed out, refused, or both versions of files kept",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return syncCmd(cmd, false)
		},
	})
	return cmd
}

func syncCmd(cmd *cobra.Command, save bool) error {
	sock, err := ensureService(cmd.Context())
	if err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	s := client.AttachSettings(sock)
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
	if failedSync(st.State) {
		return withCode(exitFailure, errors.New("not saved to the service yet"))
	}
	return nil
}

// failedSync is whether a sync's state means the service hasn't got this
// computer's saves.
func failedSync(state string) bool {
	return state == "offline" || state == "signed_out" || state == "refused"
}

func printOrgSync(w io.Writer, st client.OrgSync) {
	switch st.State {
	case "saved":
		fmt.Fprintln(w, "✓ Saved: the service has everything")
	case "saving":
		fmt.Fprintln(w, "Saving…")
	case "offline":
		fmt.Fprintln(w, "Not saved: the service can't be reached, so the changes wait on this computer")
	case "signed_out":
		fmt.Fprintln(w, "Not saved: signed out (blitz enterprise login)")
	case "refused":
		fmt.Fprintln(w, "Not saved: the service refused it")
	case "conflicts":
		fmt.Fprintln(w, "Saved, with both versions of these files kept:")
		for _, p := range st.Pairs {
			fmt.Fprintf(w, "  %s (yours: %s)\n", p[0], p[1])
		}
	default:
		fmt.Fprintln(w, st.State)
	}
	if st.Error != "" {
		fmt.Fprintln(w, "  "+st.Error)
	}
}
