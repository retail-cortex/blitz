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

package enterprise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ServiceJob is a job the organization's service runs (spec_053_enterprise
// EN-42; blitz-enterprise spec_009_jobs JB-60), as the person may see it.
type ServiceJob struct {
	ID        string     `json:"id"`
	URN       string     `json:"urn"`
	Workspace string     `json:"workspace"` // its handle
	Name      string     `json:"name"`
	Schedule  string     `json:"schedule"`
	Timezone  string     `json:"timezone"`
	Enabled   bool       `json:"enabled"`
	NextRun   *time.Time `json:"next_run"`
	LastRun   *time.Time `json:"last_run"`
}

// ServiceRun is one of a service job's runs.
type ServiceRun struct {
	ID      string `json:"id"`
	URN     string `json:"urn"`
	Job     string `json:"job"`     // its ID
	Trigger string `json:"trigger"` // schedule, catch_up or now
	// Status is queued, running, succeeded, failed, skipped or stopped.
	Status  string     `json:"status"`
	Note    string     `json:"note"`
	Output  string     `json:"output"`
	CostUSD float64    `json:"cost_usd"`
	Created time.Time  `json:"created"`
	Started *time.Time `json:"started"`
	Ended   *time.Time `json:"ended"`
	// Branch is where its changes went ("" for none); Artifacts the name
	// of what it left (spec_055_urns); As the person it acted as.
	Branch    string `json:"branch"`
	Artifacts string `json:"artifacts"`
	As        string `json:"as"`
	// Merged is whether its changes are on main too; MergeNote why not,
	// when they're on its branch only.
	Merged    bool   `json:"merged"`
	MergeNote string `json:"merge_note"`
	// A paused run's request (status paused), and how it was answered.
	Pending    *Request `json:"pending"`
	Answer     string   `json:"answer"`
	AnswerNote string   `json:"answer_note"`
}

// Resource is what the service says a resource name is (spec_055_urns
// RN-30).
type Resource struct {
	URN       string     `json:"urn"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Parent    string     `json:"parent"`
	Created   *time.Time `json:"created"`
	MediaType string     `json:"media_type"`
	Size      int64      `json:"size"`
}

// Jobs are the service's jobs in the person's workspaces.
func (c *Client) Jobs(ctx context.Context) ([]ServiceJob, error) {
	var out struct {
		Jobs []ServiceJob `json:"jobs"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/jobs", nil, &out)
	return out.Jobs, err
}

// ReadJobs has the service read a workspace's main now, not at its next
// scan, and gives its jobs there, and the jobs main defines that the
// service doesn't run, each with why (a Viewer of it may).
func (c *Client) ReadJobs(ctx context.Context, handle string) ([]ServiceJob, map[string]string, error) {
	var out struct {
		Jobs    []ServiceJob      `json:"jobs"`
		Skipped map[string]string `json:"skipped"`
	}
	err := c.signedCall(ctx, http.MethodPost, "/api/v1/workspaces/"+url.PathEscape(handle)+"/jobs/read", nil, &out)
	return out.Jobs, out.Skipped, err
}

// JobRuns are a service job's latest runs, newest first.
func (c *Client) JobRuns(ctx context.Context, job string) ([]ServiceRun, error) {
	var out struct {
		Runs []ServiceRun `json:"runs"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/jobs/"+url.PathEscape(job)+"/runs", nil, &out)
	return out.Runs, err
}

// RunJobNow starts a run of a service job (a Runner of its workspace).
func (c *Client) RunJobNow(ctx context.Context, job string) (ServiceRun, error) {
	var r ServiceRun
	err := c.signedCall(ctx, http.MethodPost, "/api/v1/jobs/"+url.PathEscape(job)+"/run", nil, &r)
	return r, err
}

// RunMerged tells the service that a run's changes, on its branch, are on
// main now (an Editor of its workspace added them).
func (c *Client) RunMerged(ctx context.Context, job, run string) (ServiceRun, error) {
	var r ServiceRun
	err := c.signedCall(ctx, http.MethodPost, "/api/v1/jobs/"+url.PathEscape(job)+"/runs/"+url.PathEscape(run)+"/merged", nil, &r)
	return r, err
}

// SetJobEnabled switches a service job on or off (an Editor of its
// workspace).
func (c *Client) SetJobEnabled(ctx context.Context, job string, on bool) error {
	return c.signedCall(ctx, http.MethodPut, "/api/v1/jobs/"+url.PathEscape(job)+"/enabled", map[string]bool{"enabled": on}, nil)
}

// Children are what's under a resource name, all of them (RN-31).
func (c *Client) Children(ctx context.Context, name string) ([]Resource, error) {
	var all []Resource
	next := ""
	for {
		var page struct {
			Items []Resource `json:"items"`
			Next  string     `json:"next"`
		}
		if err := c.signedCall(ctx, http.MethodGet, resourcePath(name)+"?children&next="+url.QueryEscape(next), nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.Next == "" || len(all) > 10000 {
			return all, nil
		}
		next = page.Next
	}
}

// OpenResource reads a file's content by its name (RN-32): the caller
// closes it.
func (c *Client) OpenResource(ctx context.Context, name string) (io.ReadCloser, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+resourcePath(name)+"?content", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the organization's service: %w", err)
	}
	if res.StatusCode/100 != 2 {
		defer res.Body.Close()
		return nil, refusal(res)
	}
	return res.Body, nil
}

// resourcePath is a name's path on the service: the name as it is, its
// own encoding kept (RN-01).
func resourcePath(name string) string {
	return "/api/v1/resources/" + strings.TrimPrefix(name, "/")
}

// signedCall is call as the signed-in person.
func (c *Client) signedCall(ctx context.Context, method, path string, in, out any) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	return call(ctx, c.hc, method, c.url+path, token, in, out)
}

// GoingRun is a run going now, with its job's name and workspace's handle.
type GoingRun struct {
	ServiceRun
	JobName   string `json:"job_name"`
	Workspace string `json:"workspace"`
}

// GoingRuns are the runs going now in the person's workspaces.
func (c *Client) GoingRuns(ctx context.Context) ([]GoingRun, error) {
	var out struct {
		Runs []GoingRun `json:"runs"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/runs", nil, &out)
	return out.Runs, err
}

// RunChange is one run's change: queued, started, ended, or its branch
// pushed.
type RunChange struct {
	Run, Job string
	// Workspace is its handle.
	Workspace, Status string
}

// WatchRuns follows the changes of runs in the person's workspaces
// (blitz-enterprise spec_009_jobs JB-60), calling on with each, until ctx
// ends or the stream does (an error either way).
func (c *Client) WatchRuns(ctx context.Context, on func(RunChange)) error {
	err := c.events(ctx, "/api/v1/runs/watch", func(data string) {
		var ch RunChange
		if json.Unmarshal([]byte(data), &ch) == nil {
			on(ch)
		}
	})
	if err == nil {
		err = errors.New("the organization's service ended the runs' stream")
	}
	return err
}

// PendingRuns are the runs paused for the person's answer (theirs, or in
// workspaces they're an Owner of; spec_053 EN-43).
func (c *Client) PendingRuns(ctx context.Context) ([]GoingRun, error) {
	var out struct {
		Runs []GoingRun `json:"runs"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/runs/pending", nil, &out)
	return out.Runs, err
}

// AnswerRun approves or denies what a paused run asks for, with a note;
// the run goes on.
func (c *Client) AnswerRun(ctx context.Context, run string, approve bool, note string) (ServiceRun, error) {
	var r ServiceRun
	err := c.signedCall(ctx, http.MethodPost, "/api/v1/runs/"+url.PathEscape(run)+"/answer", map[string]any{"approve": approve, "note": note}, &r)
	return r, err
}
