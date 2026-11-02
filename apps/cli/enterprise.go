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
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/spf13/cobra"
)

// newEnterpriseCommand is `blitz enterprise connect|disconnect|login|
// logout|status`: connecting this computer to the organization's Blitz
// service and signing in to it (spec_053_enterprise EN-10 to EN-14). The
// Blitz service keeps the connection, so the desktop app, the tray and
// the CLI share one sign-in.
func newEnterpriseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enterprise",
		Short: "Connect to your organization's Blitz service and sign in to it",
		Long: `Connects this computer to your organization's Blitz service, and signs in to
it with your organization's account. Connected, the organization's models
are the ones Blitz uses, under its limits.`,
	}
	settings := func(cmd *cobra.Command) (*client.Settings, error) {
		sock, err := ensureService(cmd.Context())
		if err != nil {
			return nil, err
		}
		return client.AttachSettings(sock), nil
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the organization's service, whether it's reached, and who is signed in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			o, err := s.Organization(cmd.Context(), true)
			if err != nil {
				return err
			}
			describeOrganization(cmd.OutOrStdout(), o)
			return nil
		},
	}
	connect := &cobra.Command{
		Use:   "connect <address>",
		Short: "Connect to your organization's service, such as https://blitz.acme.com",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			o, err := s.Connect(cmd.Context(), args[0])
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Connected to %s (%s). Sign in: blitz enterprise login\n", orgName(o), o.URL)
			return nil
		},
	}
	disconnect := &cobra.Command{
		Use:   "disconnect",
		Short: "Sign out and disconnect: Blitz uses your own providers again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			if _, err := s.Connect(cmd.Context(), ""); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Disconnected: Blitz uses your own model providers again.")
			return nil
		},
	}
	var browser bool
	login := &cobra.Command{
		Use:   "login",
		Short: "Sign in with your organization's account, with a code entered in any browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt) // Ctrl+C stops the sign-in
			defer stop()
			out := cmd.OutOrStdout()
			o, err := s.SignInOrganization(ctx, !browser, func(p client.OrganizationPrompt) {
				if p.Code != "" {
					fmt.Fprintf(out, "Open %s and enter the code %s (Ctrl+C stops).\n", p.URL, p.Code)
				} else {
					fmt.Fprintf(out, "Open this address to sign in (Ctrl+C stops):\n  %s\n", p.URL)
				}
			})
			if err != nil {
				return withCode(exitFailure, err)
			}
			fmt.Fprintf(out, "✓ Signed in to %s as %s\n", orgName(o), o.Email)
			return askTerms(ctx, cmd, s, orgName(o))
		},
	}
	login.Flags().BoolVar(&browser, "browser", false, "Sign in in a browser on this computer instead of with a code")
	logout := &cobra.Command{
		Use:   "logout",
		Short: "Sign out of your organization's service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			o, err := s.SignOutOrganization(cmd.Context())
			if err != nil {
				return err
			}
			if o.URL == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "Not connected to an organization.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s.\n", orgName(o))
			return nil
		},
	}
	terms := &cobra.Command{
		Use:   "terms",
		Short: "Read your organization's terms and accept them, if a version waits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			o, err := s.Organization(cmd.Context(), false)
			if err != nil {
				return err
			}
			return askTerms(cmd.Context(), cmd, s, orgName(o))
		},
	}
	cmd.AddCommand(status, connect, disconnect, login, logout, terms)
	return cmd
}

// orgName is the service's name, else its address.
func orgName(o client.Organization) string {
	if o.Name != "" {
		return o.Name
	}
	return o.URL
}

// describeOrganization writes the connection and the sign-in.
func describeOrganization(w io.Writer, o client.Organization) {
	if o.URL == "" {
		fmt.Fprintln(w, "Not connected to an organization: blitz enterprise connect <address>")
		return
	}
	fmt.Fprintf(w, "Organization  %s (%s)\n", orgName(o), o.URL)
	switch {
	case o.Checked && !o.Reachable:
		fmt.Fprintf(w, "Service       not reached: %s\n", o.Error)
	case o.Version != "":
		fmt.Fprintf(w, "Service       version %s; offers %s\n", o.Version, strings.Join(o.Capabilities, ", "))
	}
	if !o.SignedIn {
		fmt.Fprintln(w, "Signed in     no: blitz enterprise login")
		return
	}
	who := o.Email
	if o.PersonName != "" {
		who = o.PersonName + " <" + o.Email + ">"
	}
	fmt.Fprintf(w, "Signed in     %s", who)
	if len(o.Roles) > 0 {
		fmt.Fprintf(w, " (%s)", strings.Join(o.Roles, ", "))
	}
	fmt.Fprintln(w)
	if !o.Renews.IsZero() {
		fmt.Fprintf(w, "Renews        %s\n", o.Renews.Local().Format(time.Kitchen))
	}
	if o.Terms != "" {
		fmt.Fprintf(w, "Terms         version %s to accept\n", o.Terms)
	}
}

// askTerms shows the organization's terms when a version waits to be
// accepted, and asks [y/N] (spec_053_enterprise EN-53): yes records it
// with the organization, anything else signs out of it, since its service
// refuses everything else until they're accepted.
func askTerms(ctx context.Context, cmd *cobra.Command, s *client.Settings, name string) error {
	out := cmd.OutOrStdout()
	t, err := s.Terms(ctx)
	if err != nil {
		return err
	}
	if t.Version == "" || t.Accepted {
		fmt.Fprintf(out, "%s's terms: nothing to accept.\n", name)
		return nil
	}
	fmt.Fprintf(out, "\n%s asks you to accept its terms (version %s):\n\n", name, t.Version)
	if t.Text != "" {
		fmt.Fprintln(out, t.Text)
	} else {
		fmt.Fprintf(out, "  Read them at %s\n", t.URL)
	}
	fmt.Fprint(out, "\nAccept them? [y/N] ")
	answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		if _, err := s.SignOutOrganization(ctx); err != nil {
			return err
		}
		fmt.Fprintf(out, "Not accepted: signed out of %s.\n", name)
		return withCode(exitFailure, errors.New("the organization's terms weren't accepted"))
	}
	if _, err := s.AcceptTerms(ctx, t.Version); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Accepted %s's terms (version %s).\n", name, t.Version)
	return nil
}
