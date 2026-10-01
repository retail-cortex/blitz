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

package workers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSchedule(t *testing.T) {
	for text, want := range map[string]string{
		"0 6 * * *":                 "0 6 * * *",
		"@daily":                    "@daily",
		"@every 90m":                "@every 90m",
		"Every two hours":           "0 */2 * * *",
		"every 5 hours":             "@every 5h",
		"every 15 minutes":          "*/15 * * * *",
		"every 7 minutes":           "@every 7m",
		"every minute":              "*/1 * * * *",
		"hourly":                    "@hourly",
		"Daily":                     "@daily",
		"Daily at 6 AM":             "0 6 * * *",
		"daily at 6:30 pm":          "30 18 * * *",
		"every day at 18:05":        "5 18 * * *",
		"Weekdays at 9:30":          "30 9 * * 1-5",
		"every weekday at 9 am":     "0 9 * * 1-5",
		"weekends at noon":          "0 12 * * 0,6",
		"Every Monday at 8 PM":      "0 20 * * 1",
		"on fridays at midnight":    "0 0 * * 5",
		"every 2 days":              "0 0 */2 * *",
		"daily at 12 am":            "0 0 * * *",
		"Every Sunday at 7:15 a.m.": "15 7 * * 0",
		"weekly":                    "@weekly",
		"every month":               "@monthly",
	} {
		t.Run(text, func(t *testing.T) {
			s, err := ParseSchedule(text, "")
			assert.NoError(t, err, "%q: cron %q, %v; want %q", text, s.Cron, err, want)
			assert.Equal(t, want, s.Cron, "%q: cron %q, %v; want %q", text, s.Cron, err, want)
		})
	}
	for _, text := range []string{"", "sometimes", "every blue moon", "daily at 25:00", "daily at 13 pm", "every funday at 9", "* * *", "every 0 minutes", "0 99 * * *", "daily at teatime", "daily at 13 am"} {
		t.Run(text, func(t *testing.T) {
			_, err := ParseSchedule(text, "")
			assert.Error(t, err, "%q was accepted", text)
		})
	}
	_, err := ParseSchedule("@daily", "Mars/Olympus")
	assert.Error(t, err, "an unknown time zone was accepted")
}

func TestScheduleNextUsesItsTimeZone(t *testing.T) {
	s, err := ParseSchedule("daily at 6 AM", "America/Chicago")
	require.NoError(t, err)
	chicago, _ := time.LoadLocation("America/Chicago")
	from := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) // 7 AM in Chicago
	want := time.Date(2026, 9, 27, 6, 0, 0, 0, chicago)
	got := s.Next(from)
	assert.True(t, got.Equal(want), "next %v, want %v", got, want)
}

func writeWorker(t *testing.T, root, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	os.MkdirAll(dir, 0o755)
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644))
	return dir
}

const valid = `---
description: Summarise outdated dependencies
schedule: Daily at 6 AM
timezone: America/Chicago
permissions: ["shell:go list -m -u all", "write:reports/"]
limits: { max_turns: 30, max_cost_usd: 0.5, timeout: 20m }
---
Check for outdated Go modules and write reports/deps.md.
`

func TestLoadWorker(t *testing.T) {
	root := t.TempDir()
	dir := writeWorker(t, root, "deps", valid)
	w, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "deps", w.Name, "worker %+v", w)
	assert.Equal(t, "0 6 * * *", w.Schedule.Cron, "worker %+v", w)
	assert.Equal(t, "America/Chicago", w.Schedule.Location.String(), "worker %+v", w)
	assert.Len(t, w.Permissions, 2, "worker %+v", w)
	assert.Equal(t, 30, w.Limits.MaxTurns, "worker %+v", w)
	assert.Equal(t, 20*time.Minute, w.Limits.Timeout, "worker %+v", w)
	assert.Equal(t, "skip", w.Overlap, "worker %+v", w)
	assert.Equal(t, "none", w.CatchUp, "worker %+v", w)
	assert.True(t, strings.HasPrefix(w.Prompt, "Check for outdated"), "worker %+v", w)
	assert.True(t, strings.HasPrefix(w.Hash, "sha256:"), "worker %+v", w)

	// Any change to the worker's files changes its hash.
	before := w.Hash
	os.WriteFile(filepath.Join(dir, "template.md"), []byte("# Report"), 0o644)
	w2, _ := Load(dir)
	assert.NotEqual(t, before, w2.Hash, "adding a file didn't change the hash")
}

func TestLoadRejectsBadWorkers(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"no-frontmatter": "just text",
		"no-schedule":    "---\ndescription: x\n---\ndo it\n",
		"bad-schedule":   "---\nschedule: now and then\n---\ndo it\n",
		"no-workflow":    "---\nschedule: hourly\n---\n",
		"misspelled":     "---\nschedul: hourly\n---\ndo it\n",
		"wrong-name":     "---\nname: other\nschedule: hourly\n---\ndo it\n",
		"bad-permission": "---\nschedule: hourly\npermissions: [\"write:/etc/passwd\"]\n---\ndo it\n",
		"Bad_Name":       "---\nschedule: hourly\n---\ndo it\n",
		"bad-timeout":    "---\nschedule: hourly\nlimits: {timeout: soon}\n---\ndo it\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeWorker(t, root, name, content))
			var invalid *InvalidError
			assert.Error(t, err, "%s", name)
			assert.False(t, !errors.As(err, &invalid) && name != "no-frontmatter" && name != "misspelled", "%s: %v", name, err)
		})
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	writeWorker(t, root, "deps", valid)
	writeWorker(t, root, "broken", "---\nschedule: whenever\n---\ndo it\n")
	os.MkdirAll(filepath.Join(root, "notes"), 0o755) // no WORKER.md: ignored
	list, err := Discover(root, filepath.Join(root, "missing"))
	require.NoError(t, err, "found %d workers,", len(list))
	require.Len(t, list, 2, "found %d workers, %v", len(list), err)
	invalid := 0
	for _, f := range list {
		if f.Err != nil {
			invalid++
		}
	}
	assert.Equal(t, 1, invalid, "%d invalid, want 1", invalid)
}

func TestPermissions(t *testing.T) {
	var perms []Permission
	for _, s := range []string{"shell:go list -m -u all", "shell:git log *", "shell:make lint && make test", "write:reports/", "delete:tmp/*.log", "web:proxy.golang.org", "mcp:github:create_*"} {
		t.Run(s, func(t *testing.T) {
			p, err := ParsePermission(s)
			require.NoError(t, err)
			perms = append(perms, p)
		})
	}
	req := func(kind api.ActionKind, targets ...string) api.ApprovalRequest {
		return api.ApprovalRequest{Kind: kind, Targets: targets}
	}
	for _, c := range []struct {
		req  api.ApprovalRequest
		want bool
	}{
		{req(api.ActionCommand, "go list -m -u all"), true},
		{req(api.ActionCommand, "go list -m -u all && rm -rf ~"), false},
		{req(api.ActionCommand, "git log --oneline"), true},
		// A glob covers one command, never one chained, piped or substituted.
		{req(api.ActionCommand, "git log x; rm -rf ~"), false},
		{req(api.ActionCommand, "git log x | sh"), false},
		{req(api.ActionCommand, "git log $(curl evil.example)"), false},
		{req(api.ActionCommand, "git log `id`"), false},
		{req(api.ActionCommand, "git log x > ~/.bashrc"), false},
		{req(api.ActionCommand, "git log x\nrm -rf ~"), false},
		{req(api.ActionCommand, "make lint && make test"), true}, // exactly as permitted
		{req(api.ActionWrite, "reports/../main.go"), false},
		{req(api.ActionWrite, "reports/deps.md"), true},
		{req(api.ActionWrite, "reports/2026/deps.md"), true},
		{req(api.ActionWrite, "reports/deps.md", "main.go"), false}, // every target must be covered
		{req(api.ActionWrite, "main.go"), false},
		{req(api.ActionDelete, "tmp/a.log"), true},
		{req(api.ActionDelete, "tmp/sub/a.log"), false},
		{req(api.ActionNetwork, "proxy.golang.org"), true},
		{req(api.ActionNetwork, "evil.example"), false},
		{req(api.ActionMCP, "github:create_issue"), true},
		{req(api.ActionMCP, "github:delete_repo"), false},
		{req(api.ActionWrite), false}, // nothing to match
	} {
		got := Allows(perms, c.req)
		assert.Equal(t, c.want, got, "%v %v: %v, want %v", c.req.Kind, c.req.Targets, got, c.want)
	}
	for _, bad := range []string{"read:x", "shell", "write:../x", "write:/abs", "web:"} {
		t.Run(bad, func(t *testing.T) {
			_, err := ParsePermission(bad)
			assert.Error(t, err, "%q accepted", bad)
		})
	}
}

// What Render writes loads back as the same worker, with only the fields
// set in its frontmatter.
func TestRenderLoadsBack(t *testing.T) {
	cases := []struct {
		name string
		spec api.WorkerSpec
		not  []string // keys left out of the frontmatter
	}{
		{
			name: "every field",
			spec: api.WorkerSpec{
				Name: "deps", Description: "Outdated modules", Schedule: "Weekdays at 9:30", Timezone: "Europe/Paris",
				Agent: "qa", Model: "gemini-3.8-flash", Permissions: []string{"shell:go list -m -u all", " ", "write:reports/*"},
				Limits: api.Limits{MaxTurns: 20, MaxCostUSD: 0.5, TimeoutRaw: "15m"}, CatchUp: "once", Prompt: "  Report outdated modules.\n\nIn reports/deps.md.  ",
			},
		},
		{
			name: "the least",
			spec: api.WorkerSpec{Name: "tidy", Schedule: "@daily", Prompt: "Tidy up."},
			not:  []string{"description", "timezone", "agent", "model", "permissions", "limits", "catch_up"},
		},
		{
			name: "text that needs quoting",
			spec: api.WorkerSpec{Name: "odd", Description: "a: b # c", Schedule: "0 6 * * *", Prompt: "---\nnot frontmatter"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := Render(c.spec)
			fm, _, err := split(data)
			require.NoError(t, err, "%s", data)
			for _, k := range c.not {
				assert.NotContains(t, string(fm), k+":", "empty %s written:\n%s", k, data)
			}
			dir := filepath.Join(t.TempDir(), c.spec.Name)
			require.NoError(t, os.Mkdir(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), data, 0o644))
			w, err := Load(dir)
			require.NoError(t, err, "%s", data)
			assert.Equal(t, strings.TrimSpace(c.spec.Description), w.Description)
			assert.Equal(t, strings.TrimSpace(c.spec.Prompt), w.Prompt)
			assert.Equal(t, c.spec.Agent, w.Agent)
			assert.Equal(t, c.spec.Model, w.Model)
			assert.Equal(t, c.spec.Limits.MaxTurns, w.Limits.MaxTurns)
			assert.Equal(t, c.spec.Limits.MaxCostUSD, w.Limits.MaxCostUSD)
			assert.Equal(t, c.spec.Limits.TimeoutRaw, w.Limits.TimeoutRaw)
			var perms []string
			for _, p := range w.Permissions {
				perms = append(perms, p.String())
			}
			var want []string
			for _, p := range c.spec.Permissions {
				if strings.TrimSpace(p) != "" {
					want = append(want, p)
				}
			}
			assert.Equal(t, want, perms)
		})
	}
}

// TestLoadReportsEveryProblem checks the remaining problems Load reports,
// each named in the error's message.
func TestLoadReportsEveryProblem(t *testing.T) {
	root := t.TempDir()
	for name, tc := range map[string]struct{ content, want string }{
		"negative":   {"---\nschedule: hourly\nlimits: {max_turns: -1}\n---\ndo it\n", "can't be negative"},
		"overlap":    {"---\nschedule: hourly\noverlap: queue\n---\ndo it\n", "overlap \"queue\""},
		"catch-up":   {"---\nschedule: hourly\ncatch_up: always\n---\ndo it\n", "catch_up \"always\""},
		"unclosed":   {"---\nschedule: hourly\n", "ends the frontmatter"},
		"no-newline": {"---\nschedule: hourly\n---", "workflow"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeWorker(t, root, name, tc.content))
			assert.ErrorContains(t, err, tc.want)
		})
	}

	w, err := Load(writeWorker(t, root, "explicit", "---\nschedule: hourly\noverlap: skip\ncatch_up: once\n---\ndo it\n"))
	require.NoError(t, err)
	assert.Equal(t, "skip", w.Overlap)
	assert.Equal(t, "once", w.CatchUp)

	_, err = Load(filepath.Join(root, "missing"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestLoadReportsUnhashableDirectories checks that a worker whose files
// can't be read is invalid.
func TestLoadReportsUnhashableDirectories(t *testing.T) {
	for name, mode := range map[string]string{"file": "secret.md", "dir": "private"} {
		t.Run(name, func(t *testing.T) {
			dir := writeWorker(t, t.TempDir(), "deps", valid)
			p := filepath.Join(dir, mode)
			if name == "dir" {
				require.NoError(t, os.Mkdir(p, 0o755))
			} else {
				require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
			}
			require.NoError(t, os.Chmod(p, 0o000))
			t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
			if _, err := os.ReadDir(p); err == nil && name == "dir" {
				t.Skip("running with privileges that ignore file modes")
			}
			_, err := Load(dir)
			assert.ErrorContains(t, err, "hashing")
		})
	}
}

// TestDiscoverReportsUnreadable checks that a root that isn't a directory
// and a WORKER.md that can't be read are errors, and loose files ignored.
func TestDiscoverReportsUnreadable(t *testing.T) {
	root := t.TempDir()
	writeWorker(t, root, "deps", valid)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), nil, 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "odd", FileName), 0o755))
	file := filepath.Join(root, "README.md")

	list, err := Discover(root, file)
	require.Len(t, list, 1)
	assert.Equal(t, "deps", list[0].Worker.Name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "odd", "the unreadable WORKER.md")
	assert.Contains(t, err.Error(), "README.md", "the root that isn't a directory")
}

// TestInvalidErrorMessage checks the error names the file and each problem,
// and is api.ErrWorkerInvalid to errors.Is, as a client sees it.
func TestInvalidErrorMessage(t *testing.T) {
	err := &InvalidError{Path: "w/WORKER.md", Problems: []string{"a", "b"}}
	assert.Equal(t, "w/WORKER.md: a; b", err.Error())
	assert.ErrorIs(t, fmt.Errorf("loading: %w", err), api.ErrWorkerInvalid)
	assert.NotErrorIs(t, err, api.ErrWorkerExists)
}

// TestParsePermissionRejectsBadGlobs checks a malformed pattern is refused.
func TestParsePermissionRejectsBadGlobs(t *testing.T) {
	_, err := ParsePermission("write:reports/[")
	assert.Error(t, err)
}
