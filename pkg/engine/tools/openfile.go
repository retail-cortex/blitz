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

package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// FileOpener has the desktop window open a workspace file (relative to the
// workspace) in its editor at line (0: the top); an error when no window
// is watching the workspace.
type FileOpener func(ctx context.Context, path string, line int) error

// fileTool is open_file's state: the desktop window's opener.
type fileTool struct {
	mu     sync.Mutex
	opener FileOpener
}

// SetFileOpener supplies the desktop window's editor (spec_060_blitz_tools
// DRV-31).
func (r *Registry) SetFileOpener(o FileOpener) {
	r.file.mu.Lock()
	defer r.file.mu.Unlock()
	r.file.opener = o
}

// OpenFileInput is what the open_file tool takes.
type OpenFileInput struct {
	Path string `json:"path" jsonschema:"The workspace file to show the user, relative to the workspace (notes/plan.md)"`
	Line int    `json:"line,omitempty" jsonschema:"The line to show; empty: the top"`
}

// OpenFileOutput is what it returns.
type OpenFileOutput struct {
	Path   string `json:"path,omitempty"`
	Opened bool   `json:"opened,omitempty"`
	Error  string `json:"error,omitempty"`
}

// errNoEditor is open_file's answer with no desktop window: the agent
// tells the person where the file is instead.
var errNoEditor = errors.New("no Blitz window is open to show it in: tell the user the file's path instead")

// NewOpenFileTool is open_file: a workspace file opened in the desktop
// app's editor for the user to look at (spec_060_blitz_tools DRV-31). It
// reads nothing for the agent and changes nothing, so it isn't asked
// about; blocked paths and files outside the workspace are refused.
func NewOpenFileTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "open_file", Description: "Show the user a file of this workspace in Blitz's editor, when they ask to open, show or preview it, or to see a file you wrote (Markdown shows rendered). Use this for workspace files, never open_page, browser or the shell's open. It doesn't read the file for you: for that, read_file."},
		func(ctx agent.Context, in OpenFileInput) (OpenFileOutput, error) {
			rel, err := r.editorPath(in.Path)
			if err != nil {
				return OpenFileOutput{Path: in.Path, Error: err.Error()}, nil
			}
			r.file.mu.Lock()
			open := r.file.opener
			r.file.mu.Unlock()
			if open == nil {
				return OpenFileOutput{Path: rel, Error: errNoEditor.Error()}, nil
			}
			if err := open(ctx, rel, max(in.Line, 0)); err != nil {
				return OpenFileOutput{Path: rel, Error: fmt.Sprintf("%v: tell the user the file's path instead", err)}, nil
			}
			return OpenFileOutput{Path: rel, Opened: true}, nil
		})
}

// editorPath is p as the editor opens it, relative to the workspace, or
// why it can't be: it must be a readable file (blocked paths refused)
// inside the workspace's folder.
func (r *Registry) editorPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path is required")
	}
	ws := r.Workspace()
	rel, err := ws.Rel(p)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s isn't in the workspace: only its files open in the editor", rel)
	}
	info, err := ws.Stat(rel)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a folder, not a file", rel)
	}
	return filepath.ToSlash(rel), nil
}
