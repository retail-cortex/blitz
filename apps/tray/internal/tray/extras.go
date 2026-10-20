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
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The tray's MCP servers submenu (spec_mcp_009 MCP-48) and what the menu's
// entries do (the jobs and workflows are in each workspace's submenu,
// menus.go; spec_workers_023 WK-90): what the running service says, as
// menu entries, and what clicking one does. main draws them.

// Extras is what the submenus show: the global MCP servers, every
// workspace's workers and the runs going on.
type Extras struct {
	MCP     []*pb.MCPServerConfig
	Workers []*pb.Worker
	Runs    []*pb.WorkerRun
}

// FetchExtras asks the service at sock for the submenus' lists, within a
// couple of seconds.
func FetchExtras(ctx context.Context, sock string) (Extras, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	hc := clientFor(sock)
	var e Extras
	mcp, err := pb.NewConfigServiceClient(hc, socket.BaseURL).ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{}))
	if err != nil {
		return e, err
	}
	e.MCP = mcp.Msg.Servers
	wc := pb.NewWorkerServiceClient(hc, socket.BaseURL)
	ws, err := wc.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{}))
	if err != nil {
		return e, err
	}
	e.Workers = ws.Msg.Workers
	runs, err := wc.ListRunningWorkerRuns(ctx, connect.NewRequest(&pb.ListRunningWorkerRunsRequest{}))
	if err != nil {
		return e, err
	}
	e.Runs = runs.Msg.Runs
	return e, nil
}

// ActionKind is what clicking an entry does.
type ActionKind int

// What an entry does.
const (
	NoAction      ActionKind = iota
	SetMCP                   // turn an MCP server on or off (On)
	StopRun                  // stop a worker's run (RunID)
	RunWorker                // run a worker now
	EnableWorker             // enable a worker at the hash reviewed (Hash)
	DisableWorker            // disable a worker
	ShowWorkers              // open the app on a workspace's workers
	RunWorkflow              // run a workflow in the background (Name)
	StopWorkflow             // stop a workflow's run (RunID)
	ShowWorkflow             // open the app on a workflow's canvas (Name), on a run (RunID)
)

// Action is an entry's click.
type Action struct {
	Kind      ActionKind
	Workspace string
	Name      string
	RunID     string
	Hash      string
	On        bool
}

// Entry is a menu item: a title, a checkbox (Checked) or not, greyed out
// (Disabled), its icon (Icon*, Dot*: main draws it), what a click does,
// and a submenu.
type Entry struct {
	Title    string
	Checkbox bool
	Checked  bool
	Disabled bool
	Icon     string
	Action   Action
	Children []Entry
}

// MCPEntries are the MCP servers submenu: each global server, checked
// while it's on; a click turns it off or on.
func MCPEntries(e Extras) []Entry {
	if len(e.MCP) == 0 {
		return []Entry{{Title: i18n.T("tray.mcp.none"), Disabled: true}}
	}
	out := make([]Entry, 0, len(e.MCP))
	for _, s := range e.MCP {
		out = append(out, Entry{Title: s.Name, Checkbox: true, Checked: !s.Disabled, Action: Action{Kind: SetMCP, Name: s.Name, On: s.Disabled}})
	}
	return out
}

// Key identifies a menu's entries, to redraw it only when they change.
func Key(entries []Entry) string { return fmt.Sprintf("%+v", entries) }

// WorkersLink is the blitz:// link that opens the app on a workspace's
// workers.
func WorkersLink(workspace string) string {
	return "blitz://workers?" + url.Values{"dir": {workspace}}.Encode()
}

// Do does what a click asks of the service at sock; ShowWorkers and
// ShowWorkflow open their links with open.
func Do(ctx context.Context, sock string, a Action, open func(link string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hc := clientFor(sock)
	wc := pb.NewWorkerServiceClient(hc, socket.BaseURL)
	var err error
	switch a.Kind {
	case SetMCP:
		_, err = pb.NewConfigServiceClient(hc, socket.BaseURL).SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Name: a.Name, Enabled: a.On}))
	case StopRun:
		_, err = wc.StopWorkerRun(ctx, connect.NewRequest(&pb.StopWorkerRunRequest{RunId: a.RunID}))
	case RunWorker:
		_, err = wc.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: a.Workspace, Name: a.Name}))
	case EnableWorker:
		_, err = wc.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: a.Workspace, Name: a.Name, Hash: a.Hash}))
	case DisableWorker:
		_, err = wc.DisableWorker(ctx, connect.NewRequest(&pb.DisableWorkerRequest{Workspace: a.Workspace, Name: a.Name}))
	case ShowWorkers:
		err = open(WorkersLink(a.Workspace))
	case RunWorkflow:
		// As the canvas's Run does: "/name" in the background, its input empty.
		_, err = pb.NewSessionServiceClient(hc, socket.BaseURL).StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: a.Workspace, Turn: &pb.Turn{Text: "/" + a.Name}}))
	case StopWorkflow:
		_, err = pb.NewWorkspaceServiceClient(hc, socket.BaseURL).StopWorkflowRun(ctx, connect.NewRequest(&pb.StopWorkflowRunRequest{Workspace: a.Workspace, Id: a.RunID}))
	case ShowWorkflow:
		err = open(WorkflowLink(a.Workspace, a.Name, a.RunID))
	case NoAction:
	default:
		err = errors.New("no such action")
	}
	return err
}
