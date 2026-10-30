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

package session

import (
	"encoding/json"
	"fmt"
	"os"
)

// subagentsSuffix names a session's log of what its foreground sub-agents
// did: their own conversations are kept nowhere else.
const subagentsSuffix = ".subagents.jsonl"

// AppendSubagentEvent adds ev, an event of a sub-agent that session id ran
// in the foreground, to the session's sub-agent log (<id>.subagents.jsonl),
// so what the sub-agent did and asked can be read afterwards. The event
// names its agent (Author).
func (s *Storage) AppendSubagentEvent(id string, ev any) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("encode sub-agent event: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path(id, subagentsSuffix), os.O_WRONLY|os.O_CREATE|os.O_APPEND, filePerm)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
