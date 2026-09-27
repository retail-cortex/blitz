package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/retail-cortex/blitz/internal/tools"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/retail-cortex/blitz/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/genai"
)

// ErrNothingToCompact is returned when a session is too short to compact.
var ErrNothingToCompact = errors.New("nothing to compact yet")

// compactTimeout bounds a manual summarization call.
const compactTimeout = 3 * time.Minute

// CompactResult describes a manual compaction.
type CompactResult struct {
	EventsCompacted int
	TurnsKept       int
	SummaryChars    int
}

const compactPromptTemplate = "Summarize the conversation below between a user and an AI coding agent so the agent " +
	"can continue the work without the original transcript. Keep: the user's goals and constraints, decisions made " +
	"and why, files read or changed (with paths), commands run and their outcomes, errors still unresolved, and the " +
	"agreed next steps. Be specific and concise; omit pleasantries.%s\n\n" + compaction.ConversationHistoryPlaceholder

// Compact summarises a session's history except its last keepTurns user
// turns and records the summary as a compaction event, so later prompts carry
// the summary instead of the covered events. focus, if set, tells the
// summarizer what to emphasise. The cut is always made at the start of a user
// turn so a tool call is never separated from its result.
func (e *Engine) Compact(ctx context.Context, sessionID, focus string, keepTurns int) (CompactResult, error) {
	if keepTurns < 1 {
		keepTurns = 1
	}
	return e.compact(ctx, sessionID, focus, "manual", attribute.Int("keep_turns", keepTurns), func(events []*session.Event) ([]*session.Event, int, error) {
		// Find the start of the keepTurns-th most recent user turn.
		cut, seen := -1, 0
		for i := len(events) - 1; i >= 0; i-- {
			if isUserTurnStart(events[i]) {
				seen++
				if seen == keepTurns {
					cut = i
					break
				}
			}
		}
		if cut <= 0 {
			return nil, 0, fmt.Errorf("%w: need more than %d turn(s) of history", ErrNothingToCompact, keepTurns)
		}
		return events[:cut], keepTurns, nil
	})
}

// CompactAt summarises a session's history on one side of the user turn
// that starts at event index at: the turns before it (upTo) or it and every
// turn after (from here). The rest stays as it was.
func (e *Engine) CompactAt(ctx context.Context, sessionID, focus string, at int, upTo bool) (CompactResult, error) {
	return e.compact(ctx, sessionID, focus, "rewind", attribute.Int("at", at), func(events []*session.Event) ([]*session.Event, int, error) {
		if at < 0 || at >= len(events) || !isUserTurnStart(events[at]) {
			return nil, 0, fmt.Errorf("event %d doesn't start a prompt", at)
		}
		kept := 0
		for _, ev := range events[at:] {
			if isUserTurnStart(ev) {
				kept++
			}
		}
		if upTo {
			if at == 0 {
				return nil, 0, fmt.Errorf("%w: nothing before this prompt", ErrNothingToCompact)
			}
			return events[:at], kept, nil
		}
		return events[at:], 0, nil
	})
}

// compact summarises the window pick chooses from the session's events.
func (e *Engine) compact(ctx context.Context, sessionID, focus, reason string, attr attribute.KeyValue, pick func([]*session.Event) ([]*session.Event, int, error)) (out CompactResult, err error) {
	ctx = withSettingsLookup(ctx, e.lookupSettings)
	ctx, span := observability.Start(ctx, "compact",
		observability.ConversationID.String(sessionID),
		attr,
		attribute.Bool("focus", focus != ""),
	)
	defer func() {
		span.SetAttributes(attribute.Int("events.compacted", out.EventsCompacted), attribute.Int("summary.chars", out.SummaryChars))
		observability.End(span, err)
	}()
	got, err := e.sessions.Get(ctx, &session.GetRequest{AppName: appName, UserID: "user", SessionID: sessionID})
	if err != nil {
		return CompactResult{}, fmt.Errorf("%w: no conversation in this session", ErrNothingToCompact)
	}
	hooks := e.toolReg.ScriptHooks()
	hooks.Async(ctx, "pre_compact", "", tools.HookEvent{SessionID: sessionID, Reason: reason, Prompt: focus})
	defer func() {
		if err == nil {
			hooks.Async(ctx, "post_compact", "", tools.HookEvent{SessionID: sessionID, Reason: reason})
		}
	}()
	sess := got.Session

	events := finalEvents(sess)
	window, keptTurns, err := pick(events)
	if err != nil {
		return CompactResult{}, err
	}
	if !hasUncompacted(window) {
		return CompactResult{}, fmt.Errorf("%w: those turns are already compacted", ErrNothingToCompact)
	}

	e.mu.RLock()
	llm, gc := e.llm, e.generateConfig()
	e.mu.RUnlock()
	focusLine := ""
	if f := strings.TrimSpace(focus); f != "" {
		focusLine = "\n\nPay particular attention to: " + f
	}
	summarizer, err := compaction.NewLLMSummarizer(compaction.LLMSummarizerConfig{
		Model:                 llm,
		PromptTemplate:        fmt.Sprintf(compactPromptTemplate, focusLine),
		Timeout:               compactTimeout,
		GenerateContentConfig: gc,
	})
	if err != nil {
		return CompactResult{}, err
	}
	res, err := summarizer.SummarizeEvents(ctx, transcriptEvents(window))
	if err != nil {
		return CompactResult{}, fmt.Errorf("summarize: %w", err)
	}
	summary := proseOnly(res.Content)
	if summary == nil {
		// Recording an empty summary would delete the covered turns outright.
		return CompactResult{}, errors.New("the summarizer returned no text; history left unchanged")
	}
	e.usage.Record(sessionID, llm.Name(), res.Usage)

	ev, err := compactionEvent(window, events, summary)
	if err != nil {
		return CompactResult{}, err
	}
	if err := e.sessions.AppendEvent(ctx, sess, ev); err != nil {
		return CompactResult{}, fmt.Errorf("record compaction: %w", err)
	}
	chars := 0
	for _, p := range summary.Parts {
		chars += len(p.Text)
	}
	return CompactResult{EventsCompacted: len(window), TurnsKept: keptTurns, SummaryChars: chars}, nil
}

// finalEvents are the session's events, as stored (not partial ones).
func finalEvents(sess session.Session) []*session.Event {
	var events []*session.Event
	for ev := range sess.Events().All() {
		if ev != nil && !ev.Partial {
			events = append(events, ev)
		}
	}
	return events
}

// EventCount is how many events a session's log holds (0 for a new one).
func (e *Engine) EventCount(ctx context.Context, sessionID string) int {
	got, err := e.sessions.Get(ctx, &session.GetRequest{AppName: appName, UserID: "user", SessionID: sessionID})
	if err != nil {
		return 0
	}
	return len(finalEvents(got.Session))
}

// truncater is a session service that can drop the end of a session's log
// in place (session.PersistentService).
type truncater interface {
	Truncate(ctx context.Context, appName, userID, id string, keep int) error
}

// TruncateSession keeps the first keep events of a session's log and drops
// the rest, rewinding the conversation to before them.
func (e *Engine) TruncateSession(ctx context.Context, sessionID string, keep int) error {
	if t, ok := e.sessions.(truncater); ok {
		return t.Truncate(ctx, appName, "user", sessionID, keep)
	}
	got, err := e.sessions.Get(ctx, &session.GetRequest{AppName: appName, UserID: "user", SessionID: sessionID})
	if err != nil {
		return err
	}
	events := finalEvents(got.Session)
	if keep > len(events) {
		return fmt.Errorf("session %s has %d events, fewer than %d", sessionID, len(events), keep)
	}
	if err := e.sessions.Delete(ctx, &session.DeleteRequest{AppName: appName, UserID: "user", SessionID: sessionID}); err != nil {
		return err
	}
	created, err := e.sessions.Create(ctx, &session.CreateRequest{AppName: appName, UserID: "user", SessionID: sessionID})
	if err != nil {
		return err
	}
	for _, ev := range events[:keep] {
		if err := e.sessions.AppendEvent(ctx, created.Session, ev); err != nil {
			return err
		}
	}
	return nil
}

// isUserTurnStart reports whether ev is a prompt typed by the user (as
// opposed to a tool response, which ADK also records with the user role).
func isUserTurnStart(ev *session.Event) bool {
	if ev.Author != "user" || ev.Content == nil || ev.Actions.Compaction != nil {
		return false
	}
	for _, p := range ev.Content.Parts {
		if p != nil && p.FunctionResponse != nil {
			return false
		}
	}
	return true
}

// transcriptEvents prepares a window for the summarizer the way the prompt
// builder would see it: an earlier summary becomes an event carrying its text,
// and the events it already covers are left out. Without this a second
// compaction would summarize the raw events it can see and silently lose
// everything the first summary stood in for.
func transcriptEvents(window []*session.Event) []*session.Event {
	var ranges []*session.EventCompaction
	for _, ev := range window {
		if c := ev.Actions.Compaction; c != nil && c.CompactedContent != nil {
			ranges = append(ranges, c)
		}
	}
	covered := func(ev *session.Event) bool {
		for _, r := range ranges {
			if ev.Timestamp.Before(r.StartTimestamp) || ev.Timestamp.After(r.EndTimestamp) {
				continue
			}
			excluded := false
			for _, x := range r.ExcludedEvents {
				if x.InvocationID == ev.InvocationID && x.Timestamp.Equal(ev.Timestamp) {
					excluded = true
				}
			}
			if !excluded {
				return true
			}
		}
		return false
	}
	out := make([]*session.Event, 0, len(window))
	for _, ev := range window {
		if c := ev.Actions.Compaction; c != nil {
			if c.CompactedContent == nil {
				continue
			}
			summary := &session.Event{ID: ev.ID, InvocationID: ev.InvocationID, Timestamp: ev.Timestamp, Author: "model"}
			summary.LLMResponse.Content = &genai.Content{Role: genai.RoleModel, Parts: append([]*genai.Part{{Text: "[Summary of earlier conversation]\n"}}, c.CompactedContent.Parts...)}
			out = append(out, summary)
			continue
		}
		if !covered(ev) {
			out = append(out, ev)
		}
	}
	return out
}

func hasUncompacted(events []*session.Event) bool {
	for _, ev := range events {
		if ev.Actions.Compaction == nil {
			return true
		}
	}
	return false
}

func proseOnly(c *genai.Content) *genai.Content {
	if c == nil {
		return nil
	}
	out := &genai.Content{Role: genai.RoleModel}
	for _, p := range c.Parts {
		if p != nil && p.Text != "" && !p.Thought && p.FunctionCall == nil && p.FunctionResponse == nil {
			out.Parts = append(out.Parts, &genai.Part{Text: p.Text})
		}
	}
	if len(out.Parts) == 0 {
		return nil
	}
	return out
}

// compactionEvent mirrors ADK's own summary events: the range is the true
// timestamp span of the window, and any event inside that span that is not in
// the window (and not already covered by an earlier summary in the window) is
// listed as excluded so it is never dropped unsummarized.
func compactionEvent(window, all []*session.Event, summary *genai.Content) (*session.Event, error) {
	start, end := window[0].Timestamp, window[0].Timestamp
	in := make(map[*session.Event]bool, len(window))
	var rolled []*session.EventCompaction
	for _, ev := range window {
		in[ev] = true
		if ev.Timestamp.Before(start) {
			start = ev.Timestamp
		}
		if ev.Timestamp.After(end) {
			end = ev.Timestamp
		}
		if ev.Actions.Compaction != nil {
			rolled = append(rolled, ev.Actions.Compaction)
		}
	}
	var excluded []session.EventRef
	for _, ev := range all {
		if in[ev] || ev.Actions.Compaction != nil || ev.Timestamp.Before(start) || ev.Timestamp.After(end) {
			continue
		}
		covered := false
		for _, r := range rolled {
			if !ev.Timestamp.Before(r.StartTimestamp) && !ev.Timestamp.After(r.EndTimestamp) {
				covered = true
			}
		}
		if !covered {
			excluded = append(excluded, session.EventRef{InvocationID: ev.InvocationID, Timestamp: ev.Timestamp})
		}
	}
	now := time.Now()
	if !now.After(end) {
		return nil, errors.New("clock is behind the conversation; refusing to record a compaction")
	}
	return &session.Event{
		ID:           uuid.NewString(),
		InvocationID: "e-" + uuid.NewString(),
		Timestamp:    now,
		Author:       "user",
		Branch:       window[0].Branch,
		Actions: session.EventActions{Compaction: &session.EventCompaction{
			StartTimestamp:   start,
			EndTimestamp:     end,
			CompactedContent: summary,
			ExcludedEvents:   excluded,
		}},
	}, nil
}
