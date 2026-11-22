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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/plugins"
)

// argValues are what a command's arguments complete to in the shell, by
// its path (spec_067_cli_parity CLP-14): each reads the settings or the
// catalog, never the network.
func argValues(g *globalFlags) map[string]func(args []string) []string {
	mcpNames := func([]string) []string {
		dir := g.dir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		servers, err := mcpServers(g, dir)
		if err != nil {
			servers, _ = mcpServers(g, "")
		}
		var out []string
		for _, s := range servers {
			out = append(out, s.Name)
		}
		return out
	}
	pluginNames := func([]string) []string {
		all, _ := plugins.Default().List()
		var out []string
		for _, p := range all {
			out = append(out, p.Name)
		}
		return out
	}
	providers := func(args []string) []string {
		if len(args) == 0 {
			return config.Providers
		}
		return nil
	}
	out := map[string]func([]string) []string{
		"blitz do": func(args []string) []string {
			if len(args) > 0 {
				return nil
			}
			var names []string
			for _, c := range api.BlitzCommands {
				names = append(names, c.Name)
			}
			return names
		},
		"blitz models":             func([]string) []string { return config.Providers },
		"blitz config set-key":     providers,
		"blitz config remove-key":  providers,
		"blitz config secure-key":  providers,
		"blitz completion install": func([]string) []string { return []string{"zsh", "bash", "fish"} },
		"blitz config set-auth": func(args []string) []string {
			if len(args) == 1 {
				return []string{"api_key", "adc", "oauth"}
			}
			return providers(args)
		},
	}
	for _, sub := range []string{"get", "remove", "enable", "disable", "make-global", "login", "logout"} {
		out["blitz mcp "+sub] = mcpNames
	}
	for _, sub := range []string{"show", "remove", "enable", "disable", "update"} {
		out["blitz plugin "+sub] = pluginNames
	}
	return out
}

// flagValues are what flags complete to, wherever they're defined.
var flagValues = map[string][]string{
	"permission-mode": {"auto", "default", "accept-edits", "plan", "dont-ask", "bypass"},
	"agency":          {"low", "medium", "high", "extreme"},
	"effort":          {"minimal", "low", "medium", "high", "max", "auto"},
	"output-format":   {formatText, "json", "stream-json"},
	"input-format":    {formatText, "stream-json"},
	"color":           {"auto", "always", "never"},
}

// addShellCompletion gives the commands their arguments' and flags'
// values for shell completion, and adds `blitz completion install`.
func addShellCompletion(root *cobra.Command, g *globalFlags) {
	values := argValues(g)
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if f, ok := values[c.CommandPath()]; ok && c.ValidArgsFunction == nil {
			c.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
				return f(args), cobra.ShellCompDirectiveNoFileComp
			}
		}
		for name, vals := range flagValues {
			if c.LocalNonPersistentFlags().Lookup(name) != nil || c == root && c.PersistentFlags().Lookup(name) != nil {
				_ = c.RegisterFlagCompletionFunc(name, cobra.FixedCompletions(vals, cobra.ShellCompDirectiveNoFileComp))
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			c.AddCommand(completionInstallCommand(root))
		}
	}
	walk(root)
}

// completionMark ends the line completion install adds to a shell's
// start-up file, so it's added once.
const completionMark = "# Blitz shell completion"

// completionInstallCommand writes the completion script for the person's
// shell and has the shell load it (CLP-14): zsh and bash source it from
// ~/.blitz/completion (one line added to ~/.zshrc or ~/.bashrc); fish reads
// ~/.config/fish/completions itself.
func completionInstallCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "install [zsh|bash|fish]",
		Short: "Install shell completion for your shell (from $SHELL when not named)",
		Long: "Writes the completion script and has your shell load it: zsh and bash source it from ~/.blitz/completion " +
			"(one line added to ~/.zshrc or ~/.bashrc, once); fish reads ~/.config/fish/completions on its own. Open a new shell after.",
		Example: "  blitz completion install\n  blitz completion install fish",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := filepath.Base(os.Getenv("SHELL"))
			if len(args) == 1 {
				shell = args[0]
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			script, rc, err := installCompletion(root, shell, home)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("completion.installed", "shell", shell, "path", script))
			if rc != "" {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("completion.rc", "path", rc))
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("completion.new_shell"))
			return nil
		},
	}
}

// installCompletion writes shell's script under home, and the start-up
// file line that loads it ("" when the shell needs none, or already had
// it).
func installCompletion(root *cobra.Command, shell, home string) (script, rc string, err error) {
	var buf bytes.Buffer
	switch shell {
	case "zsh":
		err = root.GenZshCompletion(&buf)
		script, rc = filepath.Join(home, ".blitz", "completion", "blitz.zsh"), filepath.Join(home, ".zshrc")
	case "bash":
		err = root.GenBashCompletionV2(&buf, true)
		script, rc = filepath.Join(home, ".blitz", "completion", "blitz.bash"), filepath.Join(home, ".bashrc")
	case "fish":
		err = root.GenFishCompletion(&buf, true)
		script = filepath.Join(home, ".config", "fish", "completions", "blitz.fish")
	default:
		return "", "", withCode(exitUsage, fmt.Errorf("shell completion installs for zsh, bash or fish; for %q, run 'blitz completion %s' and load what it prints", shell, shell))
	}
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(script, buf.Bytes(), 0o644); err != nil {
		return "", "", err
	}
	if rc == "" {
		return script, "", nil
	}
	old, err := os.ReadFile(rc)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	if strings.Contains(string(old), completionMark) {
		return script, "", nil
	}
	q := shellQuote(script)
	line := "[ -f " + q + " ] && source " + q + " " + completionMark + "\n" // shell code, not words for the person
	if len(old) > 0 && !bytes.HasSuffix(old, []byte("\n")) {
		line = "\n" + line
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return "", "", err
	}
	return script, rc, nil
}

// completionShells are the shells completion installs for, for doctor.
var completionShells = map[string]func(home string) string{
	"zsh":  func(home string) string { return filepath.Join(home, ".blitz", "completion", "blitz.zsh") },
	"bash": func(home string) string { return filepath.Join(home, ".blitz", "completion", "blitz.bash") },
	"fish": func(home string) string { return filepath.Join(home, ".config", "fish", "completions", "blitz.fish") },
}

// completionInstalled reports whether completion is installed for the
// shell in $SHELL (true when it's one completion doesn't install for).
func completionInstalled() bool {
	home, err := os.UserHomeDir()
	path, known := completionShells[filepath.Base(os.Getenv("SHELL"))]
	if err != nil || !known {
		return true
	}
	_, err = os.Stat(path(home))
	return err == nil
}

// shellQuote quotes s for a POSIX shell, in single quotes.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
