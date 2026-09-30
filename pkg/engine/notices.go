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

// Background processes' notices (spec_parity_027 PAR-TOOL-04):
// run_shell_command's notify and notify_pattern report a process's exit
// and matching lines to its session. A turn running there reads them with
// its next tool result, as a message sent while it runs; otherwise they
// wait for the session's next prompt, which the REPL starts when it's
// idle (TakeProcessNotices).

// maxNotices bounds what waits for a session.
const maxNotices = 50

// processNotice delivers a notice for session.
func (w *Workspace) processNotice(session, text string) {
	if session != "" && w.busy(session) {
		w.engine.Steer(session, "(background process) "+text)
		return
	}
	w.noticeMu.Lock()
	defer w.noticeMu.Unlock()
	if w.notices == nil {
		w.notices = map[string][]string{}
	}
	if len(w.notices[session]) < maxNotices {
		w.notices[session] = append(w.notices[session], text)
	}
}

// TakeProcessNotices removes and returns the notices waiting for session
// (the REPL starts a turn with them).
func (w *Workspace) TakeProcessNotices(session string) []string {
	w.noticeMu.Lock()
	defer w.noticeMu.Unlock()
	out := w.notices[session]
	delete(w.notices, session)
	return out
}
