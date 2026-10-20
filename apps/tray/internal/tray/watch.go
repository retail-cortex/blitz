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
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// WatchRuns follows every workspace's workflow runs on the service at
// sock (WatchWorkflowRuns): on each change, on hears the runs going on and
// lately ended. When the stream ends (the service stops or restarts), on
// hears none, and it's asked again after retry, until ctx is done.
func WatchRuns(ctx context.Context, sock string, retry time.Duration, on func([]*pb.WorkflowRun)) {
	for ctx.Err() == nil {
		stream, err := pb.NewWorkspaceServiceClient(clientFor(sock), socket.BaseURL).WatchWorkflowRuns(ctx, connect.NewRequest(&pb.WatchWorkflowRunsRequest{}))
		if err == nil {
			for stream.Receive() {
				on(stream.Msg().Runs)
			}
			stream.Close()
		}
		on(nil)
		select {
		case <-ctx.Done():
		case <-time.After(retry):
		}
	}
}
