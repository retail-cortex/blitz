package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/internal/audit"
	"github.com/retail-cortex/blitz/internal/tools"
)

// Rewinding a session to one of its prompts: its files, its conversation,
// or both, or summarizing the conversation on either side of the prompt.

// RewindPoint is a prompt the session can be rewound to.
type RewindPoint struct {
	// Index is the prompt's position in the transcript.
	Index int
	Text  string
	Time  time.Time
	// Files are what the agent changed from this prompt until the next
	// (what rewinding the code to it restores, with later prompts' files).
	Files []string
	// Conversation reports whether the conversation can be rewound to it
	// (prompts recorded by older versions can't be).
	Conversation bool
}

// RewindMode is what a rewind restores.
type RewindMode string

const (
	RewindBoth          RewindMode = "both"            // files and conversation
	RewindConversation  RewindMode = "conversation"    // conversation only; files stay
	RewindCode          RewindMode = "code"            // files only; conversation stays
	RewindSummarizeFrom RewindMode = "summarize_from"  // summarize the prompt and everything after
	RewindSummarizeUpTo RewindMode = "summarize_up_to" // summarize everything before the prompt
)

// RewindModes are the modes, in the order offered.
var RewindModes = []RewindMode{RewindBoth, RewindConversation, RewindCode, RewindSummarizeFrom, RewindSummarizeUpTo}

// RewindResult is what a rewind did.
type RewindResult struct {
	Mode RewindMode
	// Restored are the files put back (code).
	Restored []string
	// Prompt is the rewound prompt's text, to edit and send again
	// (conversation).
	Prompt string
	// Compacted is the summary's result (summarize modes).
	Compacted CompactResult
}

var (
	// ErrSessionBusy reports a rewind while a turn runs in the session.
	ErrSessionBusy = errors.New("a turn is running in this session")
	// ErrNotRewindPoint reports an index that isn't one of the session's
	// prompts.
	ErrNotRewindPoint = errors.New("not a prompt of this session")
	// ErrCantRewindConversation reports a prompt recorded without its place
	// in the conversation (by an older version).
	ErrCantRewindConversation = errors.New("this prompt's place in the conversation isn't known")
	// ErrUnknownRewindMode reports a mode that isn't one of RewindModes.
	ErrUnknownRewindMode = errors.New("unknown rewind mode")
)

func (w *Workspace) turnStarted(session string) {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	if w.inTurn == nil {
		w.inTurn = map[string]int{}
	}
	w.inTurn[session]++
}

func (w *Workspace) turnEnded(session string) {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	if w.inTurn[session]--; w.inTurn[session] <= 0 {
		delete(w.inTurn, session)
	}
}

// Busy reports whether a turn is running in any of the workspace's
// sessions.
func (w *Workspace) Busy() bool {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	return len(w.inTurn) > 0
}

func (w *Workspace) busy(session string) bool {
	w.turnsMu.Lock()
	defer w.turnsMu.Unlock()
	return w.inTurn[session] > 0
}

// RewindPoints lists the active session's prompts, oldest first.
func (w *Workspace) RewindPoints() ([]RewindPoint, error) {
	active := w.storage.Active()
	if active == nil {
		return nil, ErrNoActiveSession
	}
	var points []RewindPoint
	for i, m := range active.Messages {
		if m.IsPrompt() {
			points = append(points, RewindPoint{Index: i, Text: m.Content, Time: m.Timestamp, Conversation: m.Events != nil})
		}
	}
	// A prompt's files: those of the checkpoints between it and the next.
	for _, c := range w.tools.Checkpoints().List() {
		if c.Session != active.ID || c.Prompt < 0 {
			continue
		}
		for j := len(points) - 1; j >= 0; j-- {
			if points[j].Index <= c.Prompt {
				points[j].Files = appendNew(points[j].Files, c.Files...)
				break
			}
		}
	}
	return points, nil
}

func appendNew(list []string, add ...string) []string {
	for _, a := range add {
		found := false
		for _, x := range list {
			found = found || x == a
		}
		if !found {
			list = append(list, a)
		}
	}
	return list
}

// Rewind takes the active session back to the prompt at transcript index
// index. Files are restored first (refusing on conflicts unless force), so
// a refused rewind changes nothing; then the conversation is cut before the
// prompt, whose text is returned to send again.
func (w *Workspace) Rewind(ctx context.Context, index int, mode RewindMode, force bool) (RewindResult, error) {
	active := w.storage.Active()
	if active == nil {
		return RewindResult{}, ErrNoActiveSession
	}
	id := active.ID
	if w.busy(id) {
		return RewindResult{}, ErrSessionBusy
	}
	if index < 0 || index >= len(active.Messages) || !active.Messages[index].IsPrompt() {
		return RewindResult{}, fmt.Errorf("%w: %d", ErrNotRewindPoint, index)
	}
	prompt := active.Messages[index]
	res := RewindResult{Mode: mode, Prompt: prompt.Content}
	conversation := mode == RewindBoth || mode == RewindConversation
	switch mode {
	case RewindBoth, RewindConversation, RewindCode:
	case RewindSummarizeFrom, RewindSummarizeUpTo:
		if prompt.Events == nil {
			return RewindResult{}, ErrCantRewindConversation
		}
		before := w.engine.Usage(id)
		c, err := w.engine.CompactAt(ctx, id, "", *prompt.Events, mode == RewindSummarizeUpTo)
		if err != nil {
			return RewindResult{}, err
		}
		res.Compacted = CompactResult{EventsCompacted: c.EventsCompacted, SummaryChars: c.SummaryChars, Before: before, After: w.engine.Usage(id)}
		return res, nil
	default:
		return RewindResult{}, fmt.Errorf("%w %q", ErrUnknownRewindMode, mode)
	}
	if conversation && prompt.Events == nil {
		return RewindResult{}, ErrCantRewindConversation
	}

	if mode != RewindConversation {
		undone, err := w.tools.Checkpoints().Rewind(id, index, force)
		if err != nil && !errors.Is(err, tools.ErrNothingToUndo) {
			if len(undone.Restored) == 0 {
				return RewindResult{}, err
			}
			// Some files were put back: report the rest, and go on.
			w.warn(err.Error())
		}
		res.Restored = undone.Restored
		if len(undone.Restored) > 0 {
			w.tools.Hooks().Audit().Log(audit.Entry{Kind: audit.KindUndo, Session: id, Detail: "rewind: " + strings.Join(undone.Restored, ", ")})
		}
	}
	if conversation {
		if err := w.engine.TruncateSession(ctx, id, *prompt.Events); err != nil {
			return res, fmt.Errorf("rewind the conversation: %w", err)
		}
		if err := w.storage.Truncate(index); err != nil {
			return res, fmt.Errorf("rewind the transcript: %w", err)
		}
		// The files the dropped prompts changed (if kept) now come before
		// the next prompt.
		w.tools.Checkpoints().Detach(id, index)
	}
	return res, nil
}
