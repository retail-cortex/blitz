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

package engine

import (
	"context"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
)

// Background tasks (spec_background_agents_032): a front end in this
// process sees every task; the service shows a client its own sessions'
// (the *In methods).

// ListTasks are the workspace's background tasks, oldest first.
func (w *Workspace) ListTasks() []api.TaskInfo { return w.engine.ListTasks(nil) }

// Task is background task id with its latest events, in words.
func (w *Workspace) Task(id string) (api.TaskInfo, []string, error) { return w.TaskIn(nil, id) }

// StopTask stops background task id.
func (w *Workspace) StopTask(id string) (api.TaskInfo, error) { return w.StopTaskIn(nil, id) }

// ListTasksIn are the tasks sessions started, oldest first.
func (w *Workspace) ListTasksIn(sessions []string) []api.TaskInfo {
	if sessions == nil {
		sessions = []string{}
	}
	return w.engine.ListTasks(sessions)
}

// TaskIn is task id with its latest events, when one of sessions (nil:
// any) started it (else api.ErrUnknownTask).
func (w *Workspace) TaskIn(sessions []string, id string) (api.TaskInfo, []string, error) {
	return w.engine.WaitTask(context.Background(), sessions, id, 0)
}

// StopTaskIn stops task id, when one of sessions (nil: any) started it.
func (w *Workspace) StopTaskIn(sessions []string, id string) (api.TaskInfo, error) {
	return w.engine.StopTask(sessions, id)
}

// taskEnded records an ended task in its session's transcript, so the chat
// shows it when opened again.
func (w *Workspace) taskEnded(t api.TaskInfo) {
	w.appendIn(w.storage, t.Session, nil, session.Message{Role: "model", Kind: session.KindTask, Content: runtime.TaskNote(t)})
}
