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

package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// cmdData is /data: the workspace's databases and their saved queries
// (spec_059_data DAT-45).
func cmdData(ctx context.Context, app *App) {
	conns, err := app.Workspace.DataConnections(ctx)
	if err != nil {
		fmt.Printf("%s✗ %s%s\n", Red, safe(err.Error()), Reset)
		return
	}
	fmt.Println()
	PrintDataConnections(os.Stdout, conns)
	fmt.Println()
}

// PrintDataConnections prints conns, each with what keeps it from a chat
// (off, not trusted, a problem) and its saved queries, then how to use
// them (/data, blitz data list).
func PrintDataConnections(out io.Writer, conns []api.DataConnection) {
	if len(conns) == 0 {
		fmt.Fprintln(out, i18n.T("data.none"))
		return
	}
	fmt.Fprintf(out, "%s%s%s\n", Bold, i18n.T("data.title"), Reset)
	for _, c := range conns {
		var notes []string
		if c.Kind != "" {
			notes = append(notes, c.Kind)
		}
		if c.ReadOnly {
			notes = append(notes, i18n.T("data.read_only"))
		}
		switch {
		case c.Problem != "":
			notes = append(notes, i18n.T("data.problem", "problem", safe(c.Problem)))
		case !c.On:
			notes = append(notes, i18n.T("data.off"))
		case !c.Trusted:
			notes = append(notes, i18n.T("data.untrusted"))
		}
		fmt.Fprintf(out, "  • %s@db:%s%s  %s%s%s\n", Bold, safe(c.Name), Reset, Dim, strings.Join(notes, " · "), Reset)
		for _, q := range c.Queries {
			line := "/" + safe(q.Name)
			if q.Description != "" {
				line += ": " + safe(q.Description)
			}
			if len(q.Params) > 0 {
				line += " " + Dim + i18n.T("data.params", "params", safe(strings.Join(q.Params, ", "))) + Reset
			}
			fmt.Fprintf(out, "      %s\n", line)
		}
	}
	fmt.Fprintf(out, "%s%s%s\n", Dim, i18n.T("data.hint"), Reset)
}

// DataMentions are the @db: mentions conns offer, without the @ (DAT-40):
// each one a chat may use, and its saved queries that take no
// parameters (a mention can't fill them).
func DataMentions(conns []api.DataConnection) []string {
	var out []string
	for _, c := range conns {
		if !c.Usable() {
			continue
		}
		out = append(out, "db:"+c.Name)
		for _, q := range c.Queries {
			if len(q.Params) == 0 {
				out = append(out, "db:"+c.Name+"/"+q.Name)
			}
		}
	}
	return out
}
