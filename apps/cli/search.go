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
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
)

// searchWait is how long blitz search waits for a workspace just opened
// to be indexed.
var searchWait = 2 * time.Minute

// searchOptions are blitz search's flags.
type searchOptions struct {
	in      []string
	mode    string
	hidden  bool
	limit   int
	json    bool
	status  bool
	reindex bool
}

func newSearchCommand(g *globalFlags) *cobra.Command {
	var o searchOptions
	cmd := &cobra.Command{
		Use:   "search <terms>",
		Short: "Search the workspace: its files, PDFs and notebooks, chats and notes",
		Long: `Search the workspace's index by words and "quoted phrases", ranked by
relevance: its files (as git sees them, blocked paths left out), the text of
its PDFs and notebooks, its chats and its notes. By default it searches what
search.sources names (files and documents).`,
		RunE: func(cmd *cobra.Command, args []string) error { return runSearch(cmd, g, o, strings.Join(args, " ")) },
	}
	f := cmd.Flags()
	f.StringSliceVar(&o.in, "in", nil, "Where to search: files, documents, chats, notes or all (comma-separated)")
	f.IntVarP(&o.limit, "limit", "n", 0, "Most results (default 20)")
	f.StringVar(&o.mode, "mode", "", "keyword, semantic or hybrid (default hybrid with search.embedding_model, else keyword)")
	f.BoolVar(&o.hidden, "hidden", false, "Include hidden files: dotfiles, and files git ignores (search.include_ignored)")
	f.BoolVar(&o.json, "json", false, "Print the results as JSON")
	f.BoolVar(&o.status, "status", false, "Show the index's status instead of searching")
	f.BoolVar(&o.reindex, "reindex", false, "Scan the workspace again now")
	return cmd
}

// searchJSON is one hit as blitz search --json prints it.
type searchJSON struct {
	Source  string   `json:"source"`
	Ref     string   `json:"ref"`
	Title   string   `json:"title,omitempty"`
	Line    int      `json:"line,omitempty"`
	Section string   `json:"section,omitempty"`
	Snippet string   `json:"snippet"`
	Score   float64  `json:"score"`
	Summary string   `json:"summary,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

func runSearch(cmd *cobra.Command, g *globalFlags, o searchOptions, terms string) error {
	if strings.TrimSpace(terms) == "" && !o.status && !o.reindex {
		return withCode(exitUsage, api.ErrEmptySearch)
	}
	var sources []string
	for _, in := range o.in {
		s, ok := tui.WorkspaceSearchSources[strings.TrimSpace(in)]
		if !ok {
			return withCode(exitUsage, fmt.Errorf("%w: %q", api.ErrUnknownSearchSource, in))
		}
		sources = append(sources, s...)
	}
	cfg, err := loadConfig(g)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	warn := func(s string) { fmt.Fprintln(cmd.ErrOrStderr(), s) }
	opened := time.Now()
	b, _, remote, err := openBackend(ctx, cfg, backendOptions{trustProject: g.trustProject, flags: g}, warn)
	if err != nil {
		return err
	}
	defer b.Close()
	switch {
	case o.reindex:
		if err := b.Reindex(); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.T("search.ws_reindex"))
		if terms == "" {
			return nil
		}
	case o.status:
		tui.PrintSearchStatus(out, b.SearchStatus())
		return nil
	}
	// Opened here, the index is brought up to date first; the service's
	// is kept current.
	since := opened
	if remote {
		since = time.Time{}
	}
	if err := tui.WaitIndexed(ctx, b, since, searchWait); err != nil {
		return err
	}
	res, err := b.Search(ctx, api.SearchQuery{Text: terms, Sources: sources, Limit: o.limit, Mode: o.mode, Hidden: o.hidden})
	if err != nil {
		return err
	}
	if o.json {
		hits := make([]searchJSON, len(res.Hits))
		for i, h := range res.Hits {
			hits[i] = searchJSON(h)
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(hits)
	}
	if len(res.Hits) == 0 {
		fmt.Fprintln(out, i18n.T("search.ws_none", "query", terms))
		return nil
	}
	var b2 strings.Builder
	tui.PrintSearchHits(&b2, res.Hits)
	text := b2.String()
	if !stdoutIsTerminal() {
		text = ansiRE.ReplaceAllString(text, "")
	}
	_, err = io.WriteString(out, text)
	return err
}

// ansiRE matches the terminal's color codes, left out when the output
// isn't a terminal.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)
