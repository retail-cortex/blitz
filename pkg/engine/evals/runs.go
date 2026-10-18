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

package evals

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"
)

// RunLog keeps finished eval runs, one JSON line each, in a file per
// workspace and eval.
type RunLog struct {
	dir string
	mu  sync.Mutex
}

// OpenRunLog keeps runs under dir.
func OpenRunLog(dir string) *RunLog { return &RunLog{dir: dir} }

func (l *RunLog) file(workspace, eval string) (string, error) {
	if !ValidName(eval) {
		return "", fmt.Errorf("%w: %q", ErrUnknown, eval)
	}
	sum := sha256.Sum256([]byte(workspace))
	return filepath.Join(l.dir, hex.EncodeToString(sum[:8])+"-"+eval+".jsonl"), nil
}

// Append records a finished run.
func (l *RunLog) Append(r api.EvalRun) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	name, err := l.file(r.Workspace, r.Eval)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return errors.Join(err, f.Close())
}

// List returns an eval's runs, newest first, at most limit (0: all).
func (l *RunLog) List(workspace, eval string, limit int) ([]api.EvalRun, error) {
	name, err := l.file(workspace, eval)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []api.EvalRun
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var r api.EvalRun
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	slices.Reverse(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, sc.Err()
}
