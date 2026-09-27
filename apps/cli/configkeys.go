package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// keyCommands manage providers' API keys: kept in the OS keychain, the
// settings file referring to them. --workspace works on the workspace's
// own settings (--dir, else the current directory) instead of the global
// ones.
func keyCommands(g *globalFlags) []*cobra.Command {
	var workspace bool
	scope := func() (string, error) {
		if !workspace {
			return "", nil
		}
		cfg, err := loadConfig(g)
		if err != nil {
			return "", err
		}
		return cfg.Tools.WorkspaceDir, nil
	}
	done := func(cmd *cobra.Command, path string) {
		fmt.Fprintf(cmd.OutOrStdout(), "Updated %s. Workspaces open in the service use it when they reopen (the desktop app's settings apply at once).\n", path)
	}
	provider := func(args []string) (string, error) {
		p := strings.ToLower(args[0])
		for _, k := range config.KeyedProviders {
			if k == p {
				return p, nil
			}
		}
		return "", withCode(exitUsage, fmt.Errorf("%q takes no API key (one of %s)", args[0], strings.Join(config.KeyedProviders, ", ")))
	}

	keys := &cobra.Command{
		Use:   "keys",
		Short: "Show each provider's API key source (never the key)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := scope()
			if err != nil {
				return err
			}
			info, err := config.Describe(g.config, dir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "settings: %s\nkeys kept in: %s\n", info.Path, info.SecretStore)
			for _, p := range info.Providers {
				src := string(p.KeySource)
				if p.KeyMissing {
					src += " (missing from the store: set it again)"
				}
				if p.KeySource == config.KeyPlain || p.KeySource == config.KeyObfuscated {
					src += " (move it: blitz config secure-key " + p.Name + ")"
				}
				fmt.Fprintf(out, "  %-10s %s\n", p.Name, src)
			}
			return nil
		},
	}
	setKey := &cobra.Command{
		Use:   "set-key <provider>",
		Short: "Store a provider's API key in the OS keychain (read from stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := provider(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			key, err := readSecret(cmd, fmt.Sprintf("%s API key: ", p))
			if err != nil {
				return err
			}
			path, err := config.SetAPIKey(g.config, dir, p, key)
			if err != nil {
				return err
			}
			done(cmd, path)
			return nil
		},
	}
	removeKey := &cobra.Command{
		Use:   "remove-key <provider>",
		Short: "Remove a provider's API key (a workspace then uses the global one)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := provider(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			path, err := config.RemoveAPIKey(g.config, dir, p)
			if err != nil {
				return err
			}
			done(cmd, path)
			return nil
		},
	}
	secureKey := &cobra.Command{
		Use:   "secure-key <provider>",
		Short: "Move a key written in the settings file into the OS keychain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := provider(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			path, err := config.SecureAPIKey(g.config, dir, p)
			if err != nil {
				return err
			}
			done(cmd, path)
			return nil
		},
	}
	all := []*cobra.Command{keys, setKey, removeKey, secureKey}
	for _, c := range all {
		c.Flags().BoolVarP(&workspace, "workspace", "w", false, "The workspace's own settings (--dir, else the current directory)")
	}
	return all
}

// readSecret reads one line: without echo from a terminal, else from
// stdin as piped. A key never goes in arguments, where ps and the shell's
// history would see it.
func readSecret(cmd *cobra.Command, prompt string) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(cmd.ErrOrStderr(), prompt)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if line = strings.TrimSpace(line); line == "" {
		return "", withCode(exitUsage, fmt.Errorf("no key on stdin"))
	}
	return line, nil
}
