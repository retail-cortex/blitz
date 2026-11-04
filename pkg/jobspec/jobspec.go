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

// Package jobspec reads what a job's WORKER.md says about when and where
// it runs: its schedule (plain text or cron, in a time zone), where it runs
// (here, on the computer, or on the organization's service), what to do
// about missed times and its time limit. Blitz's workers and the
// organization's service read jobs with it alike.
package jobspec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileName is the file that defines a job, in its own directory.
const FileName = "WORKER.md"

// Where a job runs (spec_053_enterprise EN-40).
const (
	// Here is on the person's computer, while Blitz's service runs.
	Here = "here"
	// Service is on the organization's service, at every time.
	Service = "service"
)

// Spec is when and where a job runs.
type Spec struct {
	Description string
	// Schedule is as written; empty for a job run only on request.
	Schedule, Timezone string
	// RunsOn is Here or Service.
	RunsOn string
	// CatchUp is "none" or "once".
	CatchUp string
	// Timeout is a run's time limit (0: the runner's default).
	Timeout time.Duration
	// Merge is where a run on the service leaves its changes: MergeMain or
	// MergeNever.
	Merge string
}

// Where a run on the service leaves its changes (blitz-enterprise
// spec_009_jobs JB-10): each goes to its own branch first.
const (
	// MergeMain adds them to the workspace's main too when they merge
	// cleanly: the default.
	MergeMain = "main"
	// MergeNever keeps them on the run's branch, for someone to add.
	MergeNever = "never"
)

// MergeOf is what merge says: MergeMain when unset.
func MergeOf(s string) (string, error) {
	switch s {
	case "", MergeMain:
		return MergeMain, nil
	case MergeNever:
		return MergeNever, nil
	}
	return "", fmt.Errorf("merge %q: use \"main\" or \"never\"", s)
}

// Parse reads a WORKER.md's frontmatter, ignoring what it doesn't need,
// and checks what it reads: a schedule it can't understand, an unknown
// place to run, catch-up or timeout is an error.
func Parse(data []byte) (Spec, error) {
	fm, _, err := Split(data)
	if err != nil {
		return Spec{}, err
	}
	var f struct {
		Description string `yaml:"description"`
		Schedule    string `yaml:"schedule"`
		Timezone    string `yaml:"timezone"`
		RunsOn      string `yaml:"runs_on"`
		CatchUp     string `yaml:"catch_up"`
		Merge       string `yaml:"merge"`
		Limits      struct {
			Timeout string `yaml:"timeout"`
		} `yaml:"limits"`
	}
	if err := yaml.Unmarshal(fm, &f); err != nil && !errors.Is(err, io.EOF) {
		return Spec{}, err
	}
	s := Spec{Description: strings.TrimSpace(f.Description), Schedule: strings.TrimSpace(f.Schedule), Timezone: f.Timezone,
		RunsOn: f.RunsOn, CatchUp: f.CatchUp}
	if s.RunsOn, err = RunsOn(f.RunsOn); err != nil {
		return s, err
	}
	if s.Merge, err = MergeOf(f.Merge); err != nil {
		return s, err
	}
	switch s.CatchUp {
	case "":
		s.CatchUp = "none"
	case "none", "once":
	default:
		return s, fmt.Errorf("catch_up %q: use \"none\" or \"once\"", s.CatchUp)
	}
	if f.Limits.Timeout != "" {
		if s.Timeout, err = time.ParseDuration(f.Limits.Timeout); err != nil || s.Timeout <= 0 {
			return s, fmt.Errorf("limits.timeout %q isn't a duration like 20m", f.Limits.Timeout)
		}
	}
	if s.Schedule != "" {
		if _, err := ParseSchedule(s.Schedule, s.Timezone); err != nil {
			return s, err
		}
	}
	return s, nil
}

// RunsOn is where runs_on says a job runs: Here when unset.
func RunsOn(s string) (string, error) {
	switch s {
	case "", Here:
		return Here, nil
	case Service:
		return Service, nil
	}
	return "", fmt.Errorf("runs_on %q: use %q or %q", s, Here, Service)
}

// Split is a WORKER.md's frontmatter (between lines of ---) and its body.
func Split(data []byte) (fm, body []byte, err error) {
	s := bytes.TrimLeft(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}), " \t\r\n") // a byte-order mark, then blank lines
	if !bytes.HasPrefix(s, []byte("---")) {
		return nil, nil, errors.New("missing frontmatter: start with a line of ---")
	}
	s = s[3:]
	end := bytes.Index(s, []byte("\n---"))
	if end < 0 {
		return nil, nil, errors.New("missing the --- line that ends the frontmatter")
	}
	body = s[end+4:]
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = nil
	}
	return s[:end], body, nil
}
