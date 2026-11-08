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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/terminal"
)

// Terminals are the workspace's terminals (spec_062_terminal), made when
// first asked for and closed with the workspace (TRM-03).
func (w *Workspace) Terminals() *terminal.Manager {
	w.termsMu.Lock()
	defer w.termsMu.Unlock()
	if w.terms == nil {
		w.terms = terminal.NewManager(0, 0)
	}
	return w.terms
}

// OpenTerminal starts the person's shell in dir, a folder in the workspace
// ("" for the workspace itself), at cols×rows (TRM-01, TRM-04). version is
// Blitz's, for TERM_PROGRAM_VERSION.
func (w *Workspace) OpenTerminal(dir string, cols, rows uint16, version string) (*terminal.Terminal, error) {
	abs := w.Dir()
	if strings.TrimSpace(dir) != "" {
		rel, _, err := userPath(dir)
		if err != nil {
			return nil, err
		}
		abs = filepath.Join(abs, rel)
	}
	// A link in the workspace mustn't start a shell outside it.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPath, err)
	}
	root, err := filepath.EvalSymlinks(w.Dir())
	if err != nil {
		return nil, err
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: %s is outside the workspace", ErrBadPath, dir)
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s isn't a folder", ErrBadPath, dir)
	}
	t, err := w.Terminals().Open(terminal.Options{Dir: abs, Cols: cols, Rows: rows, Version: version})
	if err != nil {
		return nil, err
	}
	info := t.Info()
	slog.Info("terminal opened", "workspace", w.Dir(), "dir", dir, "shell", info.Shell) // never its text (TRM-02)
	go func() {
		<-t.Done()
		slog.Info("terminal ended", "workspace", w.Dir(), "exit_code", t.Info().ExitCode)
	}()
	return t, nil
}

// closeTerminals ends the workspace's terminals as it closes.
func (w *Workspace) closeTerminals() {
	w.termsMu.Lock()
	m := w.terms
	w.termsMu.Unlock()
	if m != nil {
		m.CloseAll()
	}
}
