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

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// followRun shows the background run app.Follow follows until it ends or
// Ctrl+C stops following it (the run goes on), then reloads the session
// so the prompt carries on after what the run added.
func followRun(ctx context.Context, app *App, interrupts <-chan os.Signal) {
	fmt.Printf("%s%s%s\n", Dim, i18n.T("follow.start", "id", app.FollowID), Reset)
	printer := NewPrinter(app.Printer)
	fctx, stop := cancelOnSignal(ctx, interrupts)
	if ih, ok := app.Input.(interface{ SetInterruptHandler(func()) }); ok {
		ih.SetInterruptHandler(stop)
		defer ih.SetInterruptHandler(nil)
	}
	printer.Begin()
	res, err := app.Follow(fctx, printer.Handle)
	printer.End()
	interrupted := fctx.Err() != nil
	stop()
	switch {
	case interrupted:
		fmt.Printf("\n%s%s%s\n", Yellow, i18n.T("follow.stopped", "id", app.FollowID), Reset)
	case err != nil:
		fmt.Printf("\n%s✗ %s%s\n", Red, i18n.T("follow.failed", "id", app.FollowID, "error", safe(err.Error())), Reset)
	default:
		fmt.Printf("\n%s%s%s\n", Dim, i18n.T("follow.done", "id", app.FollowID), Reset)
		if line := UsageLine(res.Before, res.After); line != "" {
			fmt.Printf("%s%s%s\n", Dim, line, Reset)
		}
	}
	if s, ok := app.Workspace.ActiveSession(); ok {
		if _, _, err := app.Workspace.LoadSession(s.ID); err != nil {
			fmt.Printf("%s✗ %v%s\n", Red, err, Reset)
		}
	}
}
