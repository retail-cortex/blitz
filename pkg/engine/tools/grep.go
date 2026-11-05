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

package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"runtime"
	"strings"

	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	grepMaxFileSize    = 5 * 1024 * 1024 // files larger than this are skipped
	grepBinarySniffLen = 8000            // bytes inspected for NUL to detect binaries
	grepMaxLineContent = 500             // bytes of a matching line returned to the model
)

// grepSkipDirs are directory names never descended into (in addition to dot-dirs).
var grepSkipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"__pycache__":  true,
}

// GrepMatch holds a single search match.
type GrepMatch struct {
	File       string `json:"file"`
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
}

// GrepInput defines arguments for grep search.
type GrepInput struct {
	Query           string `json:"query" jsonschema:"The search term or regex pattern"`
	Path            string `json:"path,omitempty" jsonschema:"Optional directory or file path to search in"`
	IsRegex         bool   `json:"is_regex,omitempty" jsonschema:"Whether query is a regex pattern"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty" jsonschema:"Whether search is case insensitive"`
	MaxMatches      int    `json:"max_matches,omitempty" jsonschema:"Max results to return"`
}

// GrepOutput holds grep search results.
type GrepOutput struct {
	Matches      []GrepMatch `json:"matches"`
	TotalMatches int         `json:"total_matches"`
	// Truncated says the search stopped at max_matches: there may be more.
	Truncated bool `json:"truncated,omitempty"`
	// Note says what wasn't searched (files over grep's size limit).
	Note  string `json:"note,omitempty"`
	Error string `json:"error,omitempty"`
}

// NewGrepTool creates an ADK tool for workspace grep searching.
func NewGrepTool(ws *Workspace) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "grep",
			Description: "Search for pattern or literal string across workspace files",
		},
		func(ctx agent.Context, input GrepInput) (GrepOutput, error) {
			res, err := grepSearch(ctx, ws, input)
			if err != nil {
				return GrepOutput{Error: err.Error()}, nil
			}
			out := GrepOutput{Matches: res.matches, TotalMatches: len(res.matches), Truncated: res.truncated}
			if res.tooBig > 0 {
				out.Note = fmt.Sprintf("%d file(s) over %d MiB weren't searched; use run_shell_command with grep or rg for them", res.tooBig, grepMaxFileSize>>20)
			}
			return out, nil
		},
	)
}

// lineMatcher reports whether a line matches, with an optional whole-file
// prefilter that lets files without any match be skipped cheaply.
type lineMatcher struct {
	match     func(line []byte) bool
	prefilter func(data []byte) bool
}

func newLineMatcher(input GrepInput) (*lineMatcher, error) {
	if !input.IsRegex && !input.CaseInsensitive {
		needle := []byte(input.Query)
		contains := func(b []byte) bool { return bytes.Contains(b, needle) }
		return &lineMatcher{match: contains, prefilter: contains}, nil
	}

	pattern := input.Query
	if !input.IsRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if input.CaseInsensitive {
		// Must be added after quoting, otherwise the flag itself gets escaped.
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %v", err)
	}
	m := &lineMatcher{match: re.Match}
	if !input.IsRegex {
		// Literal patterns have no anchors, so a whole-file check is equivalent.
		m.prefilter = re.Match
	}
	return m, nil
}

type grepFileResult struct {
	matches []GrepMatch
	tooBig  bool // over grepMaxFileSize: not searched
}

// grepResult is a search's matches, whether it stopped at its maximum,
// and how many files were too big to search.
type grepResult struct {
	matches   []GrepMatch
	truncated bool
	tooBig    int
}

// grepWorkspace is grepSearch's matches.
func grepWorkspace(ctx context.Context, ws *Workspace, input GrepInput) ([]GrepMatch, error) {
	res, err := grepSearch(ctx, ws, input)
	return res.matches, err
}

func grepSearch(ctx context.Context, ws *Workspace, input GrepInput) (grepResult, error) {
	if input.Query == "" {
		return grepResult{}, fmt.Errorf("query must not be empty")
	}
	searchPath := input.Path
	if searchPath == "" {
		searchPath = "."
	}
	max := input.MaxMatches
	if max <= 0 {
		max = 100
	}
	max = min(max, 500)
	matcher, err := newLineMatcher(input)
	if err != nil {
		return grepResult{}, err
	}

	// Collect candidate files first; walking is cheap relative to reading.
	// Blocked paths are filtered out by Walk.
	var files []WalkEntry
	walkErr := ws.Walk(searchPath, func(e WalkEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d := e.Entry
		if d.IsDir() {
			// Never skip the starting directory itself (its name may be ".").
			if !e.IsRoot && (strings.HasPrefix(d.Name(), ".") || grepSkipDirs[d.Name()]) {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, e)
		}
		return nil
	})
	if walkErr != nil {
		return grepResult{}, fmt.Errorf("search error: %v", walkErr)
	}

	// Scan files in parallel, but consume results in walk order so the first
	// max matches are deterministic. The producer hands per-file result
	// channels to the consumer through a bounded queue, so at most a small
	// window of files is in flight regardless of repository size. Result
	// channels are buffered so workers never block; returning early cancels
	// scanCtx, which unblocks the producer and stops outstanding workers.
	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	workers := min(runtime.GOMAXPROCS(0), 8)
	sem := make(chan struct{}, workers)
	order := make(chan chan grepFileResult, workers*2)

	go func() {
		defer close(order)
		for _, entry := range files {
			ch := make(chan grepFileResult, 1)
			select {
			case order <- ch:
			case <-scanCtx.Done():
				return
			}
			select {
			case sem <- struct{}{}:
			case <-scanCtx.Done():
				close(ch)
				return
			}
			go func(entry WalkEntry) {
				defer func() { <-sem }()
				defer close(ch)
				if scanCtx.Err() != nil {
					return
				}
				matches, tooBig := grepFile(scanCtx, entry, matcher, max)
				ch <- grepFileResult{matches: matches, tooBig: tooBig}
			}(entry)
		}
	}()

	out := grepResult{matches: make([]GrepMatch, 0, 16)}
	for ch := range order {
		res, ok := <-ch
		if !ok {
			if ctx.Err() != nil {
				return grepResult{}, ctx.Err()
			}
			continue
		}
		if res.tooBig {
			out.tooBig++
		}
		for _, m := range res.matches {
			out.matches = append(out.matches, m)
			if len(out.matches) >= max {
				out.truncated = true
				return out, nil
			}
		}
	}
	return out, nil
}

// grepFile returns up to max matches from a single file, and whether it
// was too big to search. Unreadable, oversized, and binary files yield no
// matches.
func grepFile(ctx context.Context, entry WalkEntry, m *lineMatcher, max int) ([]GrepMatch, bool) {
	f, err := entry.Open()
	if err != nil {
		return nil, false
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, false
	}
	if info.Size() > grepMaxFileSize {
		return nil, true
	}
	// Reading the whole (bounded) file avoids bufio.Scanner's line-length limit.
	data, err := io.ReadAll(io.LimitReader(f, grepMaxFileSize))
	if err != nil {
		return nil, false
	}
	if bytes.IndexByte(data[:min(len(data), grepBinarySniffLen)], 0) >= 0 {
		return nil, false
	}
	if m.prefilter != nil && !m.prefilter(data) {
		return nil, false
	}

	var out []GrepMatch
	lineNum := 0
	for len(data) > 0 {
		lineNum++
		if lineNum%4096 == 0 && ctx.Err() != nil {
			return out, false
		}
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		if m.match(line) {
			out = append(out, GrepMatch{
				File:       entry.Path,
				LineNumber: lineNum,
				Content:    textutil.Ellipsize(strings.TrimSpace(string(line)), grepMaxLineContent),
			})
			if len(out) >= max {
				return out, false
			}
		}
	}
	return out, false
}
