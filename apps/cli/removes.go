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
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// errNotRemoved is a remove the person said no to.
var errNotRemoved = errors.New("nothing was removed")

// stdinIsTerminal reports whether cmd reads from a terminal (a variable
// for tests).
var stdinIsTerminal = func(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// removeConfirmed asks question, y/N, before a remove on a terminal
// (spec_067_cli_parity CLP-52): yes (-y) skips it, and a script (stdin
// not a terminal) goes ahead as it always has.
func removeConfirmed(cmd *cobra.Command, yes bool, question string) bool {
	if yes || !stdinIsTerminal(cmd) {
		return true
	}
	fmt.Fprint(cmd.OutOrStdout(), question+" [y/N] ")
	line, _ := readLine(cmd.InOrStdin())
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "si", "sí", "o", "oui":
		return true
	}
	return false
}

// asksBeforeRemove makes c a remove that asks first, with -y to skip the
// question, and remove, rm and delete among its names (CLP-52).
func asksBeforeRemove(c *cobra.Command, question func(args []string) string) *cobra.Command {
	var yes bool
	for _, a := range []string{"remove", "rm", "delete"} {
		if c.Name() != a && !slices.Contains(c.Aliases, a) {
			c.Aliases = append(c.Aliases, a)
		}
	}
	if c.Flags().Lookup("yes") == nil {
		c.Flags().BoolVarP(&yes, "yes", "y", false, "Don't ask first")
	}
	run := c.RunE
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if v, err := cmd.Flags().GetBool("yes"); err == nil {
			yes = v
		}
		if len(args) > 0 && !removeConfirmed(cmd, yes, question(args)) {
			return withCode(exitFailure, errNotRemoved)
		}
		return run(cmd, args)
	}
	return c
}

// removeQuestion is the question for removing what a kind's key names.
func removeQuestion(key string) func(args []string) string {
	return func(args []string) string { return i18n.T(key, "name", args[0]) }
}
