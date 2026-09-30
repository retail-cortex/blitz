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
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// A workspace's project settings (.blitz/settings.toml) and the trust
// they need, as the REPL, blitz trust and doctor show them
// (spec_project_config_031).

// ProjectItem is one project setting in words: "hook stop runs
// ./scripts/stop.sh".
func ProjectItem(it api.ProjectItem) string {
	kind := it.Kind
	switch kind {
	case "limit", "skill_policy", "worker_limit":
		kind = "limit"
	case "hook", "mcp", "allow", "writable", "model", "agent_model", "worker_allow", "skill_scripts",
		"deny", "ask", "blocked_path":
	default:
		kind = "setting"
	}
	return i18n.T("project.item."+kind, "key", it.Key, "value", it.Value, "file", it.File)
}

// NeedsTrustDecision reports whether p's settings wait for a first or
// new decision.
func NeedsTrustDecision(p api.ProjectSettings) bool {
	return !p.Loaded && (p.State == api.TrustNew || p.State == api.TrustChanged)
}

// ShowProject writes p: its files, what is in force, what needs trust and
// its state, and what was ignored.
func ShowProject(w io.Writer, p api.ProjectSettings) {
	if len(p.Files) == 0 && len(p.Pending) == 0 && len(p.Problems) == 0 {
		fmt.Fprintf(w, "%s%s%s\n", Dim, i18n.T("project.none"), Reset)
		return
	}
	fmt.Fprintf(w, "%s%s%s\n", Bold, i18n.T("project.title", "files", strings.Join(p.Files, ", ")), Reset)
	list := func(title string, items []api.ProjectItem, withReason bool) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(w, "  %s\n", title)
		for _, it := range items {
			line := ProjectItem(it)
			if withReason && it.Reason != "" {
				line += Dim + " (" + ProjectReason(it.Reason) + ")" + Reset
			}
			fmt.Fprintf(w, "    %s\n", safe(line))
		}
	}
	list(i18n.T("project.applied"), p.Applied, false)
	list(i18n.T("project.pending"), p.Pending, false)
	if len(p.Pending) > 0 {
		fmt.Fprintf(w, "  %s%s%s\n", Cyan, projectState(p), Reset)
	}
	list(i18n.T("project.ignored"), p.Ignored, true)
	for _, prob := range p.Problems {
		fmt.Fprintf(w, "  %s%s%s\n", Yellow, safe(prob), Reset)
	}
}

// ProjectReason is why a project setting was ignored, in words.
func ProjectReason(reason string) string { return i18n.T("project.reason." + reason) }

func projectState(p api.ProjectSettings) string {
	switch {
	case p.Loaded && p.State != api.TrustTrusted:
		return i18n.T("project.state.for_run")
	case p.State == api.TrustTrusted && !p.Loaded:
		return i18n.T("project.state.trusted_next_open")
	}
	return i18n.T("project.state." + p.State)
}

// AskTrust shows what p's settings would do and asks whether to trust
// them: "trust", "decline", or "" when the question went unanswered
// (Ctrl+C, end of input). [s] shows the files first.
func AskTrust(ctx context.Context, in Input, w io.Writer, dir string, p api.ProjectSettings) string {
	fmt.Fprintf(w, "\n%s!  %s%s\n", Yellow+Bold, i18n.T("project.wants", "files", strings.Join(p.Files, ", ")), Reset)
	for _, it := range p.Pending {
		fmt.Fprintf(w, "     %s\n", safe(ProjectItem(it)))
	}
	for {
		answer, err := in.Ask(ctx, "   "+i18n.T("project.ask")+" ")
		if err != nil {
			return ""
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "t", "trust":
			return "trust"
		case "d", "decline":
			return "decline"
		case "s", "show":
			for _, f := range p.Files {
				data, err := os.ReadFile(filepath.Join(dir, f))
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "\n%s── %s%s\n%s\n", Dim, f, Reset, safe(string(data)))
			}
		}
	}
}

// ProjectTruster is where project settings are read and trusted: a
// workspace (api.Backend), or blitz trust's.
type ProjectTruster interface {
	Dir() string
	ProjectSettings() api.ProjectSettings
	TrustProject(hash string, trusted bool) error
}

// DecideTrust asks about p and records the answer through b, and says
// what came of it.
func DecideTrust(ctx context.Context, in Input, w io.Writer, b ProjectTruster, p api.ProjectSettings) {
	switch AskTrust(ctx, in, w, b.Dir(), p) {
	case "trust":
		if err := b.TrustProject(p.Hash, true); err != nil {
			fmt.Fprintf(w, "%s%s%s\n", Red, trustError(err), Reset)
			return
		}
		if b.ProjectSettings().Loaded {
			fmt.Fprintf(w, "%s%s%s\n", Green, i18n.T("project.trusted_now"), Reset)
		} else {
			fmt.Fprintf(w, "%s%s%s\n", Green, i18n.T("project.trusted_next_open"), Reset)
		}
	case "decline":
		if err := b.TrustProject(p.Hash, false); err != nil {
			fmt.Fprintf(w, "%s%s%s\n", Red, trustError(err), Reset)
			return
		}
		fmt.Fprintf(w, "%s%s%s\n", Dim, i18n.T("project.declined_now"), Reset)
	}
}

func trustError(err error) string {
	if strings.Contains(err.Error(), api.ErrProjectChanged.Error()) {
		return i18n.T("project.changed_since")
	}
	return err.Error()
}

// ProjectNotice is the line the REPL prints at start about the project's
// settings, or "": what applies, and what waits for trust.
func ProjectNotice(p api.ProjectSettings) string {
	waiting := 0
	if !p.Loaded {
		waiting = len(p.Pending)
	}
	switch {
	case len(p.Applied) == 0 && waiting == 0:
		return ""
	case waiting == 0:
		return i18n.N("project.notice_applied", len(p.Applied))
	}
	return i18n.N("project.notice_waiting", waiting, "applied", len(p.Applied))
}
