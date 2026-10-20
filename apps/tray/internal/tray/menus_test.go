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

package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The workspaces: the app's open ones in its order, named as it names
// them, then others with jobs, and the global jobs' last as Global.
func TestSpaces(t *testing.T) {
	t.Setenv("MODENV_PREFIX", t.TempDir())
	home := config.HomeWorkspace("")
	got := Spaces(
		[]Space{{Dir: "/w/shop", Name: "Shop"}, {Dir: "/w/blog"}, {Dir: "/w/shop"}},
		[]*pb.Worker{{Workspace: "/w/blog"}, {Workspace: "/w/ops"}, {Workspace: home}},
	)
	assert.Equal(t, []Space{{Dir: "/w/shop", Name: "Shop"}, {Dir: "/w/blog", Name: "blog"}, {Dir: "/w/ops", Name: "ops"}, {Dir: home, Name: "Global"}}, got)
	assert.Empty(t, Spaces(nil, nil))
}

// A workspace's workflows are read from its folder, by name, with their
// descriptions.
func TestFlows(t *testing.T) {
	dir := t.TempDir()
	flows := filepath.Join(dir, ".agents", "workflows")
	require.NoError(t, os.MkdirAll(flows, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(flows, "research-brief.flow.yaml"), []byte("description: A one-page brief\nnodes: []\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(flows, "a-check.flow.yaml"), []byte("{not yaml"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(flows, "notes.md"), []byte("a command, not a workflow"), 0o644))
	assert.Equal(t, []Flow{{Name: "a-check"}, {Name: "research-brief", Description: "A one-page brief"}}, Flows(dir))
	assert.Empty(t, Flows(t.TempDir()))
}

// What's going on: a job's run (with its workflow's progress, the
// workflow run not listed twice), then the other workflow runs going on;
// each with Show and Stop; names with the workspace when there are two.
func TestGoing(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *timestamppb.Timestamp { return timestamppb.New(now.Add(-d)) }
	steps := []*pb.WorkflowRunStep{{Status: "done"}, {Status: "skipped"}, {Status: "running"}, {Status: "waiting"}}
	l := Live{
		Runs: []*pb.WorkflowRun{
			{Id: "f1", Workspace: "/w/shop", Workflow: "nightly-flow", SessionId: "s-job", Status: "running", Steps: steps, Started: ago(40 * time.Second)},
			{Id: "f2", Workspace: "/w/shop", Workflow: "research-brief", Status: "waiting", Started: ago(3 * time.Minute)},
			{Id: "f3", Workspace: "/w/blog", Workflow: "old", Status: "done"},
		},
		JobRuns: []*pb.WorkerRun{{Id: "j1", Workspace: "/w/shop", Worker: "nightly", SessionId: "s-job", Started: ago(65 * time.Minute)}},
	}
	assert.True(t, l.Busy())
	assert.False(t, Live{Runs: []*pb.WorkflowRun{{Status: "failed"}}}.Busy())

	got := Going(l, []Space{{Dir: "/w/shop", Name: "Shop"}, {Dir: "/w/blog", Name: "blog"}}, now)
	require.Len(t, got, 2, "the job's workflow run isn't listed again")
	assert.Equal(t, "nightly · job · Shop · 2/4 · 1h 5m", got[0].Title)
	assert.Equal(t, DotRunning, got[0].Icon)
	assert.Equal(t, []Entry{
		{Title: "Show in Blitz", Icon: IconEye, Action: Action{Kind: ShowWorkers, Workspace: "/w/shop"}},
		{Title: "Stop", Icon: IconStop, Action: Action{Kind: StopRun, RunID: "j1"}},
	}, got[0].Children)
	assert.Equal(t, "research-brief · Shop · 3m", got[1].Title)
	assert.Equal(t, DotWaiting, got[1].Icon, "waiting on a person or a timer")
	assert.Equal(t, Action{Kind: ShowWorkflow, Workspace: "/w/shop", Name: "research-brief", RunID: "f2"}, got[1].Children[0].Action)
	assert.Equal(t, Action{Kind: StopWorkflow, Workspace: "/w/shop", RunID: "f2"}, got[1].Children[1].Action)

	one := Going(Live{Runs: l.Runs[1:2]}, []Space{{Dir: "/w/shop", Name: "Shop"}}, now)
	assert.Equal(t, "research-brief · 3m", one[0].Title, "one workspace: it goes without saying")
	assert.Equal(t, "40s", since(now.Add(-40*time.Second), now))
}

// A workspace's submenu: its jobs (Run now, Stop while running, Enabled,
// Show; running or failed marks; when they run next), then its
// workflows (Run, Stop while running, Show on the run).
func TestSpaceEntries(t *testing.T) {
	shop := Space{Dir: "/w/shop", Name: "Shop"}
	next := timestamppb.New(time.Date(2026, 10, 20, 2, 0, 0, 0, time.Local))
	workers := []*pb.Worker{
		{Workspace: "/w/shop", Name: "nightly", State: pb.WorkerState_WORKER_STATE_ENABLED, NextRun: next},
		{Workspace: "/w/shop", Name: "weekly", State: pb.WorkerState_WORKER_STATE_DISABLED, Hash: "h2", LastRun: &pb.WorkerRun{Status: pb.RunStatus_RUN_STATUS_FAILED}},
		{Workspace: "/w/shop", Name: "fresh", State: pb.WorkerState_WORKER_STATE_NEW},
		{Workspace: "/w/blog", Name: "elsewhere"},
	}
	l := Live{
		JobRuns: []*pb.WorkerRun{{Id: "j1", Workspace: "/w/shop", Worker: "nightly"}},
		Runs:    []*pb.WorkflowRun{{Id: "f1", Workspace: "/w/shop", Workflow: "brief", Status: "running", Steps: []*pb.WorkflowRunStep{{Status: "done"}, {Status: "running"}}}},
	}
	got := SpaceEntries(shop, workers, []Flow{{Name: "brief"}, {Name: "digest"}}, l)
	titles := make([]string, len(got))
	for i, e := range got {
		titles[i] = e.Title
	}
	assert.Equal(t, []string{"Jobs", "nightly · next Tue 02:00", "weekly", "fresh", "Workflows", "brief · 1/2", "digest"}, titles)
	assert.True(t, got[0].Disabled)
	assert.Equal(t, IconJob, got[0].Icon)

	nightly := got[1]
	assert.Equal(t, DotRunning, nightly.Icon)
	assert.Equal(t, []Action{
		{Kind: RunWorker, Workspace: "/w/shop", Name: "nightly"},
		{Kind: StopRun, RunID: "j1"},
		{Kind: DisableWorker, Workspace: "/w/shop", Name: "nightly"},
		{Kind: ShowWorkers, Workspace: "/w/shop"},
	}, actions(nightly.Children))
	weekly := got[2]
	assert.Equal(t, DotFailed, weekly.Icon, "its last run failed")
	assert.Equal(t, Action{Kind: EnableWorker, Workspace: "/w/shop", Name: "weekly", Hash: "h2"}, weekly.Children[1].Action, "enabled again at the hash reviewed")
	fresh := got[3].Children
	assert.True(t, fresh[0].Disabled, "a new job doesn't run before it's reviewed")
	assert.Equal(t, "Review it in Blitz to enable it", fresh[1].Title)

	brief := got[5]
	assert.Equal(t, DotRunning, brief.Icon)
	assert.Equal(t, []Action{
		{Kind: RunWorkflow, Workspace: "/w/shop", Name: "brief"},
		{Kind: StopWorkflow, Workspace: "/w/shop", RunID: "f1"},
		{Kind: ShowWorkflow, Workspace: "/w/shop", Name: "brief", RunID: "f1"},
	}, actions(brief.Children))
	assert.Equal(t, []Action{{Kind: RunWorkflow, Workspace: "/w/shop", Name: "digest"}, {Kind: ShowWorkflow, Workspace: "/w/shop", Name: "digest"}}, actions(got[6].Children))

	assert.Empty(t, SpaceEntries(Space{Dir: "/w/new"}, workers, nil, Live{}), "nothing to show: no submenu")
	onlyFlows := SpaceEntries(Space{Dir: "/w/new"}, workers, []Flow{{Name: "brief"}}, Live{})
	assert.Equal(t, "Workflows", onlyFlows[0].Title, "no Jobs heading without jobs")
	assert.Len(t, onlyFlows, 2)
	onlyJobs := SpaceEntries(Space{Dir: "/w/blog"}, workers, nil, Live{})
	assert.Equal(t, []string{"Jobs", "elsewhere"}, []string{onlyJobs[0].Title, onlyJobs[1].Title}, "no Workflows heading without workflows")
}

func actions(entries []Entry) []Action {
	out := make([]Action, len(entries))
	for i, e := range entries {
		out[i] = e.Action
	}
	return out
}

func TestWorkflowLink(t *testing.T) {
	assert.Equal(t, "blitz://workflow?dir=%2Fw%2Fmy+shop&workflow=brief", WorkflowLink("/w/my shop", "brief", ""))
	assert.Equal(t, "blitz://workflow?dir=%2Fw&run=r1&workflow=brief", WorkflowLink("/w", "brief", "r1"))
}

// A pulse frame keeps the icon's colours and scales its alpha.
func TestPulse(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{G: 100, A: 100})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	out, err := Pulse(b.Bytes(), 0.5)
	require.NoError(t, err)
	got, err := png.Decode(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, color.NRGBA{R: 200, A: 127}, color.NRGBAModel.Convert(got.At(0, 0)))
	assert.Equal(t, color.NRGBA{G: 100, A: 50}, color.NRGBAModel.Convert(got.At(1, 0)))
	_, err = Pulse([]byte("not a png"), 0.5)
	assert.Error(t, err)
	assert.Equal(t, 1.0, PulseFrames[0], "it starts at full brightness")
}
