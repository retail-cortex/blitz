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
	"bufio"
	"encoding/json"
	"errors"
	"os"

	"github.com/retail-cortex/blitz/pkg/api"
)

// usageSuffix is a session's ledger: one model call (or a search's billed
// queries) per line, as it was charged (spec_056_conversation_adapters
// CA-120). It's what the session spent, so a fork or a snapshot starts its
// own (what was spent getting there is the source's), and a rewind keeps
// it.
const usageSuffix = ".usage.jsonl"

// AppendCall adds c to session id's ledger. A session this store doesn't
// have (a worker run's, kept elsewhere) is left alone.
func (s *Storage) AppendCall(id string, c api.Call) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if o := s.ownerOf(id); o != nil {
		return o.AppendCall(id, c)
	}
	line, err := json.Marshal(c)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.ID != id {
		if _, err := os.Stat(s.path(id, metaSuffix)); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	f, err := os.OpenFile(s.path(id, usageSuffix), os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return errors.Join(err, f.Close())
}

// Calls returns session id's ledger, oldest first: none for a session
// from before it was kept. A torn line (a write cut short) is skipped.
func (s *Storage) Calls(id string) ([]api.Call, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	if o := s.ownerOf(id); o != nil {
		return o.Calls(id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, err := os.Open(s.path(id, usageSuffix))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []api.Call
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var c api.Call
		if json.Unmarshal(sc.Bytes(), &c) == nil {
			out = append(out, c)
		}
	}
	return out, sc.Err()
}
