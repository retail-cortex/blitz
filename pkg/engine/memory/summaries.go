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

package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Session summaries are where each earlier session of a workspace left off
// (spec_memory_012 MEM-35): what it was for, what was done and decided,
// what's open and what comes next, one Markdown file per session in the
// workspace's notes folder (sessions/<id>.md). Kept beside the notes, not in
// the project, since a conversation is the person's own.

// Summary is one session's summary.
type Summary struct {
	Session  string    // the session's ID
	Title    string    // the session's title when it was written
	Messages int       // how many of the session's messages it covers
	Updated  time.Time // when the last of those was written
	Text     string    // the summary, Markdown
	Path     string
}

// Stands is the summary's "Where it stands" line: its first paragraph of
// prose, for a list of sessions.
func (s Summary) Stands() string {
	for _, para := range strings.Split(s.Text, "\n\n") {
		var lines []string
		for _, l := range strings.Split(para, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				lines = append(lines, strings.TrimLeft(l, "-* "))
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, " ")
		}
	}
	return ""
}

// SummariesDir is where the summaries are kept in a workspace's notes
// folder (NotesDir).
func SummariesDir(notesDir string) string { return filepath.Join(notesDir, "sessions") }

var sessionIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// SaveSummary writes s to dir, replacing the session's earlier summary.
func SaveSummary(dir string, s Summary) (Summary, error) {
	if !sessionIDRE.MatchString(s.Session) {
		return Summary{}, fmt.Errorf("not a session ID: %q", s.Session)
	}
	s.Text = strings.TrimSpace(s.Text)
	if s.Text == "" {
		return Summary{}, errors.New("the summary is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Summary{}, err
	}
	title := strings.Join(strings.Fields(s.Title), " ")
	body := fmt.Sprintf("---\nsession: %s\ntitle: %s\nmessages: %d\nupdated: %s\n---\n%s\n",
		s.Session, strconv.Quote(title), s.Messages, s.Updated.UTC().Format(time.RFC3339), s.Text)
	s.Path = filepath.Join(dir, s.Session+".md")
	tmp, err := os.CreateTemp(dir, ".summary-*")
	if err != nil {
		return Summary{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return Summary{}, err
	}
	if err := tmp.Close(); err != nil {
		return Summary{}, err
	}
	return s, os.Rename(tmp.Name(), s.Path)
}

// FindSummary is session id's summary in dir (os.ErrNotExist when none).
func FindSummary(dir, id string) (Summary, error) {
	if !sessionIDRE.MatchString(id) {
		return Summary{}, os.ErrNotExist
	}
	return readSummary(filepath.Join(dir, id+".md"))
}

// Summaries are dir's summaries, the most recently updated session first.
func Summaries(dir string) ([]Summary, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if s, err := readSummary(filepath.Join(dir, e.Name())); err == nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

func readSummary(path string) (Summary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Session: strings.TrimSuffix(filepath.Base(path), ".md"), Path: path}
	text := string(data)
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if head, body, ok := strings.Cut(rest, "\n---\n"); ok {
			text = body
			for _, line := range strings.Split(head, "\n") {
				k, v, _ := strings.Cut(line, ":")
				v = strings.TrimSpace(v)
				switch strings.TrimSpace(k) {
				case "title":
					if t, err := strconv.Unquote(v); err == nil {
						v = t
					}
					s.Title = v
				case "messages":
					s.Messages, _ = strconv.Atoi(v)
				case "updated":
					s.Updated, _ = time.Parse(time.RFC3339, v)
				}
			}
		}
	}
	s.Text = strings.TrimSpace(text)
	return s, nil
}

// RecentSession is an earlier session for RenderRecent: its summary when
// one has been written.
type RecentSession struct {
	ID      string
	Title   string
	Updated time.Time
	Summary *Summary
}

// maxStandsChars bounds each session's line in RenderRecent.
const maxStandsChars = 300

// RenderRecent is the earlier sessions as a new session's first prompt
// carries them: when each was, what it was, and where it stood, with how to
// read the rest. Empty when there are none.
func RenderRecent(recent []RecentSession) string {
	if len(recent) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Earlier sessions in this workspace, the latest first. When the user asks to pick up where they left off, or about earlier work, read one's summary with recall_session before answering:\n")
	for _, r := range recent {
		fmt.Fprintf(&b, "- %s · %q (%s)", r.Updated.Local().Format("2006-01-02 15:04"), r.Title, r.ID)
		if r.Summary != nil {
			if st := r.Summary.Stands(); st != "" {
				fmt.Fprintf(&b, ": %s", textutil.Ellipsize(st, maxStandsChars))
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
