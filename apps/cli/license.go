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
	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/spf13/cobra"
)

// newLicenseCommand is `blitz license [full|third-party]`: the NOTICE,
// the Apache License, or the third-party notices, as /license shows them.
func newLicenseCommand() *cobra.Command {
	return &cobra.Command{
		Use:       "license [full|third-party]",
		Short:     "Show Blitz's license and the third-party notices",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"full", "third-party"},
		RunE: func(cmd *cobra.Command, args []string) error {
			which := ""
			if len(args) == 1 {
				which = args[0]
			}
			text, err := tui.LicenseText(which, "blitz license")
			if err != nil {
				return withCode(exitUsage, err)
			}
			return tui.Page(cmd.OutOrStdout(), text)
		},
	}
}
