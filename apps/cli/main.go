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

// Command blitz is Blitz's command line: an interactive session (the REPL)
// in the current directory, a one-shot run with a prompt (`blitz "…"`,
// `blitz exec`), and the commands around them: doctor, config, service,
// workers and license. It attaches to the Blitz service when one is
// running, so the terminal and the desktop app share workspaces; --local
// runs the workspace in-process instead (spec_cli_020).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/retail-cortex/blitz/pkg/socket"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// version is set when a release is built (the git tag).
var version = "dev"

// rootOptions are the flags of the main (prompt/REPL) command.
type rootOptions struct {
	global       globalFlags
	prompt       string
	interactive  bool
	version      bool
	resume       string
	cont         bool
	outputFormat string
	inputFormat  string
	jsonSchema   string
	noPersist    bool   // --no-session-persistence
	fork         bool   // --fork: continue a copy of the resumed session
	name         string // --name: the new session's title
	// appendPrompt and appendPromptFile add to the agent's instructions
	// for the run (--append-system-prompt[-file]).
	appendPrompt, appendPromptFile string
	maxTurns                       int
	maxCostUSD                     float64
	timeout                        time.Duration
	plan                           bool
	images                         []string
	local                          bool
	mode                           string // --permission-mode
	effort                         string // --effort
	allowRules                     []string
	denyRules                      []string
	// requirePrompt: a run without a prompt is a usage error (exec),
	// rather than the interactive session.
	requirePrompt bool
	// sendPrompt, if set, is sent to the agent instead of the prompt,
	// which the transcript records (blitz init records "/init").
	sendPrompt string
}

func main() {
	// Until engine.StartObservability installs the log file, slog's default would
	// print to stderr and duplicate the terminal warnings.
	slog.SetDefault(slog.New(slog.DiscardHandler))
	root := newRootCommand()
	err := root.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("repl.error", "error", err))
	}
	os.Exit(exitCodeFor(err))
}

func newRootCommand() *cobra.Command {
	o := &rootOptions{}
	root := &cobra.Command{
		Use:   "blitz [flags] [prompt...]",
		Short: "Blitz - autonomous AI coding agent built on Google ADK",
		Long: `Blitz is an AI coding agent. Run it with no arguments for an interactive
session, or pass a prompt to run once and exit.

Exit codes: 0 success, 1 error, 2 usage, 3 --max-turns reached,
4 prompt blocked by a hook, 130 interrupted.`,
		Example: `  blitz                          # interactive
  blitz -d ~/src/app "fix the failing test"
  git diff | blitz -p - --output-format json
  blitz --continue "now add docs"`,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoot(cmd, o, args)
		},
	}
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		//nolint:ST1005 // shown to the user as is, after the flag error
		return withCode(exitUsage, fmt.Errorf("%w\nRun '%s --help' for usage.", err, c.CommandPath()))
	})

	pf := root.PersistentFlags()
	pf.StringVarP(&o.global.config, "config", "c", "", "Directory containing .env.toml config (default ~/.blitz)")
	pf.StringVarP(&o.global.dir, "dir", "d", "", "Workspace directory to start in (default: current directory)")

	f := root.Flags()
	f.BoolVarP(&o.interactive, "interactive", "i", false, "Start the interactive REPL even when a prompt is given")
	f.BoolVarP(&o.version, "version", "v", false, "Print Blitz version")
	addRunFlags(f, o)

	root.AddCommand(newExecCommand(o), newInitCommand(o), newDoctorCommand(&o.global), newConfigCommand(&o.global), newServeCommand(), newWorkersCommand(&o.global), newServiceCommand(), newLicenseCommand(), newTrustCommand(&o.global), newMCPCommand(&o.global), newWorktreesCommand(&o.global), newMemoryCommand(&o.global), newPluginCommand(), newModelsCommand(&o.global), newUpdateCommand(), newSessionsCommand(&o.global))
	return root
}

// addRunFlags adds the flags of a run, shared by the root command and exec.
func addRunFlags(f *pflag.FlagSet, o *rootOptions) {
	f.StringVarP(&o.prompt, "prompt", "p", "", `One-shot prompt; "-" reads it from stdin`)
	f.StringVarP(&o.global.agent, "agent", "a", "", "Agent to activate (blitz, helios, qa, ...)")
	f.StringVarP(&o.global.model, "model", "m", "", "Model identifier to use")
	f.StringVar(&o.global.agency, "agency", "", "Agency level (low, medium, high, extreme)")
	f.BoolVar(&o.global.trustProject, "trust-project", false, "Trust the workspace's project settings (.blitz/settings.toml) for this run, without recording it")
	f.BoolVar(&o.global.trustProject, "trust-workspace", false, "Deprecated: --trust-project")
	_ = f.MarkHidden("trust-workspace")
	f.StringVarP(&o.global.worktree, "worktree", "w", "", "Start in a new git worktree (.blitz/worktrees/<name>, branch blitz/<name>); no name: one is made up")
	f.Lookup("worktree").NoOptDefVal = "new"
	f.StringArrayVar(&o.global.pluginDirs, "plugin-dir", nil, "Load the plugin in this directory for the run (repeatable; runs without the service)")
	f.StringVar(&o.global.ref, "ref", "", "With --worktree: the commit or branch to start from (default HEAD)")
	f.StringVarP(&o.resume, "resume", "r", "", "Resume a saved session by ID (no ID: the most recent), or start a new one from a snapshot by name")
	f.Lookup("resume").NoOptDefVal = "latest"
	f.BoolVarP(&o.cont, "continue", "C", false, "Continue the most recent session")
	f.StringVar(&o.outputFormat, "output-format", formatText, "Output for one-shot runs: text, json, or stream-json")
	f.StringVar(&o.inputFormat, "input-format", inputText, "Input: text, or stream-json (JSON lines on stdin: prompts, approval answers and question answers; needs --output-format stream-json)")
	f.StringVar(&o.jsonSchema, "json-schema", "", "One-shot runs answer with JSON valid against this schema (a file, or inline JSON), as structured_result")
	f.StringVar(&o.appendPrompt, "append-system-prompt", "", "Add this to the agent's instructions for the run (runs without the service)")
	f.StringVar(&o.appendPromptFile, "append-system-prompt-file", "", "Add this file's text to the agent's instructions for the run (runs without the service)")
	f.BoolVar(&o.fork, "fork", false, "With --resume or --continue: continue in a copy of the session, leaving it as it was")
	f.StringVar(&o.name, "name", "", "Name the new session")
	f.BoolVar(&o.noPersist, "no-session-persistence", false, "Don't keep the run's session (one-shot runs; the audit log is still written)")
	f.IntVar(&o.maxTurns, "max-turns", 0, "Stop after this many model calls in a one-shot run (0 = unlimited)")
	f.Float64Var(&o.maxCostUSD, "max-cost-usd", 0, "Stop a one-shot run once it has cost more than this, in USD (0 = unlimited)")
	f.DurationVar(&o.timeout, "timeout", 0, "Stop a one-shot run after this long, e.g. 10m (0 = unlimited)")
	f.BoolVar(&o.plan, "plan", false, "One-shot plan: the agent may read and search but not edit or run commands")
	f.BoolVar(&o.local, "local", false, "Run the workspace in this process even when the Blitz service is running")
	f.StringArrayVar(&o.allowRules, "allow", nil, `Allow an action without asking, for this run: a rule such as "shell(go test *)" or "write(docs/**)" (repeatable)`)
	f.StringArrayVar(&o.denyRules, "deny", nil, `Refuse an action, for this run: a rule such as "shell(git push *)" or "web(*.internal)" (repeatable)`)
	f.StringVar(&o.effort, "effort", "", "Reasoning effort for this run: minimal, low, medium, high, or max (where the model supports it)")
	f.StringVar(&o.mode, "permission-mode", "", "Start in a permission mode: default, accept-edits, plan, dont-ask, or bypass (needs the OS sandbox)")
	f.StringArrayVar(&o.images, "image", nil, "Attach an image to the first prompt (repeatable); @file.png in a prompt also works")
}

// newInitCommand is `blitz init`: the agent writes or updates BLITZ.md, as
// /init does in a session.
func newInitCommand(root *rootOptions) *cobra.Command {
	o := &rootOptions{requirePrompt: true, outputFormat: formatText}
	return &cobra.Command{
		Use:   "init",
		Short: "Have the agent write or update BLITZ.md, the project's instructions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.global = root.global // persistent flags (--dir, --config)
			o.prompt, o.sendPrompt = "/init", api.InitPrompt()
			return runRoot(cmd, o, nil)
		},
	}
}

// newExecCommand is `blitz exec <prompt>`: one run, then exit, as
// `blitz <prompt>` does, but never the interactive session.
func newExecCommand(root *rootOptions) *cobra.Command {
	o := &rootOptions{requirePrompt: true}
	cmd := &cobra.Command{
		Use:   "exec [flags] <prompt...>",
		Short: "Run one prompt and exit",
		Example: `  blitz exec "add unit tests for user_service.go covering edge cases"
  git diff | blitz exec -p - --output-format json`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			o.global.config, o.global.dir = root.global.config, root.global.dir // persistent flags
			return runRoot(cmd, o, args)
		},
	}
	addRunFlags(cmd.Flags(), o)
	return cmd
}

func runRoot(cmd *cobra.Command, o *rootOptions, args []string) (err error) {
	if o.version {
		fmt.Printf("Blitz Go (Google ADK) version %s\n", version)
		return nil
	}
	switch o.outputFormat {
	case formatText, formatJSON, formatStreamJSON:
	default:
		return withCode(exitUsage, fmt.Errorf("invalid --output-format %q (use text, json, or stream-json)", o.outputFormat))
	}
	if o.maxTurns < 0 {
		return withCode(exitUsage, errors.New("--max-turns must be >= 0"))
	}
	if _, err := api.ParsePermissionMode(o.mode); err != nil {
		return withCode(exitUsage, fmt.Errorf("--permission-mode: %w", err))
	}
	if o.effort != "" {
		if _, err := config.ParseEffort(o.effort); err != nil {
			return withCode(exitUsage, fmt.Errorf("--effort: %w", err))
		}
	}
	if o.maxCostUSD < 0 || o.timeout < 0 {
		return withCode(exitUsage, errors.New("--max-cost-usd and --timeout must be >= 0"))
	}

	streamIn := false
	switch o.inputFormat {
	case inputText, "":
	case inputStreamJSON:
		if o.outputFormat != formatStreamJSON || o.interactive {
			return withCode(exitUsage, errors.New("--input-format stream-json needs --output-format stream-json (and no -i)"))
		}
		streamIn = true
	default:
		return withCode(exitUsage, fmt.Errorf("invalid --input-format %q (use text or stream-json)", o.inputFormat))
	}
	var schema *answerSchema
	if o.jsonSchema != "" {
		var err error
		if schema, err = loadSchema(o.jsonSchema); err != nil {
			return withCode(exitUsage, fmt.Errorf("--json-schema: %w", err))
		}
	}

	stdinTTY := tui.StdinIsTerminal()
	// stream-json input keeps stdin for its lines.
	prompt, stdinUsed, err := resolvePrompt(o.prompt, args, stdinTTY || streamIn, o.interactive, os.Stdin)
	if err != nil {
		return err
	}
	if o.requirePrompt && prompt == "" && !streamIn {
		return withCode(exitUsage, errors.New("no prompt: give one as arguments, with -p, or on stdin"))
	}
	oneShot := (prompt != "" || streamIn) && !o.interactive
	if !oneShot && (schema != nil || o.noPersist) {
		return withCode(exitUsage, errors.New("--json-schema and --no-session-persistence are for one-shot runs and require a prompt"))
	}
	if o.fork && o.resume == "" && !o.cont {
		return withCode(exitUsage, errors.New("--fork copies the session --resume or --continue picks"))
	}
	if o.name != "" && (o.resume != "" || o.cont) && !o.fork {
		return withCode(exitUsage, errors.New("--name names a new session: not with --resume or --continue (unless --fork)"))
	}
	if o.noPersist && (o.resume != "" || o.cont) {
		return withCode(exitUsage, errors.New("--no-session-persistence starts a new session: not with --resume or --continue"))
	}
	if !oneShot && o.plan {
		return withCode(exitUsage, errors.New("--plan requires a prompt (in a session, use /plan <goal>)"))
	}
	if !oneShot && (o.maxTurns > 0 || o.maxCostUSD > 0 || o.timeout > 0) {
		return withCode(exitUsage, errors.New("--max-turns, --max-cost-usd and --timeout limit a one-shot run and require a prompt"))
	}
	if !oneShot && o.outputFormat != formatText {
		return withCode(exitUsage, errors.New("--output-format json/stream-json requires a prompt"))
	}

	// SIGTERM (and Ctrl+C in one-shot mode) cancels in-flight model calls and
	// tool processes so deferred cleanup runs. The REPL handles Ctrl+C itself.
	sigs := []os.Signal{syscall.SIGTERM}
	if oneShot {
		sigs = append(sigs, os.Interrupt)
	}
	ctx, stop := signal.NotifyContext(context.Background(), sigs...)
	defer stop()

	if o.global.worktree != "" {
		if err := enterWorktree(&o.global); err != nil {
			return err
		}
	}
	cfg, err := loadConfig(&o.global)
	if err != nil {
		return err
	}
	appendPrompt := o.appendPrompt
	if o.appendPromptFile != "" {
		data, err := os.ReadFile(config.ExpandHome(o.appendPromptFile))
		if err != nil {
			return withCode(exitUsage, fmt.Errorf("--append-system-prompt-file: %w", err))
		}
		appendPrompt = strings.TrimSpace(appendPrompt + "\n\n" + string(data))
	}
	if appendPrompt != "" {
		o.local = true // the service's workspace has its own instructions
	}
	if o.noPersist { // sessions in a folder removed at exit, in this process
		tmp, err := os.MkdirTemp("", "blitz-sessions-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		cfg.Session.StorageDir = tmp
		o.local = true
	}

	textMode := o.outputFormat == formatText
	pretty := textMode && stdoutIsTerminal()
	warnOut := os.Stderr
	warnFn := func(msg string) {
		slog.Warn(msg)
		fmt.Fprintf(warnOut, "%s!  %s%s\n", tui.Yellow, msg, tui.Reset)
	}
	defer engine.StartObservability(ctx, cfg, version, warnFn)()
	defer func() { // runs before the log closes
		if err != nil {
			slog.Error("exit", "code", exitCodeFor(err), "error", err)
		}
	}()

	// With nobody to ask (a prompt, a pipe), project settings that need
	// trust stay off, and a line says so.
	askTrust := func(dir string) func(api.ProjectSettings) string {
		if !stdinTTY || oneShot || stdinUsed {
			return nil
		}
		return func(p api.ProjectSettings) string {
			return tui.AskTrust(ctx, tui.NewLineReader(os.Stdin, os.Stdout), os.Stdout, dir, p)
		}
	}
	w, locales, remote, err := openBackend(ctx, cfg, backendOptions{local: o.local, streaming: pretty, trustProject: o.global.trustProject, appendPrompt: appendPrompt, askTrust: askTrust(cfg.Tools.WorkspaceDir)}, warnFn)
	if err != nil {
		return err
	}
	if notice := tui.ProjectNotice(w.ProjectSettings()); notice != "" {
		if tui.NeedsTrustDecision(w.ProjectSettings()) || oneShot {
			warnFn(notice)
		} else {
			fmt.Fprintf(os.Stderr, "%s%s%s\n", tui.Dim, notice, tui.Reset)
		}
	}
	if remote && !oneShot {
		fmt.Fprintf(os.Stderr, "%s%s%s\n", tui.Dim, i18n.T("startup.attached", "socket", socket.DefaultSocket()), tui.Reset)
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if o.mode != "" {
		if _, err := w.SetPermissionMode(o.mode); err != nil {
			return withCode(exitUsage, fmt.Errorf("--permission-mode: %w", err))
		}
	}
	if o.effort != "" {
		if _, err := w.Set(ctx, "effort", o.effort); err != nil {
			return withCode(exitUsage, fmt.Errorf("--effort: %w", err))
		}
	}
	for effect, rules := range map[string][]string{"allow": o.allowRules, "deny": o.denyRules} {
		for _, rule := range rules {
			if _, err := w.AddPermissionRule(effect, rule, api.ScopeSession); err != nil {
				return withCode(exitUsage, fmt.Errorf("--%s %q: %w", effect, rule, err))
			}
		}
	}
	if merr := w.ModelErr(); merr != nil {
		msg := i18n.T("startup.model_failed", "error", engine.ModelErrorSummary(merr, cfg))
		if oneShot {
			// Every turn would fail; say why at once, with an exit code.
			return withCode(exitFailure, fmt.Errorf("%s (run 'blitz doctor')", msg))
		}
		warnFn(msg)
		warnFn(i18n.T("startup.placeholder_model"))
	}

	var completer *tui.Completer
	// One input source for everything read from the terminal.
	var input tui.Input
	switch {
	case stdinUsed, streamIn:
		// stdin carried the prompt, or carries stream-json lines (their
		// answers are set up with the run).
	case stdinTTY && !oneShot:
		completer = newCompleter(w)
		ti, terr := tui.NewTerminalInput(tui.TerminalOptions{
			HistoryFile: config.ExpandHome(cfg.UI.HistoryFile),
			HistorySize: cfg.UI.HistorySize,
			Completer:   completer,
		})
		if terr != nil {
			warnFn(i18n.T("startup.line_editor", "error", terr.Error()))
			input = tui.NewLineReader(os.Stdin, os.Stdout)
		} else {
			defer ti.Close()
			input = ti
		}
	default:
		promptOut := io.Writer(os.Stdout)
		if !textMode {
			promptOut = os.Stderr // keep stdout pure JSON
		}
		input = tui.NewLineReader(os.Stdin, promptOut)
	}
	if input != nil {
		n := notifier(cfg, pretty)
		w.SetUI(n.Approving(tui.NewApprover(input, cfg.UI.DiffLines)), n.Asking(tui.NewUserPrompter(input)))
	}

	attachPrompt := ""
	if oneShot {
		attachPrompt = prompt
	}
	attached, err := w.LoadAttachments(o.images, attachPrompt, warnFn)
	if err != nil {
		return withCode(exitUsage, fmt.Errorf("--image %w", err))
	}

	sess, resumed, err := w.OpenSession(o.resume, o.cont)
	if err != nil {
		var re *api.ResumeError
		if errors.As(err, &re) {
			return withCode(exitUsage, err)
		}
		return err
	}
	if o.fork && resumed {
		if sess, err = w.ForkSession(ctx, 0); err != nil {
			return fmt.Errorf("--fork: %w", err)
		}
	}
	if o.name != "" {
		if sess, err = w.RenameSession(o.name); err != nil {
			return withCode(exitUsage, fmt.Errorf("--name: %w", err))
		}
	}

	if oneShot {
		if model := w.Model().Name; o.maxCostUSD > 0 && !runtime.NewUsageTracker(cfg.Pricing).HasPrice(model) {
			warnFn(i18n.T("startup.cost_unpriced", "model", model))
		}
		run := oneShotOptions{
			prompt: prompt, sessionID: sess.ID, format: o.outputFormat, maxTurns: o.maxTurns, plan: o.plan,
			maxCostUSD: o.maxCostUSD, timeout: o.timeout, sendPrompt: o.sendPrompt,
			input: input, stdinTTY: stdinTTY && !stdinUsed && !streamIn, stdout: os.Stdout,
			markdown: pretty && cfg.UI.Markdown, spinner: pretty && cfg.UI.Spinner, width: terminalWidth(),
			usageLines: pretty, images: attached, theme: cfg.UI.Theme, schema: schema,
		}
		if streamIn {
			return runStreamInput(ctx, w, run, os.Stdin)
		}
		return runOneShot(ctx, w, run)
	}

	if resumed && sess.Workspace != "" && sess.Workspace != w.Dir() {
		warnFn(i18n.T("resume.other_workspace_id", "id", sess.ID, "workspace", sess.Workspace))
	}
	if resumed {
		msg := i18n.T("resume.starting", "id", sess.ID, "messages", i18n.N("session.messages", sess.MessageCount))
		if o.resume != "" && o.resume != "latest" && o.resume != sess.ID { // a snapshot name
			msg = i18n.T("snapshot.branched", "id", sess.ID, "name", o.resume, "messages", i18n.N("session.messages", sess.MessageCount))
		}
		fmt.Printf("%s%s%s\n", tui.Green, msg, tui.Reset)
		tui.PrintRecap(sess.Messages, 3)
		fmt.Println()
	}
	return tui.RunREPL(ctx, &tui.App{
		Workspace:     w,
		Locales:       locales,
		Version:       version,
		Input:         input,
		Attachments:   attached,
		TerminalTitle: pretty && cfg.UI.TerminalTitle,
		DiffLines:     cfg.UI.DiffLines,
		Attached:      remote,
		ConfigDir:     o.global.config,
		Notify:        notifier(cfg, pretty),
		// /cd: the target opens as a workspace does at start (its
		// settings, the trust question, the service when it runs); the
		// REPL switches to it once the session has moved, and the old one
		// closes.
		Cd: func(ctx context.Context, dir string) (api.Backend, *i18n.Bundle, func(), error) {
			g := o.global
			g.dir = dir
			next, err := loadConfig(&g)
			if err != nil {
				return nil, nil, nil, err
			}
			nb, loc, _, err := openBackend(ctx, next, backendOptions{local: o.local, streaming: pretty, trustProject: o.global.trustProject, appendPrompt: appendPrompt, askTrust: askTrust(next.Tools.WorkspaceDir)}, warnFn)
			if err != nil {
				return nil, nil, nil, err
			}
			if input != nil {
				n := notifier(next, pretty)
				nb.SetUI(n.Approving(tui.NewApprover(input, next.UI.DiffLines)), n.Asking(tui.NewUserPrompter(input)))
			}
			return nb, loc, func() {
				w, cfg = nb, next // what closes at exit, and prices the rest
				if completer != nil {
					completer.SetWorkspace(nb.Dir())
				}
			}, nil
		},
		Printer: tui.PrinterOptions{
			Out: os.Stdout, Markdown: pretty && cfg.UI.Markdown, Theme: cfg.UI.Theme,
			Width: terminalWidth(), Spinner: pretty && cfg.UI.Spinner,
		},
	})
}

// resolvePrompt returns the prompt from -p, arguments, or stdin. stdin is
// read when -p is "-" or when it is piped and no prompt was given.
func resolvePrompt(flag string, args []string, stdinTTY, interactive bool, stdin io.Reader) (prompt string, stdinUsed bool, err error) {
	switch {
	case flag == "-" || (flag == "" && !stdinTTY && !interactive):
		data, err := io.ReadAll(io.LimitReader(stdin, 10<<20))
		if err != nil {
			return "", false, fmt.Errorf("read prompt from stdin: %w", err)
		}
		piped := strings.TrimSpace(string(data))
		// Arguments frame the piped content: `git diff | blitz review this`.
		switch {
		case len(args) > 0 && piped != "":
			prompt = strings.Join(args, " ") + "\n\n" + piped
		case len(args) > 0:
			prompt = strings.Join(args, " ")
		default:
			prompt = piped
		}
		if prompt == "" {
			// stdin is consumed, so there is nothing left to drive a REPL.
			return "", true, withCode(exitUsage, errors.New("no prompt: stdin was empty (use -i for an interactive session)"))
		}
		return prompt, true, nil
	case flag != "":
		if len(args) > 0 {
			return "", false, withCode(exitUsage, errors.New("give the prompt either with -p or as arguments, not both"))
		}
		return flag, false, nil
	default:
		return strings.Join(args, " "), false, nil
	}
}

// newCompleter registers slash commands and dynamic argument sources.
func newCompleter(w api.Backend) *tui.Completer {
	c := tui.NewCompleter(w.Dir())
	for _, cmd := range []string{"help", "agents", "model", "skills", "session", "set", "clear", "sandbox", "exit", "quit",
		"undo", "checkpoints", "diff", "cost", "context", "compact", "memory", "approvals", "hooks", "goal", "loop", "style", "fork", "export", "mcp", "resume", "locale", "attach", "paste",
		"tools", "plan", "show", "init", "mode", "permissions", "effort", "rewind", "pin_model", "unpin", "model_settings", "search", "btw", "rename", "envs", "tasks", "cd", "trust", "license", "status", "copy", "config"} {
		c.Command(cmd)
	}
	c.Command("skills", "list", "show", "search")
	c.Command("session", "list", "new", "load", "save")
	c.Command("search", "web", "session")
	c.Command("envs", "prune", "remove")
	c.Command("license", "full", "third-party")
	c.Command("memory", "show", "reload", "add", "notes", "forget")
	c.Command("approvals", "revoke", "clear")
	c.Command("goal", "clear")
	c.Command("loop", "stop")
	c.Command("diff", "git")
	c.Command("attach", "clear")
	c.Command("undo", "--force")
	c.Command("compact")
	c.Command("set", "agency=")
	c.Command("mode", "default", "accept-edits", "auto", "plan", "dont-ask", "bypass")
	c.Command("permissions", "allow", "ask", "deny", "remove")
	c.Command("effort", "minimal", "low", "medium", "high", "max", "auto")
	c.Command("rewind", "--force")
	for _, cmd := range w.ListCommands() { // custom commands, as they are at startup
		c.Command(cmd.Name)
	}
	agentNames := func() []string {
		var names []string
		for _, a := range w.ListAgents() {
			names = append(names, a.Name)
		}
		return names
	}
	c.Dynamic("agent", agentNames)
	c.Dynamic("pin_model", agentNames)
	c.Dynamic("unpin", func() []string {
		var names []string
		for _, a := range w.ListAgents() {
			if a.PinnedModel != "" {
				names = append(names, a.Name)
			}
		}
		return names
	})
	// Models in use (the main one and any agent's) and those with settings.
	c.Dynamic("model_settings", func() []string {
		names := []string{w.Model().Name}
		for _, a := range w.ListAgents() {
			if a.PinnedModel != "" {
				names = append(names, a.PinnedModel)
			}
		}
		for m := range w.AllModelSettings() {
			names = append(names, m)
		}
		return names
	})
	c.Dynamic("locale", func() []string {
		var tags []string
		list, _ := w.AvailableLocales()
		for _, m := range list {
			tags = append(tags, m.Tag)
		}
		return tags
	})
	c.Dynamic("resume", func() []string {
		var ids []string
		if list, err := w.ListSessions(false); err == nil {
			for i, s := range list {
				if i == 20 {
					break
				}
				ids = append(ids, s.ID)
			}
		}
		if all, err := w.ListSessions(true); err == nil {
			for _, s := range all {
				if s.Snapshot != "" {
					ids = append(ids, s.Snapshot)
				}
			}
		}
		return ids
	})
	return c
}

// notifier is how the REPL tells you a long turn ended or waits
// (ui.notify_after, ui.notify); only on a terminal.
func notifier(cfg *config.Config, terminal bool) tui.Notifier {
	if !terminal {
		return tui.Notifier{Mode: "off"}
	}
	return tui.Notifier{After: time.Duration(cfg.UI.NotifyAfter) * time.Second, Mode: cfg.UI.Notify}
}
