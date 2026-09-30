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

// Package tui is the CLI's interactive session: the prompt with its history
// and completion, the slash commands, streamed Markdown output, approvals
// and questions answered at the terminal, and Ctrl+C to stop a turn. It
// drives an api.Backend, so it works the same on a local workspace and on
// one held by the service (spec_tui_019).
package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/images"
)

// App is the REPL's state: the workspace it drives and the terminal.
type App struct {
	// Workspace is the program behind the REPL: every command and turn goes
	// through it. It runs here or in the Blitz service.
	Workspace api.Backend
	// loops are /loop's prompts, sent again while the REPL stays open.
	loops loops
	// notices are background processes' reports taken while idle, to run
	// a turn with.
	notices   []string
	noticesMu sync.Mutex
	Version   string
	Input     Input
	// Attached: the workspace runs in the Blitz service (/status).
	Attached bool
	// ConfigDir is where the user's settings live ("" for the default),
	// for /config --save.
	ConfigDir string
	// Notify tells you when a long turn ends or waits for you.
	Notify Notifier
	// StatusLine is ui.status_line: "default", a command, or "" for none.
	StatusLine string
	// Follow, when set, shows a background run until it ends before the
	// first prompt (blitz attach), answering what it asks; FollowID names
	// it.
	Follow   func(ctx context.Context, on func(api.Event)) (api.TurnResult, error)
	FollowID string
	// Printer configures output rendering.
	Printer PrinterOptions
	// Locales are the interface's translation catalogs (nil: the built-in
	// ones). The interface language is the front end's own.
	Locales *i18n.Bundle
	// Attachments are images to send with the next prompt (/attach, /paste,
	// --image).
	Attachments []*images.Image
	// TerminalTitle shows the session's name in the terminal window title.
	TerminalTitle bool
	// DiffLines caps the diff shown with a background task's approval
	// request, as with the turn's own.
	DiffLines int
	// Cd opens the workspace in dir for /cd, asking about its project
	// settings as at start; commit makes it the one the program closes at
	// exit. Nil: /cd isn't available.
	Cd func(ctx context.Context, dir string) (next api.Backend, locales *i18n.Bundle, commit func(), err error)
	// announced are the background tasks whose end the REPL has shown.
	announced map[string]bool
	// Interrupts delivers Ctrl+C. If nil, RunREPL subscribes to os.Interrupt
	// itself. At the prompt an interrupt starts exit (confirming if background
	// processes run); during a turn it cancels only that turn.
	Interrupts <-chan os.Signal
}

// Printer renders agent events: model text (optionally as Markdown), tool
// activity, and a spinner while waiting.
type Printer struct {
	out    io.Writer
	pause  *pausableWriter
	respin bool // Resume restarts the spinner (it was showing at Pause)
	md     *markdownStream
	spin   *Spinner
}

// PrinterOptions configure a Printer.
type PrinterOptions struct {
	Out      io.Writer
	Markdown bool // render Markdown (requires a terminal)
	Theme    string
	Width    int
	Spinner  bool
}

// NewPrinter creates a Printer; Markdown falls back to plain text on error.
func NewPrinter(o PrinterOptions) *Printer {
	if o.Out == nil {
		o.Out = os.Stdout
	}
	pw := &pausableWriter{w: o.Out}
	p := &Printer{out: pw, pause: pw, spin: NewSpinner(pw, o.Spinner)}
	if o.Markdown {
		if md, err := newMarkdownStream(pw, o.Theme, o.Width); err == nil {
			p.md = md
		}
	}
	return p
}

// Begin marks the start of a turn.
func (p *Printer) Begin() { p.spin.Start(i18n.T("spinner.thinking")) }

// End flushes buffered output at the end of a turn.
func (p *Printer) End() {
	p.spin.Stop()
	if p.md != nil {
		p.md.Flush()
	}
}

// Pause holds back output (e.g. while the user types a steer message) until
// Resume, which prints what arrived meanwhile.
func (p *Printer) Pause() {
	p.respin = p.spin.Running()
	p.spin.Stop()
	p.pause.hold()
}

// Resume prints held-back output and continues normally. The spinner comes
// back only if it was showing: restarting it mid-sentence would erase the
// partly printed line.
func (p *Printer) Resume() {
	p.pause.release()
	if p.respin {
		p.spin.Start(i18n.T("spinner.working"))
	}
}

// pausableWriter buffers writes while held. Printer output arrives from the
// turn's goroutine while the steer prompt runs on another.
type pausableWriter struct {
	mu   sync.Mutex
	w    io.Writer
	held bool
	buf  bytes.Buffer
}

func (p *pausableWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held {
		return p.buf.Write(b)
	}
	return p.w.Write(b)
}

func (p *pausableWriter) hold() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held = true
}

func (p *pausableWriter) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held = false
	if p.buf.Len() > 0 {
		p.w.Write(p.buf.Bytes())
		p.buf.Reset()
	}
}

func (p *Printer) text(s string) {
	p.spin.Stop()
	s = safe(s)
	if p.md != nil {
		p.md.Write(s)
		return
	}
	io.WriteString(p.out, s)
}

func (p *Printer) flushText() {
	if p.md != nil {
		p.md.Flush()
	}
}

// Handle renders a turn's event.
func (p *Printer) Handle(ev api.Event) {
	switch {
	case ev.Text != nil:
		if !ev.Text.Thought && !ev.Text.Repeat {
			p.text(ev.Text.Text)
		}
	case ev.Tasks != nil:
		p.spin.Stop()
		p.flushText()
		fmt.Fprint(p.out, FormatTasks(ev.Tasks))
		p.spin.Start(i18n.T("spinner.working"))
	case ev.Notice != nil:
		p.spin.Stop()
		p.flushText()
		color := Dim
		if ev.Notice.Error {
			color = Red
		}
		fmt.Fprintf(p.out, "%s%s%s\n", color, safe(ev.Notice.Text), Reset)
		p.spin.Start(i18n.T("spinner.working"))
	case ev.ToolCall != nil && ev.ToolCall.Name == "todo", ev.ToolResult != nil && ev.ToolResult.Name == "todo" && ev.ToolResult.Result["error"] == nil:
		// Shown as the checklist (Tasks).
	case ev.ToolCall != nil:
		p.spin.Stop()
		p.flushText()
		fmt.Fprint(p.out, FormatToolCall(ev.ToolCall.Name, ev.ToolCall.Args))
	case ev.ToolResult != nil:
		p.spin.Stop()
		summary, ok := SummarizeToolResponse(ev.ToolResult.Result)
		fmt.Fprint(p.out, FormatToolResult(ev.ToolResult.Name, ok, summary))
		p.spin.Start(i18n.T("spinner.working"))
	}
}

// RunREPL runs the interactive REPL prompt loop until /exit, EOF, or ctx is cancelled.
func RunREPL(ctx context.Context, app *App) error {
	applyTheme(app, app.Printer.Theme)
	PrintBanner(app.Version, app.Workspace.ActiveAgent().Name, app.Workspace.Model().Name)
	if sandbox := app.Workspace.SandboxSummary(); len(sandbox) > 0 {
		fmt.Printf("%s%s%s\n", Dim, safe(sandbox[0]), Reset)
		fmt.Printf("   %s%s%s\n", Dim, i18n.T("repl.hint"), Reset)
		if _, ok := app.Input.(steerInput); ok {
			fmt.Printf("   %s%s%s\n", Dim, i18n.T("steer.hint"), Reset)
			fmt.Printf("   %s%s%s\n", Dim, i18n.T("keys.hint"), Reset)
		}
		fmt.Println()
	}

	if _, ok := app.Workspace.ActiveSession(); !ok {
		if _, err := app.Workspace.NewSession(); err != nil { // named after its first prompt
			return fmt.Errorf("failed to create session: %w", err)
		}
	}

	interrupts := app.Interrupts
	if interrupts == nil {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		defer signal.Stop(ch)
		interrupts = ch
	}

	var shownTitle string
	defer func() {
		if shownTitle != "" {
			fmt.Print("\033]0;\007") // give the terminal its own title back
		}
	}()
	goodbye := func() error {
		fmt.Println()
		if a, ok := app.Workspace.ActiveSession(); ok && a.MessageCount > 0 {
			fmt.Printf("%s%s%s\n", Dim, i18n.T("repl.resume_hint", "command", "blitz --resume="+a.ID), Reset)
		}
		return nil
	}
	exitPrompt := ExitPrompt{CanPrompt: true, AllowCancel: true, Tasks: app}
	promptText := func() string {
		return fmt.Sprintf("%s%s%s%s %s›%s ", Bold+Green, app.Workspace.ActiveAgent().Name, Reset, modeTag(app.Workspace.Settings().PermissionMode), Bold+Green, Reset)
	}
	if pk, ok := app.Input.(interface{ SetPromptKeys(PromptKeys) }); ok {
		// Shift+Tab reaches bypass only if the session started in it.
		withBypass := app.Workspace.Settings().PermissionMode == string(api.ModeBypass)
		pk.SetPromptKeys(PromptKeys{CycleMode: func() string {
			if !cycleMode(app, withBypass) {
				return ""
			}
			return promptText()
		}})
		defer pk.SetPromptKeys(PromptKeys{})
	}

	for {
		if app.TerminalTitle {
			active, _ := app.Workspace.ActiveSession()
			if t := Product + " · " + sessionTitle(active); t != shownTitle {
				fmt.Printf("\033]0;%s\007", safe(t))
				shownTitle = t
			}
		}
		announceTasks(app)
		answerTasks(ctx, app)
		if l := app.loops.takeDue(); l != nil { // it came due during a turn
			runLoop(ctx, app, l, interrupts)
			continue
		}
		if app.hasNotices() { // a watched background process reported
			runNotices(ctx, app, interrupts)
			continue
		}
		if app.Follow != nil {
			followRun(ctx, app, interrupts)
			app.Follow = nil
		}
		printStatusLine(ctx, app)
		prompt := promptText()
		idleCtx, stopIdle := cancelOnSignal(ctx, interrupts)
		readCtx, stopLoopWait := app.loops.waitCtx(idleCtx)
		readCtx, stopNotices := watchNotices(readCtx, app)
		line, err := app.Input.ReadInput(readCtx, prompt)
		loopDue := errors.Is(context.Cause(readCtx), errLoopDue) || errors.Is(context.Cause(readCtx), errNoticeDue)
		stopNotices()
		stopLoopWait()
		stopIdle()
		if err != nil && loopDue && ctx.Err() == nil {
			fmt.Println()
			continue // the loop runs at the top
		}
		switch {
		case err == nil:
		case ctx.Err() != nil:
			// SIGTERM: no prompt; deferred cleanup kills background processes.
			return goodbye()
		case errors.Is(err, io.EOF):
			ConfirmExit(ctx, app.Input, app.Workspace.Processes(), interrupts, ExitPrompt{Tasks: app})
			return goodbye()
		case errors.Is(err, ErrRewindKey): // Esc Esc at an empty prompt
			cmdRewind(ctx, nil, app)
			continue
		case errors.Is(err, context.Canceled): // Ctrl+C at the prompt
			if ConfirmExit(ctx, app.Input, app.Workspace.Processes(), interrupts, exitPrompt) {
				return goodbye()
			}
			continue
		default:
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if cmd, ok := strings.CutPrefix(line, "!"); ok {
			runShellPassthrough(ctx, app, cmd, interrupts)
			continue
		}
		if q, ok := strings.CutPrefix(line, "/btw"); ok && (q == "" || q[0] == ' ') {
			active, ok := app.Workspace.ActiveSession()
			switch {
			case strings.TrimSpace(q) == "":
				fmt.Printf("%s%s%s\n", Yellow, i18n.T("btw.usage"), Reset)
			case !ok:
				fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.none_active"), Reset)
			default:
				runTurn(ctx, app, active.ID, strings.TrimSpace(q), interrupts, turnOptions{aside: true})
			}
			continue
		}
		if line == "/init" {
			active, ok := app.Workspace.ActiveSession()
			if !ok {
				fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.none_active"), Reset)
				continue
			}
			runTurn(ctx, app, active.ID, line, interrupts, turnOptions{prompt: api.InitPrompt()})
			// The new BLITZ.md applies from the next prompt.
			cmdMemory(ctx, []string{"reload"}, app)
			continue
		}
		if rest, ok := strings.CutPrefix(line, "/goal"); ok && (rest == "" || rest[0] == ' ') {
			if cond := strings.TrimSpace(rest); cond != "" && cond != "clear" {
				startGoal(ctx, app, line, cond, interrupts)
				continue
			}
		}
		if rest, ok := strings.CutPrefix(line, "/search"); ok && (rest == "" || rest[0] == ' ') {
			active, ok := app.Workspace.ActiveSession()
			if !ok {
				fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.none_active"), Reset)
				continue
			}
			if t, ok := prepareSearch(ctx, app, rest, interrupts); ok {
				runTurn(ctx, app, active.ID, t.recorded, interrupts, turnOptions{prompt: t.prompt, readOnly: "search", grants: t.grants})
			}
			continue
		}
		plan := false
		if goal, ok := strings.CutPrefix(line, "/plan"); ok && (goal == "" || goal[0] == ' ') {
			if line = strings.TrimSpace(goal); line == "" {
				fmt.Printf("%s%s%s\n", Yellow, i18n.T("plan.usage"), Reset)
				continue
			}
			plan = true
		}

		if !plan { // a plan goal is text for the agent, even if it starts with "/"
			handled, err := HandleCommand(ctx, line, app)
			if errors.Is(err, ErrExit) {
				if ConfirmExit(ctx, app.Input, app.Workspace.Processes(), interrupts, exitPrompt) {
					return goodbye()
				}
				continue
			}
			if handled {
				continue
			}
		}

		// Re-read each turn: /session new and /session load switch sessions.
		active, ok := app.Workspace.ActiveSession()
		if !ok {
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.none_active"), Reset)
			continue
		}
		// A slash line that wasn't a built-in command is a custom one.
		runTurn(ctx, app, active.ID, line, interrupts, turnOptions{plan: plan, command: !plan && strings.HasPrefix(line, "/")})
	}
}

// turnOptions change how runTurn treats its prompt.
type turnOptions struct {
	// accepted: the prompt already passed prompt_submit hooks and was
	// recorded (a steer message that arrived too late to be read mid-turn).
	accepted bool
	// plan: the prompt is a goal to plan for; tools that change anything
	// are refused for the whole turn (see runtime.WithPlanOnly).
	plan bool
	// prompt, if set, is sent to the agent instead of the line, which is
	// what the transcript records (e.g. "/search web …").
	prompt string
	// readOnly names a mode that refuses the same tools as plan mode
	// (see runtime.WithReadOnly).
	readOnly string
	// aside: a /btw question, answered in a throwaway copy of the session
	// (runtime.Engine.Aside) and recorded nowhere.
	aside bool
	// command: the line is a custom slash command, expanded by the
	// workspace.
	command bool
	// grants are URLs the agent may fetch without asking (/search web).
	grants []string
}

// runTurn sends one prompt to the agent and renders the result.
func runTurn(ctx context.Context, app *App, sessionID, line string, interrupts <-chan os.Signal, o turnOptions) {
	var attached []*images.Image
	if !o.aside { // attachments wait for the next real prompt
		var ok bool
		if attached, ok = collectAttachments(app, line); !ok {
			return
		}
	}

	printer := NewPrinter(app.Printer)
	turnCtx, stopTurn := cancelOnSignal(ctx, interrupts)
	if ih, ok := app.Input.(interface{ SetInterruptHandler(func()) }); ok {
		ih.SetInterruptHandler(stopTurn)
		defer ih.SetInterruptHandler(nil)
	}
	stopSteering := func() {}
	started := time.Now()
	turnStart.Store(started.UnixNano())
	defer turnStart.Store(0)
	res, streamErr := app.Workspace.Run(turnCtx, sessionID, api.Turn{
		Text: line, Prompt: o.prompt, Plan: o.plan, ReadOnly: o.readOnly, Aside: o.aside, Accepted: o.accepted,
		Images: attached, FetchGrants: o.grants, Command: o.command,
		OnAccepted: func() {
			if len(attached) > 0 {
				for _, img := range attached {
					fmt.Printf("%s%s%s\n", Dim, safe(img.Summary()), Reset)
				}
				app.Attachments = nil
			}
			if o.plan {
				fmt.Printf("%s%s%s\n", Dim, i18n.T("plan.mode"), Reset)
			}
			if o.aside {
				fmt.Printf("%s%s%s\n", Dim, i18n.T("btw.mode"), Reset)
			}
			fmt.Println()
			printer.Begin()
			if !o.aside {
				stopSteering = watchSteering(turnCtx, app, sessionID, printer)
			}
		},
		// Waits for a message being typed, so it is sent rather than lost.
		OnFinished: func() { stopSteering() },
	}, printer.Handle)
	var blocked *api.BlockedError
	if errors.As(streamErr, &blocked) {
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("repl.prompt_blocked", "reason", safe(blocked.Reason)), Reset)
		stopTurn()
		return
	}
	printer.End()
	turnInterrupted := turnCtx.Err() != nil
	stopTurn()
	if !turnInterrupted && app.Notify.longRunning() {
		app.Notify.Send(i18n.T("notify.turn_finished", "seconds", int(time.Since(started).Seconds())))
	}
	switch {
	case streamErr != nil && turnInterrupted:
		fmt.Printf("\n%s%s%s\n", Yellow, i18n.T("repl.interrupted"), Reset)
	case streamErr != nil:
		fmt.Printf("\n%s✗ %s%s\n", Red, i18n.T("repl.error", "error", safe(streamErr.Error())), Reset)
	default:
		fmt.Println()
	}
	if line := UsageLine(res.Before, res.After); line != "" {
		fmt.Printf("%s%s%s\n", Dim, line, Reset)
	}
	fmt.Println()

	// Messages sent after the model's last tool call were never read.
	if len(res.Leftover) > 0 {
		text := strings.Join(res.Leftover, "\n\n")
		if turnInterrupted {
			fmt.Printf("%s%s%s\n\n", Yellow, i18n.T("steer.dropped", "text", safe(text)), Reset)
			return
		}
		fmt.Printf("%s%s%s\n", Cyan, i18n.T("steer.sending"), Reset)
		runTurn(ctx, app, sessionID, text, interrupts, turnOptions{accepted: true})
	}
}

// steerInput is an Input that can watch the keyboard during a turn.
type steerInput interface {
	WatchKeys(onKey func(prefill string)) (stop func())
	AskSteer(ctx context.Context, prompt, prefill string) (string, error)
}

// watchSteering lets the user type a message while the turn runs. Output
// is held back while they type. An accepted message is queued for the
// agent, which reads it with its next tool result (see app.Workspace.Steer).
func watchSteering(ctx context.Context, app *App, sessionID string, printer *Printer) (stop func()) {
	in, ok := app.Input.(steerInput)
	if !ok {
		return func() {}
	}
	return in.WatchKeys(func(prefill string) {
		printer.Pause()
		defer printer.Resume()
		text, err := in.AskSteer(ctx, "\n"+Cyan+i18n.T("steer.prompt")+Reset, prefill)
		text = strings.TrimSpace(text)
		switch {
		case err != nil: // Ctrl+C: the turn is being cancelled
			return
		case text == "":
			fmt.Printf("%s%s%s\n", Dim, i18n.T("steer.cancelled"), Reset)
			return
		}
		var blocked *api.BlockedError
		if err := app.Workspace.Steer(ctx, sessionID, text); errors.As(err, &blocked) {
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("repl.prompt_blocked", "reason", safe(blocked.Reason)), Reset)
			return
		}
		fmt.Printf("%s%s%s\n", Dim, i18n.T("steer.queued"), Reset)
	})
}

// UsageLine summarises a turn's usage: tokens in/out, context size and cost.
func UsageLine(before, after api.Usage) string {
	if after.Calls == before.Calls {
		return ""
	}
	s := "↳ " + i18n.T("usage.line", "input", humanTokens(after.Input-before.Input), "output", humanTokens(after.Output-before.Output), "context", humanTokens(after.LastPrompt))
	if after.Priced {
		s += " · " + i18n.T("usage.cost", "cost", fmt.Sprintf("$%.4f", after.CostUSD-before.CostUSD), "total", fmt.Sprintf("$%.4f", after.CostUSD))
	}
	return s
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// errNoticeDue interrupts the prompt when a background process reported.
var errNoticeDue = errors.New("a background process reported")

// noticePoll is how often the idle prompt asks for process notices.
var noticePoll = 2 * time.Second

// watchNotices is ctx, cancelled with errNoticeDue once the active
// session's watched processes have reported (kept in app.notices).
func watchNotices(ctx context.Context, app *App) (context.Context, func()) {
	cctx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(noticePoll)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-cctx.Done():
				return
			case <-t.C:
				active, ok := app.Workspace.ActiveSession()
				if !ok {
					continue
				}
				if n := app.Workspace.TakeProcessNotices(active.ID); len(n) > 0 {
					app.noticesMu.Lock()
					app.notices = append(app.notices, n...)
					app.noticesMu.Unlock()
					cancel(errNoticeDue)
					return
				}
			}
		}
	}()
	var once sync.Once
	return cctx, func() { once.Do(func() { close(done) }); cancel(nil) }
}

func (app *App) hasNotices() bool {
	app.noticesMu.Lock()
	defer app.noticesMu.Unlock()
	return len(app.notices) > 0
}

// runNotices runs a turn with what background processes reported.
func runNotices(ctx context.Context, app *App, interrupts <-chan os.Signal) {
	app.noticesMu.Lock()
	notices := app.notices
	app.notices = nil
	app.noticesMu.Unlock()
	active, ok := app.Workspace.ActiveSession()
	if !ok || len(notices) == 0 {
		return
	}
	fmt.Printf("%s%s%s\n", Dim, i18n.T("notice.running"), Reset)
	runTurn(ctx, app, active.ID, "(background) "+strings.Join(notices, "\n\n"), interrupts, turnOptions{})
}

// runLoop sends a loop's prompt in the active session.
func runLoop(ctx context.Context, app *App, l *sessionLoop, interrupts <-chan os.Signal) {
	active, ok := app.Workspace.ActiveSession()
	if !ok {
		return
	}
	fmt.Printf("%s%s%s\n", Dim, i18n.T("loop.running", "n", l.n, "prompt", safe(l.prompt)), Reset)
	runTurn(ctx, app, active.ID, l.prompt, interrupts, turnOptions{command: strings.HasPrefix(l.prompt, "/")})
}

// startGoal sets the session's goal and starts working toward it: after
// each turn a judge decides whether the agent goes on.
func startGoal(ctx context.Context, app *App, line, cond string, interrupts <-chan os.Signal) {
	active, ok := app.Workspace.ActiveSession()
	if !ok {
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.none_active"), Reset)
		return
	}
	g, err := app.Workspace.SetGoal(cond)
	if err != nil {
		fmt.Printf("%s✗ %v%s\n", Red, err, Reset)
		return
	}
	fmt.Printf("%s%s%s\n", Dim, i18n.T("goal.set", "max", g.Max), Reset)
	runTurn(ctx, app, active.ID, line, interrupts, turnOptions{prompt: "Work toward this goal, and keep going until it holds: " + cond})
}

// cmdGoal is /goal and /goal clear (a condition starts a turn: startGoal).
func cmdGoal(args []string, app *App) {
	if len(args) == 1 && args[0] == "clear" {
		if err := app.Workspace.ClearGoal(); err != nil {
			fmt.Println(i18n.T("goal.none"))
			return
		}
		fmt.Printf("%s✓ %s%s\n", Green, i18n.T("goal.cleared"), Reset)
		return
	}
	g, err := app.Workspace.Goal()
	if err != nil {
		fmt.Println(i18n.T("goal.none"))
		return
	}
	fmt.Println(i18n.T("goal.show", "condition", safe(g.Condition), "n", g.Continues, "max", g.Max))
}

// cancelOnSignal returns a context cancelled when parent is done or a signal
// arrives on sigs. Call stop (safe to call more than once, e.g. from an
// interrupt handler and again at the end of a turn) to cancel the context
// and release the watcher goroutine.
func cancelOnSignal(parent context.Context, sigs <-chan os.Signal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigs:
			cancel()
		case <-done:
		case <-ctx.Done():
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() { close(done) })
		cancel()
	}
}

// ListTasks and StopTask are the current workspace's (it changes with
// /cd): App is the exit prompt's TaskControl.
func (app *App) ListTasks() []api.TaskInfo { return app.Workspace.ListTasks() }

// StopTask stops one of the current workspace's tasks.
func (app *App) StopTask(id string) (api.TaskInfo, error) { return app.Workspace.StopTask(id) }
