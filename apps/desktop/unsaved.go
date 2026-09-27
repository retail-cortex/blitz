package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Unsaved is what the page says about unsaved changes in its editor, in
// the window's language: Message is "" when there are none.
type Unsaved struct {
	Message string `json:"message"`
	Quit    string `json:"quit"`
	Cancel  string `json:"cancel"`
}

// SetUnsaved records whether the editor has unsaved changes, for closing
// the window (beforeClose).
func (a *App) SetUnsaved(u Unsaved) {
	a.unsavedMu.Lock()
	a.unsaved = u
	a.unsavedMu.Unlock()
}

// beforeClose asks before closing a window with unsaved changes; it
// returns true to keep the window open.
func (a *App) beforeClose(ctx context.Context) bool {
	a.unsavedMu.Lock()
	u := a.unsaved
	a.unsavedMu.Unlock()
	if u.Message == "" {
		return false
	}
	answer, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Blitz",
		Message:       u.Message,
		Buttons:       []string{u.Quit, u.Cancel},
		DefaultButton: u.Cancel,
		CancelButton:  u.Cancel,
	})
	return err != nil || answer != u.Quit
}
