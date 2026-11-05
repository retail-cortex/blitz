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

package workers

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/jsonl"
)

// RunLog keeps finished runs, one JSON line each, in a file per worker.
type RunLog struct {
	dir string
	mu  sync.Mutex
}

// OpenRunLog keeps runs under dir.
func OpenRunLog(dir string) *RunLog { return &RunLog{dir: dir} }

// file is where a worker's runs are kept; the name must be valid, since
// it is part of the file name.
func (l *RunLog) file(workspace, worker string) (string, error) {
	if !ValidName(worker) {
		return "", fmt.Errorf("%q isn't a worker name", worker)
	}
	sum := sha256.Sum256([]byte(workspace))
	return filepath.Join(l.dir, hex.EncodeToString(sum[:8])+"-"+worker+".jsonl"), nil
}

// Append records a finished run.
func (l *RunLog) Append(r api.Run) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	name, err := l.file(r.Workspace, r.Worker)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	return jsonl.Append(name, 0o600, data)
}

// List returns a worker's runs, newest first, at most limit (0: all).
func (l *RunLog) List(workspace, worker string, limit int) ([]api.Run, error) {
	name, err := l.file(workspace, worker)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	runs, err := readRuns(name)
	slices.Reverse(runs)
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, err
}

// Get finds a run by ID.
func (l *RunLog) Get(id string) (api.Run, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := os.ReadDir(l.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return api.Run{}, false, nil
	}
	if err != nil {
		return api.Run{}, false, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		runs, err := readRuns(filepath.Join(l.dir, e.Name()))
		if err != nil {
			return api.Run{}, false, err
		}
		for _, r := range runs {
			if r.ID == id {
				return r, true, nil
			}
		}
	}
	return api.Run{}, false, nil
}

// Last is the start of a worker's latest run (zero if none). A run log
// that can't be read is logged: its latest run may be missing.
func (l *RunLog) Last(workspace, worker string) time.Time {
	runs, err := l.List(workspace, worker, 1)
	if err != nil {
		slog.Warn("job run log unreadable", "workspace", workspace, "job", worker, "error", err)
	}
	if len(runs) == 0 {
		return time.Time{}
	}
	return runs[0].Started
}

func readRuns(path string) ([]api.Run, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []api.Run
	// Line by line, however long (a run with a long error): a line too
	// long for a scanner's buffer would hide every later run.
	rd := bufio.NewReader(f)
	for {
		line, err := rd.ReadBytes('\n')
		var r api.Run
		if len(bytes.TrimSpace(line)) > 0 && json.Unmarshal(line, &r) == nil {
			out = append(out, r) // a torn last line is skipped
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}
