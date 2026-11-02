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
	"time"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// FetchApprovals are the organization's runs waiting for the person's
// answer (spec_053 EN-43): none when not connected, signed out, or from an
// older service.
func FetchApprovals(ctx context.Context, sock string) []*pb.GoingServiceRun {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := pb.NewConfigServiceClient(clientFor(sock), socket.BaseURL).ListServiceApprovals(ctx, connect.NewRequest(&pb.ListServiceApprovalsRequest{}))
	if err != nil {
		return nil
	}
	return res.Msg.Runs
}

// ApprovalsTitle is the Jobs menu's title for n runs waiting.
func ApprovalsTitle(n int) string { return i18n.N("tray.approvals", n) }

// ApprovalEntries are the waiting runs, each with what it asks and
// Approve, Deny and Show in Blitz (the window's Processes, where the
// organization's runs are).
func ApprovalEntries(runs []*pb.GoingServiceRun) []Entry {
	out := make([]Entry, 0, len(runs))
	for _, g := range runs {
		r := g.GetRun()
		p := r.GetPending()
		title := i18n.T("tray.approval.asks", "job", g.JobName, "tool", p.GetTool())
		if p.GetDetail() != "" {
			title += ": " + p.GetDetail()
		}
		out = append(out, Entry{Title: title, Icon: IconJob, Children: []Entry{
			{Title: i18n.T("tray.approval.in", "workspace", g.Workspace), Disabled: true},
			{Title: i18n.T("tray.approval.approve"), Action: Action{Kind: AnswerServiceRun, RunID: r.GetId(), On: true}},
			{Title: i18n.T("tray.approval.deny"), Action: Action{Kind: AnswerServiceRun, RunID: r.GetId()}},
			{Title: i18n.T("tray.approval.show"), Action: Action{Kind: ShowProcess, Workspace: g.Dir}},
		}})
	}
	return out
}
