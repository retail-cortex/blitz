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

package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// cmdCd is /cd [path]: the session moves to the workspace in path, opened
// from scratch (its lock, file roots, sandbox, memory, MCP servers and
// project settings, with their trust question); the old one closes, or
// in the service stays open for other clients (PAR-SES-40–43). Background
// processes and tasks are dealt with first, as at exit; a target open
// elsewhere refuses the move and nothing changes. Without a path it says
// where the session is.
func cmdCd(ctx context.Context, args []string, app *App) {
	here := app.Workspace.Dir()
	if len(args) == 0 {
		fmt.Println(i18n.T("cd.here", "dir", here))
		return
	}
	if app.Cd == nil {
		fmt.Printf("%s%s%s\n", Red, i18n.T("cd.unavailable"), Reset)
		return
	}
	target := config.ExpandHome(strings.Join(args, " "))
	if !filepath.IsAbs(target) {
		target = filepath.Join(here, target)
	}
	target = filepath.Clean(target)
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		fmt.Printf("%s%s%s\n", Red, i18n.T("cd.not_dir", "path", target), Reset)
		return
	}
	if same(target, here) {
		fmt.Println(i18n.T("cd.same", "dir", here))
		return
	}
	// Background work belongs to the workspace being left.
	if !ConfirmExit(ctx, app.Input, app.Workspace.Processes(), nil, ExitPrompt{CanPrompt: app.Input != nil, AllowCancel: true, Tasks: app}) {
		return
	}

	next, locales, commit, err := app.Cd(ctx, target)
	if err != nil {
		fmt.Printf("%s%s%s\n", Red, i18n.T("cd.failed", "dir", here, "error", err), Reset)
		return
	}
	moved := false
	if active, ok := app.Workspace.ActiveSession(); ok && active.MessageCount > 0 {
		if _, err := next.MoveSession(active.ID); err != nil {
			next.Close()
			fmt.Printf("%s%s%s\n", Red, i18n.T("cd.failed", "dir", here, "error", err), Reset)
			return
		}
		moved = true
	} else if _, _, err := next.OpenSession("", false); err != nil {
		next.Close()
		fmt.Printf("%s%s%s\n", Red, i18n.T("cd.failed", "dir", here, "error", err), Reset)
		return
	}
	old := app.Workspace
	app.Workspace = next
	if locales != nil {
		app.Locales = locales
	}
	commit()
	if err := old.Close(); err != nil {
		fmt.Printf("%s%s%s\n", Dim, safe(err.Error()), Reset)
	}
	if moved {
		fmt.Printf("%s%s%s\n", Green, i18n.T("cd.moved", "dir", next.Dir()), Reset)
	} else {
		fmt.Printf("%s%s%s\n", Green, i18n.T("cd.new", "dir", next.Dir()), Reset)
	}
}

// same reports whether a and b are the same directory.
func same(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}
