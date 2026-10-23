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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestHarnessEvidenceTool(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	type asked struct {
		since time.Time
		about string
	}
	tests := []struct {
		name      string
		off       bool
		args      map[string]any
		wantSince func(time.Time) bool
		wantAbout string
		wantErr   string
	}{
		{name: "off without a reader", off: true, args: map[string]any{}, wantErr: "isn't available"},
		{name: "the default", args: map[string]any{}, wantSince: time.Time.IsZero},
		{name: "a date", args: map[string]any{"since": "2026-10-01"}, wantSince: day.Equal},
		{name: "days", args: map[string]any{"since": "14", "about": "Skills"}, wantSince: func(t time.Time) bool {
			return t.After(time.Now().AddDate(0, 0, -14).Add(-time.Minute)) && t.Before(time.Now().AddDate(0, 0, -13))
		}, wantAbout: "skills"},
		{name: "days with a unit", args: map[string]any{"since": "3d"}, wantSince: func(t time.Time) bool { return !t.IsZero() }},
		{name: "not a date", args: map[string]any{"since": "last week"}, wantErr: "a date (2026-10-01) or a number of days"},
		{name: "no days", args: map[string]any{"since": "0"}, wantErr: "above 0"},
		{name: "about something else", args: map[string]any{"about": "jobs"}, wantErr: "skills, agents, rules or empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{}
			var got []asked
			if !tt.off {
				r.SetEvidenceReader(func(_ context.Context, since time.Time, about string) (string, time.Time) {
					got = append(got, asked{since, about})
					return "Evidence since …", day
				})
			}
			out := runTool(t, toolOf(t)(NewHarnessEvidenceTool(r)), tt.args)
			if tt.wantErr != "" {
				assert.Contains(t, errOf(out), tt.wantErr)
				assert.Empty(t, got, "the reader isn't asked")
				return
			}
			assert.Empty(t, errOf(out))
			assert.Equal(t, "Evidence since …", out["evidence"])
			assert.Equal(t, "2026-10-01", out["since"], "where it starts, as the reader says")
			if assert.Len(t, got, 1) {
				assert.True(t, tt.wantSince(got[0].since), "since %v", got[0].since)
				assert.Equal(t, tt.wantAbout, got[0].about)
			}
		})
	}
}

// A scheduled reflection keeps what it would have offered; in a
// conversation the user is asked instead.
func TestHarnessProposeTool(t *testing.T) {
	unattended := Unattended(context.Background(), func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil })
	six := []any{"add a", " ", "add b", "add c", "add d", "add e", "add f"}
	tests := []struct {
		name    string
		ctx     context.Context
		off     bool
		fail    error
		want    []string
		wantErr string
	}{
		{name: "unattended", ctx: unattended, want: []string{"add a", "add b", "add c", "add d", "add e"}},
		{name: "in a conversation", ctx: context.Background(), wantErr: "ask the user"},
		{name: "off without a saver", ctx: unattended, off: true, wantErr: "isn't available"},
		{name: "the saver fails", ctx: unattended, fail: errors.New("disk full"), wantErr: "disk full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{}
			var saved []string
			if !tt.off {
				r.SetProposalSaver(func(_ context.Context, changes []string) error {
					saved = changes
					return tt.fail
				})
			}
			out := runToolCtx(t, tt.ctx, toolOf(t)(NewHarnessProposeTool(r)), map[string]any{"changes": six})
			if tt.wantErr != "" {
				assert.Contains(t, errOf(out), tt.wantErr)
				if tt.fail == nil {
					assert.Nil(t, saved, "nothing is kept")
				}
				return
			}
			assert.Empty(t, errOf(out))
			assert.Equal(t, tt.want, saved, "at most five, blanks dropped")
			assert.EqualValues(t, 5, out["saved"])
		})
	}
}

// A chosen "How to work" entry goes to the global file, in a conversation
// only.
func TestSaveGlobalExperienceTool(t *testing.T) {
	unattended := Unattended(context.Background(), func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil })
	tests := []struct {
		name    string
		ctx     context.Context
		off     bool
		entry   string
		fail    error
		want    string
		wantErr string
	}{
		{name: "chosen", ctx: context.Background(), entry: "- Read the error before retrying. (2026-10-01 \"a\")", want: "Read the error before retrying. (2026-10-01 \"a\")"},
		{name: "unattended", ctx: unattended, entry: "x", wantErr: "nobody can answer"},
		{name: "empty", ctx: context.Background(), entry: " ", wantErr: "one line"},
		{name: "two lines", ctx: context.Background(), entry: "a\nb", wantErr: "one line"},
		{name: "off", ctx: context.Background(), off: true, entry: "x", wantErr: "isn't available"},
		{name: "the save fails", ctx: context.Background(), entry: "x", fail: errors.New("read-only"), wantErr: "read-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{}
			var saved string
			if !tt.off {
				r.SetGlobalExperienceSaver(func(_ context.Context, entry string) (string, error) {
					saved = entry
					return "/home/me/.blitz/BLITZ.md", tt.fail
				})
			}
			out := runToolCtx(t, tt.ctx, toolOf(t)(NewSaveGlobalExperienceTool(r)), map[string]any{"entry": tt.entry})
			if tt.wantErr != "" {
				assert.Contains(t, errOf(out), tt.wantErr)
				return
			}
			assert.Empty(t, errOf(out))
			assert.Equal(t, tt.want, saved)
			assert.Equal(t, "/home/me/.blitz/BLITZ.md", out["path"])
		})
	}
}
