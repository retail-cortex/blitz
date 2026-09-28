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

// Command coverage summarizes the Go tests' coverage from the LCOV report
// `bazel coverage --combined_report=lcov` writes: the total and each
// package's and file's share of lines covered. It writes the summary as
// JSON for the docs site's coverage page (--json), as Markdown for CI's
// job summary (--markdown), and fails when the total is below the floor
// (--floor), so coverage can only go up. tools/coverage.sh runs it.
//
//	bazel run //tools/coverage -- --lcov=FILE [--json=FILE] [--markdown=FILE] [--floor=FILE]
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Counts is how many lines tests ran of those that could run.
type Counts struct {
	Lines   int     `json:"lines"`
	Covered int     `json:"covered"`
	Percent float64 `json:"percent"`
}

func (c *Counts) add(lines, covered int) {
	c.Lines += lines
	c.Covered += covered
	c.Percent = percent(c.Covered, c.Lines)
}

// File is one source file's coverage.
type File struct {
	Name string `json:"name"`
	Counts
}

// Package is one Go package's coverage: its directory, and its files.
type Package struct {
	Name string `json:"name"`
	Counts
	Files []File `json:"files"`
}

// Report is the whole summary: the total, then the packages from the least
// covered up.
type Report struct {
	Commit   string    `json:"commit,omitempty"`
	Date     string    `json:"date,omitempty"`
	Floor    float64   `json:"floor"`
	Total    Counts    `json:"total"`
	Packages []Package `json:"packages"`
}

func main() {
	lcov := flag.String("lcov", "bazel-out/_coverage/_coverage_report.dat", "the combined LCOV report, relative to the repository")
	jsonOut := flag.String("json", "", "write the summary as JSON to this file")
	mdOut := flag.String("markdown", "", "append the summary as Markdown to this file (CI's $GITHUB_STEP_SUMMARY)")
	floorFile := flag.String("floor", "tools/coverage/floor.txt", "fail when the total is below the percentage in this file (empty: don't check)")
	commit := flag.String("commit", "", "the commit measured, for the report")
	date := flag.String("date", "", "when it was measured, for the report")
	flag.Parse()
	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
		if err := os.Chdir(ws); err != nil {
			fail(err)
		}
	}

	f, err := os.Open(*lcov)
	if err != nil {
		fail(fmt.Errorf("%w (run tools/coverage.sh)", err))
	}
	r, err := parse(f)
	f.Close()
	if err != nil {
		fail(fmt.Errorf("%s: %w", *lcov, err))
	}
	r.Commit, r.Date = *commit, *date
	if *floorFile != "" {
		if r.Floor, err = readFloor(*floorFile); err != nil {
			fail(err)
		}
	}

	if *jsonOut != "" {
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			fail(err)
		}
		if err := os.MkdirAll(filepath.Dir(*jsonOut), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(*jsonOut, append(data, '\n'), 0o644); err != nil {
			fail(err)
		}
	}
	if *mdOut != "" {
		out, err := os.OpenFile(*mdOut, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fail(err)
		}
		writeMarkdown(out, r)
		if err := out.Close(); err != nil {
			fail(err)
		}
	}

	fmt.Printf("coverage: %.1f%% of %d lines in %d packages (floor %.1f%%)\n", r.Total.Percent, r.Total.Lines, len(r.Packages), r.Floor)
	if r.Total.Percent < r.Floor {
		fail(fmt.Errorf("coverage %.1f%% is below the floor of %.1f%% (%s): add tests", r.Total.Percent, r.Floor, *floorFile))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "✗", err)
	os.Exit(1)
}

// parse reads an LCOV report. A file's lines are its DA records (line,
// hits); a file listed more than once, by several tests, counts a line as
// covered if any test ran it.
func parse(r io.Reader) (Report, error) {
	hits := map[string]map[int]bool{} // file -> line -> covered
	var file string
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024*1024), 1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			file = strings.TrimPrefix(line, "SF:")
			if hits[file] == nil {
				hits[file] = map[int]bool{}
			}
		case strings.HasPrefix(line, "DA:"):
			if file == "" {
				return Report{}, fmt.Errorf("%q outside a file record", line)
			}
			fields := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
			if len(fields) < 2 {
				return Report{}, fmt.Errorf("bad record %q", line)
			}
			n, err1 := strconv.Atoi(fields[0])
			count, err2 := strconv.Atoi(fields[1])
			if err1 != nil || err2 != nil {
				return Report{}, fmt.Errorf("bad record %q", line)
			}
			hits[file][n] = hits[file][n] || count > 0
		case line == "end_of_record":
			file = ""
		}
	}
	if err := s.Err(); err != nil {
		return Report{}, err
	}

	var rep Report
	pkgs := map[string]*Package{}
	for name, lines := range hits {
		if len(lines) == 0 {
			continue
		}
		covered := 0
		for _, c := range lines {
			if c {
				covered++
			}
		}
		dir := path.Dir(name)
		p := pkgs[dir]
		if p == nil {
			p = &Package{Name: dir}
			pkgs[dir] = p
		}
		fc := File{Name: path.Base(name)}
		fc.add(len(lines), covered)
		p.Files = append(p.Files, fc)
		p.add(len(lines), covered)
		rep.Total.add(len(lines), covered)
	}
	if len(pkgs) == 0 {
		return Report{}, fmt.Errorf("no coverage data")
	}
	for _, p := range pkgs {
		sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Name < p.Files[j].Name })
		rep.Packages = append(rep.Packages, *p)
	}
	sort.Slice(rep.Packages, func(i, j int) bool {
		a, b := rep.Packages[i], rep.Packages[j]
		if a.Percent != b.Percent {
			return a.Percent < b.Percent
		}
		return a.Name < b.Name
	})
	return rep, nil
}

// percent is covered/lines as a percentage, to one decimal place.
func percent(covered, lines int) float64 {
	if lines == 0 {
		return 0
	}
	return float64(int(float64(covered)*1000/float64(lines))) / 10
}

func readFloor(file string) (float64, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %q isn't a percentage", file, line)
		}
		return v, nil
	}
	return 0, fmt.Errorf("%s: no percentage", file)
}

// writeMarkdown writes the total and the package table, least covered
// first.
func writeMarkdown(w io.Writer, r Report) {
	fmt.Fprintf(w, "### Coverage: %.1f%%\n\n", r.Total.Percent)
	fmt.Fprintf(w, "%d of %d lines in `apps/` and `pkg/` run by the Go tests; the floor is %.1f%%.\n\n", r.Total.Covered, r.Total.Lines, r.Floor)
	fmt.Fprintln(w, "| Package | Coverage | Lines |")
	fmt.Fprintln(w, "|---|---:|---:|")
	for _, p := range r.Packages {
		fmt.Fprintf(w, "| `%s` | %.1f%% | %d / %d |\n", p.Name, p.Percent, p.Covered, p.Lines)
	}
	fmt.Fprintln(w)
}
