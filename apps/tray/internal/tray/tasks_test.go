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
	"context"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// taskService answers ListHumanTasks with a human task and a timer.
type taskService struct {
	pb.UnimplementedWorkspaceServiceHandler
}

func (taskService) ListHumanTasks(context.Context, *connect.Request[pb.ListHumanTasksRequest]) (*connect.Response[pb.ListHumanTasksResponse], error) {
	return connect.NewResponse(&pb.ListHumanTasksResponse{Alerts: "open", Tasks: []*pb.HumanTask{
		{Id: "r1/approve", Kind: "human_task", Title: "Approve the refund", Workflow: "refund-triage"},
		{Id: "r2/wait", Kind: "timer"},
	}}), nil
}

func (taskService) ListWorkflowRuns(_ context.Context, r *connect.Request[pb.ListWorkflowRunsRequest]) (*connect.Response[pb.ListWorkflowRunsResponse], error) {
	return connect.NewResponse(&pb.ListWorkflowRunsResponse{Runs: []*pb.WorkflowRun{{Id: "r1", Status: "failed", Workflow: string(rune('a' + r.Msg.Limit - 20))}}}), nil
}

func TestFetchTasks(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(pb.NewWorkspaceServiceHandler(taskService{}))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })

	tasks, alerts, err := FetchTasks(context.Background(), sock)
	require.NoError(t, err)
	require.Len(t, tasks, 1, "timers aren't the user's")
	assert.Equal(t, "r1/approve", tasks[0].Id)
	assert.Equal(t, "open", alerts)

	runs, err := FetchRuns(context.Background(), sock)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "a", runs[0].Workflow, "the latest 20 asked for")

	_, _, err = FetchTasks(context.Background(), filepath.Join(shortTemp(t), "none.sock"))
	assert.Error(t, err, "nothing listening")
	_, err = FetchRuns(context.Background(), filepath.Join(shortTemp(t), "none.sock"))
	assert.Error(t, err)
}

func TestArrivedAndTasksLink(t *testing.T) {
	a := &pb.HumanTask{Id: "a", Title: "Approve the refund", Workflow: "refund-triage"}
	b := &pb.HumanTask{Id: "b", Title: "Check the order", Workflow: "refund-triage"}
	seen := map[string]bool{}
	assert.Equal(t, []*pb.HumanTask{a}, Arrived(seen, []*pb.HumanTask{a}))
	assert.Empty(t, Arrived(seen, []*pb.HumanTask{a}), "already seen")
	assert.Equal(t, []*pb.HumanTask{b}, Arrived(seen, []*pb.HumanTask{b}))
	assert.Equal(t, map[string]bool{"b": true}, seen, "an answered task is forgotten")

	cases := []struct {
		name, alerts string
		arrived      []*pb.HumanTask
		want         string
		body         string
	}{
		{name: "off", alerts: "off", arrived: []*pb.HumanTask{a}},
		{name: "nothing arrived", alerts: "notify"},
		{name: "open", alerts: "open", arrived: []*pb.HumanTask{a}, want: "blitz://tasks"},
		{name: "notify one", alerts: "notify", arrived: []*pb.HumanTask{a}, want: "blitz://tasks?", body: "Approve the refund"},
		{name: "notify more", alerts: "notify", arrived: []*pb.HumanTask{a, b}, want: "blitz://tasks?", body: "Approve the refund and 1 more"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TasksLink(c.alerts, c.arrived)
			if c.body == "" {
				assert.Equal(t, c.want, got)
				return
			}
			u, err := url.Parse(got)
			require.NoError(t, err)
			assert.Equal(t, "tasks", u.Host)
			assert.Equal(t, "1", u.Query().Get("notify"))
			assert.Equal(t, "refund-triage is waiting for you", u.Query().Get("title"))
			assert.Equal(t, c.body, u.Query().Get("body"))
		})
	}
	assert.Equal(t, "2 tasks waiting", TasksTitle(2))
}

func TestWithDot(t *testing.T) {
	for _, size := range []int{44, 64} {
		var icon bytes.Buffer
		require.NoError(t, png.Encode(&icon, image.NewNRGBA(image.Rect(0, 0, size, size))))
		out, err := WithDot(icon.Bytes())
		require.NoError(t, err)
		img, err := png.Decode(bytes.NewReader(out))
		require.NoError(t, err)
		b := img.Bounds()
		r := b.Dx() / 5
		_, _, _, alpha := img.At(b.Max.X-r-1, b.Min.Y+r+1).RGBA()
		assert.Equal(t, uint32(0xffff), alpha, "the dot's centre")
	}
	_, err := WithDot([]byte("not a png"))
	assert.Error(t, err)
}

// Runs that failed since the tray started are announced once each, with
// a link that opens the app on the run; alerts off says nothing.
func TestFailedAndRunLink(t *testing.T) {
	start := time.Unix(1000, 0)
	at := func(sec int64) *timestamppb.Timestamp { return timestamppb.New(time.Unix(sec, 0)) }
	failed := &pb.WorkflowRun{Id: "r1", Workflow: "brief", Workspace: "/w", Status: "failed", Ended: at(1100), Error: "The workflow brief stopped at Save: not approved"}
	runs := []*pb.WorkflowRun{
		failed,
		{Id: "r2", Status: "failed", Ended: at(900)},
		{Id: "r3", Status: "done", Ended: at(1200)},
		{Id: "r4", Status: "failed"},
	}
	seen := map[string]bool{}
	assert.Equal(t, []*pb.WorkflowRun{failed}, Failed(seen, runs, start), "only r1 failed since")
	assert.Empty(t, Failed(seen, runs, start), "once")

	assert.Empty(t, RunLink("off", failed))
	u, err := url.Parse(RunLink("notify", failed))
	require.NoError(t, err)
	assert.Equal(t, "run", u.Host)
	q := u.Query()
	assert.Equal(t, "1", q.Get("notify"))
	assert.Equal(t, "brief failed", q.Get("title"))
	assert.Equal(t, failed.Error, q.Get("body"))
	assert.Equal(t, "/w", q.Get("dir"))
	assert.Equal(t, "brief", q.Get("workflow"))
	assert.Equal(t, "r1", q.Get("run"))
}
