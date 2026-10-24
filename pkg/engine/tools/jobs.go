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
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Jobs makes and lists the workspace's jobs (workers, spec_workers_023
// WK-70) for create_job and list_jobs; the engine supplies them.
type Jobs struct {
	// Draft is what Create would write: the job's folder, its WORKER.md,
	// and why it wouldn't load (nothing written).
	Draft func(spec api.WorkerSpec) (dir string, data []byte, problems []string, err error)
	// Preview is how a schedule is understood, and its next runs.
	Preview func(schedule, timezone string) (api.SchedulePreview, error)
	// Create writes the job, paused until the user enables it.
	Create func(spec api.WorkerSpec) (api.WorkerInfo, []string, error)
	List   func() ([]api.WorkerInfo, error)
}

// SetJobs supplies create_job's and list_jobs' calls; the zero Jobs turns
// them off.
func (r *Registry) SetJobs(j Jobs) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	r.jobs = j
}

func (r *Registry) jobCalls() Jobs {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	return r.jobs
}

var errNoJobs = errors.New("jobs are off in this workspace ([workers] enabled)")

// CreateJobInput is what the create_job tool takes.
type CreateJobInput struct {
	Name        string   `json:"name" jsonschema:"The job's name: lowercase letters, digits, - and _ (ai-news-summary)"`
	Description string   `json:"description,omitempty" jsonschema:"One line saying what it does"`
	Schedule    string   `json:"schedule" jsonschema:"When it runs, in plain English (every day at 8 am, weekdays at 9:30, every Monday at 8 PM, every two hours) or a cron expression"`
	Timezone    string   `json:"timezone,omitempty" jsonschema:"An IANA time zone (America/Chicago); empty: the service machine's"`
	Prompt      string   `json:"prompt" jsonschema:"What the agent does on each run, step by step: what to fetch or read, what to write, where (the folder and the file name pattern, e.g. reports/ai-news/MM-DD-YYYY.html) and how it looks"`
	Permissions []string `json:"permissions,omitempty" jsonschema:"What each run may do without asking, kind:pattern: web:<host> (web:news.google.com), write:<path glob> (write:reports/**), shell:<command pattern>, delete:<path glob>, mcp:<server:tool>. Only what the prompt needs"`
	Agent       string   `json:"agent,omitempty" jsonschema:"The agent that runs it; empty: the default"`
	Model       string   `json:"model,omitempty" jsonschema:"provider/model; empty: the agent's"`
	MaxTurns    int      `json:"max_turns,omitempty" jsonschema:"Model calls a run (empty: the host's default)"`
	MaxCostUSD  float64  `json:"max_cost_usd,omitempty" jsonschema:"US dollars a run (empty: the host's default)"`
	Timeout     string   `json:"timeout,omitempty" jsonschema:"Longest run, as 30m or 2h (empty: the host's default)"`
	CatchUp     string   `json:"catch_up,omitempty" jsonschema:"once: run once when a time was missed while the service was off; none (default): skip it"`
}

// CreateJobOutput is what it returns.
type CreateJobOutput struct {
	Name     string   `json:"name,omitempty"`
	Path     string   `json:"path,omitempty"`
	Schedule string   `json:"schedule,omitempty"`
	NextRuns []string `json:"next_runs,omitempty"`
	Paused   bool     `json:"paused,omitempty"`
	Note     string   `json:"note,omitempty"`
	Problems []string `json:"problems,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// jobSpec is in as the job it describes.
func jobSpec(in CreateJobInput) api.WorkerSpec {
	var perms []string
	for _, p := range in.Permissions {
		if p = strings.TrimSpace(p); p != "" {
			perms = append(perms, p)
		}
	}
	return api.WorkerSpec{
		Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description),
		Schedule: strings.TrimSpace(in.Schedule), Timezone: strings.TrimSpace(in.Timezone),
		Agent: strings.TrimSpace(in.Agent), Model: strings.TrimSpace(in.Model),
		Permissions: perms, CatchUp: strings.TrimSpace(in.CatchUp), Prompt: strings.TrimSpace(in.Prompt),
		Limits: api.Limits{MaxTurns: in.MaxTurns, MaxCostUSD: in.MaxCostUSD, TimeoutRaw: strings.TrimSpace(in.Timeout)},
	}
}

// runTimes are the next runs, as the user reads them.
func runTimes(p api.SchedulePreview, n int) []string {
	var out []string
	for i, t := range p.Next {
		if i == n {
			break
		}
		out = append(out, t.Format("Mon 2 Jan 2006 15:04 MST"))
	}
	return out
}

// NewCreateJobTool is create_job: the job the user asked for in the chat,
// written as a job (.agents/workers/<name>/WORKER.md) once they approve it
// as shown, the file and its next runs. It starts paused: the user reviews
// and enables it (WK-70). Never unattended (MustAsk refuses where no one
// can be asked).
func NewCreateJobTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "create_job", Description: "Create a job that runs a prompt on a schedule in this workspace (\"every day at 8 am fetch … and write a report to …\"). The user sees the job's file and its next runs and approves it; it starts paused until they enable it. Give the prompt everything a run needs, since it runs without this conversation, and only the permissions it needs."},
		func(ctx agent.Context, in CreateJobInput) (CreateJobOutput, error) {
			j := r.jobCalls()
			if j.Create == nil || j.Draft == nil {
				return CreateJobOutput{Error: errNoJobs.Error()}, nil
			}
			spec := jobSpec(in)
			dir, data, problems, err := j.Draft(spec)
			if err != nil {
				return CreateJobOutput{Error: err.Error()}, nil
			}
			if len(problems) > 0 {
				return CreateJobOutput{Problems: problems, Error: "the job isn't valid yet: fix these and call create_job again"}, nil
			}
			var next []string
			if j.Preview != nil {
				if p, err := j.Preview(spec.Schedule, spec.Timezone); err == nil {
					next = runTimes(p, 3)
				}
			}
			path := filepath.Join(dir, "WORKER.md")
			detail := fmt.Sprintf("Create the job %q: %s", spec.Name, spec.Schedule)
			if len(next) > 0 {
				detail += " (next: " + strings.Join(next, ", ") + ")"
			}
			detail += ". It starts paused until you enable it."
			if err := r.hooks.Approve(ctx, api.ApprovalRequest{
				Tool: "create_job", Kind: api.ActionWrite, MustAsk: true,
				Detail: detail, Diff: unifiedDiff(r.Workspace().display(path), "", string(data)), Targets: []string{r.Workspace().display(path)},
			}); err != nil {
				return CreateJobOutput{Error: err.Error()}, nil
			}
			info, problems, err := j.Create(spec)
			if err != nil {
				return CreateJobOutput{Error: err.Error()}, nil
			}
			if len(problems) > 0 {
				return CreateJobOutput{Problems: problems, Error: "the job isn't valid yet: fix these and call create_job again"}, nil
			}
			return CreateJobOutput{
				Name: info.Name, Path: r.Workspace().display(info.Path), Schedule: info.Schedule, NextRuns: next, Paused: true,
				Note: "Created paused: tell the user to review it and enable it (the card below, or Studio › Jobs).",
			}, nil
		})
}

// ListJobsOutput is what list_jobs returns.
type ListJobsOutput struct {
	Jobs  []JobSummary `json:"jobs,omitempty"`
	Error string       `json:"error,omitempty"`
}

// JobSummary is one job, as list_jobs gives it.
type JobSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Schedule    string   `json:"schedule"`
	State       string   `json:"state"`
	NextRun     string   `json:"next_run,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Problems    []string `json:"problems,omitempty"`
}

// NewListJobsTool is list_jobs: the workspace's jobs, so the agent doesn't
// make one that exists and can say what's scheduled.
func NewListJobsTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "list_jobs", Description: "List this workspace's scheduled jobs: each one's schedule, whether it's enabled, its next run and what it may do."},
		func(_ agent.Context, _ struct{}) (ListJobsOutput, error) {
			j := r.jobCalls()
			if j.List == nil {
				return ListJobsOutput{Error: errNoJobs.Error()}, nil
			}
			list, err := j.List()
			if err != nil {
				return ListJobsOutput{Error: err.Error()}, nil
			}
			out := ListJobsOutput{Jobs: []JobSummary{}}
			for _, w := range list {
				s := JobSummary{Name: w.Name, Description: w.Description, Schedule: w.Schedule, State: string(w.State), Permissions: w.Permissions, Problems: w.Problems}
				if !w.Next.IsZero() {
					s.NextRun = w.Next.Format(time.RFC3339)
				}
				out.Jobs = append(out.Jobs, s)
			}
			return out, nil
		})
}
