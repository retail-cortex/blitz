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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/gitcred"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newEnterpriseCommand is `blitz enterprise connect|disconnect|login|
// logout|status`: connecting this computer to the organization's Blitz
// service and signing in to it (spec_053_enterprise EN-10 to EN-14). The
// Blitz service keeps the connection, so the desktop app, the tray and
// the CLI share one sign-in.
func newEnterpriseCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enterprise",
		Short: "Connect to your organization's Blitz service and sign in to it",
		Long: `Connects this computer to your organization's Blitz service, and signs in to
it with your organization's account. Connected, the organization's models
are the ones Blitz uses, under its limits.`,
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the organization's service, whether it's reached, and who is signed in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
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
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			o, err := s.Connect(cmd.Context(), args[0])
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ "+i18n.T("org.connected", "name", orgName(o), "url", o.URL))
			return nil
		},
	}
	disconnect := &cobra.Command{
		Use:   "disconnect",
		Short: "Sign out and disconnect: Blitz uses your own providers again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			if _, err := s.Connect(cmd.Context(), ""); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.disconnected"))
			return nil
		},
	}
	var browser bool
	login := &cobra.Command{
		Use:   "login",
		Short: "Sign in with your organization's account, with a code entered in any browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt) // Ctrl+C stops the sign-in
			defer stop()
			out := cmd.OutOrStdout()
			o, err := s.SignInOrganization(ctx, !browser, func(p client.OrganizationPrompt) {
				if p.Code != "" {
					fmt.Fprintln(out, i18n.T("org.login_code", "url", p.URL, "code", p.Code))
				} else {
					fmt.Fprintf(out, "%s\n  %s\n", i18n.T("org.login_url"), p.URL)
				}
			})
			if err != nil {
				return withCode(exitFailure, err)
			}
			fmt.Fprintln(out, "✓ "+i18n.T("org.signed_in", "name", orgName(o), "email", o.Email))
			return askTerms(ctx, cmd, s, orgName(o))
		},
	}
	login.Flags().BoolVar(&browser, "browser", false, "Sign in in a browser on this computer instead of with a code")
	logout := &cobra.Command{
		Use:   "logout",
		Short: "Sign out of your organization's service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			o, err := s.SignOutOrganization(cmd.Context())
			if err != nil {
				return err
			}
			if o.URL == "" {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.not_connected"))
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.signed_out", "name", orgName(o)))
			return nil
		},
	}
	terms := &cobra.Command{
		Use:   "terms",
		Short: "Read your organization's terms and accept them, if a version waits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
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
	about := &cobra.Command{
		Use:   "about",
		Short: "What your organization says about itself: about text, support, terms and privacy notice",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			b, why, err := s.Branding(cmd.Context(), true)
			if err != nil {
				return err
			}
			describeBranding(cmd.OutOrStdout(), b, why)
			return nil
		},
	}
	workspaces := &cobra.Command{
		Use:   "workspaces",
		Short: "List your organization's workspaces you have a role in, and where each is cloned",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			list, org, err := s.OrgWorkspaces(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, i18n.T("org.no_workspaces", "org", org))
				return nil
			}
			tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			for _, w := range list {
				where := i18n.T("org.not_cloned", "handle", w.Handle)
				if w.Cloned {
					where = w.Dir
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", w.Handle, w.Name, w.Role, where)
			}
			return tw.Flush()
		},
	}
	open := &cobra.Command{
		Use:   "open <workspace>",
		Short: "Clone one of your organization's workspaces, the first time, and say where it is",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			dir, cloned, err := s.OpenOrgWorkspace(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if cloned {
				fmt.Fprintln(cmd.OutOrStdout(), "✓ "+i18n.T("org.cloned", "dir", dir))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), dir)
			}
			return nil
		},
	}
	var personal bool
	var handle string
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Make a workspace of your organization's, with you its owner",
		Long: `Makes a workspace of your organization's, with you its owner. --personal
makes it yours alone, its handle ~<you>/<name>: anyone may have one called
home.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			w, err := s.CreateOrgWorkspace(cmd.Context(), args[0], handle, personal)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ "+i18n.T("org.created", "name", w.Name, "handle", w.Handle))
			return nil
		},
	}
	create.Flags().BoolVar(&personal, "personal", false, "make it yours alone (~<you>/<name>)")
	create.Flags().StringVar(&handle, "handle", "", "its handle (default: made from the name)")
	var shareName, shareHandle string
	var sharePersonal bool
	share := &cobra.Command{
		Use:   "share [folder]",
		Short: "Share a folder with your organization where it is, and keep it in step",
		Long: `Shares a folder (--dir's, else the current one) with your organization where
it is: its changes are committed, the folder and its history are checked for
secrets (nothing is sent if any are found), a workspace is made for it and
the folder is pushed there, then kept in step as you work. A repository's own
origin stays as it was; the organization's is the remote "organization".`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := orgWorkspace(g) // --dir's, else the current folder
			if len(args) == 1 {
				abs, err = filepath.Abs(args[0])
			}
			if err != nil {
				return err
			}
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			w, notes, err := s.ShareWithOrganization(cmd.Context(), abs, shareName, shareHandle, sharePersonal)
			if err != nil {
				return err
			}
			for _, n := range notes {
				fmt.Fprintln(cmd.OutOrStdout(), "! "+n)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✓ "+i18n.T("org.shared", "dir", abs, "name", w.Name, "handle", w.Handle))
			return nil
		},
	}
	share.Flags().StringVar(&shareName, "name", "", "the workspace's name (default: the folder's)")
	share.Flags().StringVar(&shareHandle, "handle", "", "its handle (default: made from the name)")
	share.Flags().BoolVar(&sharePersonal, "personal", false, "make it yours alone (~<you>/<name>)")
	cmd.AddCommand(status, connect, disconnect, login, logout, terms, about, workspaces, open, create, share, newEnterpriseJobsCommand(g), newEnterpriseUsageCommand(), newEnterprisePublishCommand(g), newEnterpriseReviewsCommand(), newEnterpriseCatalogCommand(g), newEnterpriseNoticesCommand(), newEnterpriseSyncCommand(g))
	return cmd
}

// orgSettings attaches to the Blitz service (starting it if need be) for
// the enterprise commands.
func orgSettings(cmd *cobra.Command) (*client.Settings, error) {
	sock, err := ensureService(cmd.Context())
	if err != nil {
		return nil, err
	}
	return client.AttachSettings(sock), nil
}

// orgWorkspace is the workspace an enterprise command works in: the
// global --dir's, else the current folder.
func orgWorkspace(g *globalFlags) (string, error) {
	if g.dir == "" {
		return os.Getwd()
	}
	return filepath.Abs(config.ExpandHome(g.dir))
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
		fmt.Fprintln(w, i18n.T("org.status_not_connected"))
		return
	}
	fmt.Fprintln(w, i18n.T("org.status_org", "name", orgName(o), "url", o.URL))
	if o.Managed {
		fmt.Fprintln(w, i18n.T("org.status_managed"))
	}
	switch {
	case o.Checked && !o.Reachable:
		fmt.Fprintln(w, i18n.T("org.status_unreached", "error", o.Error))
	case o.Version != "":
		fmt.Fprintln(w, i18n.T("org.status_service", "version", o.Version, "capabilities", strings.Join(o.Capabilities, ", ")))
	}
	if !o.SignedIn {
		fmt.Fprintln(w, i18n.T("org.status_signed_out"))
		return
	}
	who := o.Email
	if o.PersonName != "" {
		who = o.PersonName + " <" + o.Email + ">"
	}
	fmt.Fprint(w, i18n.T("org.status_signed_in", "who", who))
	if len(o.Roles) > 0 {
		fmt.Fprintf(w, " (%s)", strings.Join(o.Roles, ", "))
	}
	fmt.Fprintln(w)
	if !o.Renews.IsZero() {
		fmt.Fprintln(w, i18n.T("org.status_renews", "time", o.Renews.Local().Format(time.Kitchen)))
	}
	if o.Terms != "" {
		fmt.Fprintln(w, i18n.T("org.status_terms", "version", o.Terms))
	}
}

// describeBranding writes what the organization says about itself, as the
// desktop app's About shows it (EN-52): its name and about text, support
// link, and its documents (a privacy notice in full; the terms through
// blitz enterprise terms, where they're accepted).
func describeBranding(w io.Writer, b client.Branding, why string) {
	if why != "" {
		fmt.Fprintln(w, "! "+i18n.T("org.about_stale", "error", why))
	}
	if b.Name == "" && b.About == "" && b.Support == "" && len(b.Documents) == 0 {
		fmt.Fprintln(w, i18n.T("org.about_none"))
		return
	}
	if b.Name != "" {
		fmt.Fprintln(w, b.Name)
	}
	if b.About != "" {
		fmt.Fprintln(w, "\n"+b.About)
	}
	if b.Support != "" {
		fmt.Fprintln(w, "\n"+i18n.T("org.about_support", "url", b.Support))
	}
	for _, d := range b.Documents {
		switch d.Document {
		case "terms":
			where := cmp.Or(d.URL, "blitz enterprise terms")
			fmt.Fprintln(w, i18n.T("org.about_terms", "version", d.Version, "where", where))
		case "privacy":
			if d.URL != "" || d.Text == "" {
				fmt.Fprintln(w, i18n.T("org.about_privacy", "version", d.Version, "where", d.URL))
				continue
			}
			fmt.Fprintln(w, "\n"+strings.TrimSpace(i18n.T("org.about_privacy", "version", d.Version, "where", ""))+"\n"+d.Text)
		}
	}
}

// askTerms shows the organization's terms when a version waits to be
// accepted, and asks [y/N] (spec_053_enterprise EN-53): yes records it
// with the organization, anything else signs out of it, since its service
// refuses everything else until they're accepted. Without a terminal to
// ask in (a script), it asks nothing and stays signed in, saying how to
// accept them (EN-53a): the service still refuses the organization's
// features until they are.
func askTerms(ctx context.Context, cmd *cobra.Command, s *client.Settings, name string) error {
	out := cmd.OutOrStdout()
	t, err := s.Terms(ctx)
	if err != nil {
		return err
	}
	if t.Version == "" || t.Accepted {
		fmt.Fprintln(out, i18n.T("org.terms_none", "name", name))
		return nil
	}
	if f, ok := cmd.InOrStdin().(*os.File); ok && !term.IsTerminal(int(f.Fd())) {
		fmt.Fprintln(out, i18n.T("org.terms_waiting", "name", name, "version", t.Version))
		return nil
	}
	fmt.Fprintf(out, "\n%s\n\n", i18n.T("org.terms_ask", "name", name, "version", t.Version))
	if t.Text != "" {
		fmt.Fprintln(out, t.Text)
	} else {
		fmt.Fprintln(out, "  "+i18n.T("org.terms_read", "url", t.URL))
	}
	fmt.Fprint(out, "\n"+i18n.T("org.terms_prompt"))
	answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		if _, err := s.SignOutOrganization(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.T("org.terms_declined", "name", name))
		return withCode(exitFailure, errors.New("the organization's terms weren't accepted"))
	}
	if _, err := s.AcceptTerms(ctx, t.Version); err != nil {
		return err
	}
	fmt.Fprintln(out, "✓ "+i18n.T("org.terms_accepted", "name", name, "version", t.Version))
	return nil
}

// newGitCredentialCommand is blitz git-credential: Git's credential helper
// for the organization's service (EN-61), for setting in Git's own
// settings to use Git's clients with it (Blitz sets blitzd's in each
// organization clone). It asks the Blitz service, which renews the token.
func newGitCredentialCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "git-credential <get|store|erase>",
		Short: "Git's credential helper for your organization's Blitz service",
		Long: `Git's credential helper for your organization's Blitz service, to use Git's own
clients with its workspaces:

  git config --global credential.https://blitz.acme.com.helper "!blitz git-credential"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return gitcred.Run(cmd.Context(), args[0], cmd.InOrStdin(), cmd.OutOrStdout(), func(ctx context.Context, protocol, host string) (string, string, bool, error) {
				s, err := orgSettings(cmd)
				if err != nil {
					return "", "", false, err
				}
				return s.GitCredential(ctx, protocol, host)
			})
		},
	}
}
