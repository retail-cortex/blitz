package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	globDefaultResults = 200
	globMaxResults     = 1000
	// globMaxAlternatives bounds brace expansion ({a,b}{c,d}...).
	globMaxAlternatives = 64
)

// GlobInput defines arguments for glob.
type GlobInput struct {
	Pattern    string `json:"pattern" jsonschema:"Glob relative to path, e.g. **/*.go or src/**/*.{ts,tsx}. * and ? stay within one path segment; ** crosses segments"`
	Path       string `json:"path,omitempty" jsonschema:"Directory to search from (default: the workspace)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum files to return (default 200, max 1000)"`
}

// GlobOutput holds the matching files, newest first.
type GlobOutput struct {
	Files     []string `json:"files"`
	Count     int      `json:"count"`
	Truncated bool     `json:"truncated,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// NewGlobTool creates glob: find files by name pattern.
func NewGlobTool(ws *Workspace) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "glob",
			Description: "Find files whose paths match a glob pattern (supports **, *, ?, {a,b}), newest first",
		},
		func(ctx agent.Context, input GlobInput) (GlobOutput, error) {
			out, err := globWorkspace(ctx, ws, input)
			if err != nil {
				return GlobOutput{Files: []string{}, Error: err.Error()}, nil
			}
			return out, nil
		},
	)
}

type globHit struct {
	path string
	mod  time.Time
}

func globWorkspace(ctx interface{ Err() error }, ws *Workspace, in GlobInput) (GlobOutput, error) {
	pattern := strings.TrimSpace(strings.ReplaceAll(in.Pattern, `\`, "/"))
	if pattern == "" {
		return GlobOutput{}, errors.New("pattern must not be empty")
	}
	if strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "~") {
		return GlobOutput{}, errors.New("pattern is relative to path; pass the directory as path")
	}
	pattern = strings.TrimPrefix(pattern, "./")
	if pattern == ".." || strings.HasPrefix(pattern, "../") || strings.Contains(pattern, "/../") {
		return GlobOutput{}, errors.New(`pattern can't contain ".."; pass the directory as path`)
	}
	max := in.MaxResults
	if max <= 0 {
		max = globDefaultResults
	}
	max = min(max, globMaxResults)

	alts, err := expandBraces(pattern)
	if err != nil {
		return GlobOutput{}, err
	}
	var res []*regexp.Regexp
	for _, a := range alts {
		expr := "^" + globToRegex(a) + "$"
		if caseInsensitiveFS {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return GlobOutput{}, fmt.Errorf("invalid pattern: %v", err)
		}
		res = append(res, re)
	}
	matches := func(rel string) bool {
		for _, re := range res {
			if re.MatchString(rel) {
				return true
			}
		}
		return false
	}

	start := in.Path
	if start == "" {
		start = "."
	}
	base, err := ws.Rel(start)
	if err != nil {
		return GlobOutput{}, err
	}
	var hits []globHit
	walkErr := ws.Walk(base, func(e WalkEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.IsRoot {
			return nil
		}
		rel := relTo(base, e.Path)
		d := e.Entry
		if d.IsDir() {
			// Dot and dependency directories are skipped, as grep does,
			// unless the pattern names them.
			name := d.Name()
			if (strings.HasPrefix(name, ".") || grepSkipDirs[name]) && !patternNames(alts, name) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !matches(rel) {
			return nil
		}
		var mod time.Time
		if info, err := d.Info(); err == nil {
			mod = info.ModTime()
		}
		hits = append(hits, globHit{path: e.Path, mod: mod})
		return nil
	})
	if walkErr != nil {
		return GlobOutput{}, fmt.Errorf("search error: %v", walkErr)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if !hits[i].mod.Equal(hits[j].mod) {
			return hits[i].mod.After(hits[j].mod)
		}
		return hits[i].path < hits[j].path
	})
	out := GlobOutput{Files: []string{}}
	for i, h := range hits {
		if i == max {
			out.Truncated = true
			break
		}
		out.Files = append(out.Files, h.path)
	}
	out.Count = len(out.Files)
	return out, nil
}

// relTo returns p (a display path) relative to base (another display
// path), slash-separated, for matching.
func relTo(base, p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	base = strings.ReplaceAll(base, `\`, "/")
	if base == "." || base == "" {
		return p
	}
	return strings.TrimPrefix(strings.TrimPrefix(p, base), "/")
}

// patternNames reports whether a pattern names dir as a path segment:
// literally (".github/**"), or with a wildcard segment that itself starts
// with a dot (".git*"). "*" and "**" never name a dot or dependency
// directory.
func patternNames(alts []string, dir string) bool {
	for _, a := range alts {
		for seg := range strings.SplitSeq(a, "/") {
			if seg == dir {
				return true
			}
			if strings.HasPrefix(seg, ".") && strings.ContainsAny(seg, "*?[") {
				if ok, _ := path.Match(seg, dir); ok {
					return true
				}
			}
		}
	}
	return false
}

// expandBraces expands {a,b} alternatives (nested allowed) into plain
// patterns.
func expandBraces(p string) ([]string, error) {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}, nil
	}
	depth, end := 0, -1
	for i := open; i < len(p) && end < 0; i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
	}
	if end < 0 {
		return nil, errors.New("invalid pattern: unmatched {")
	}
	var parts []string
	depth, last := 0, open+1
	for i := open + 1; i < end; i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, p[last:i])
				last = i + 1
			}
		}
	}
	parts = append(parts, p[last:end])
	var out []string
	for _, part := range parts {
		rest, err := expandBraces(p[:open] + part + p[end+1:])
		if err != nil {
			return nil, err
		}
		out = append(out, rest...)
		if len(out) > globMaxAlternatives {
			return nil, fmt.Errorf("invalid pattern: more than %d alternatives", globMaxAlternatives)
		}
	}
	return out, nil
}
