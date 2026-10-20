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

package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"gopkg.in/yaml.v3"
)

// The tray's richer menu (spec_electron_038 EL-111): what's going on, at
// its top, each run with Show and Stop; then a submenu per workspace with
// its jobs and its workflows, each with Run, Stop and Show; the icons by
// name (main draws them), and the tray's icon pulsing while anything
// runs (Pulse).

// Icon names: each drawn as a character before the entry's title (Label).
const (
	IconPlay      = "play"
	IconStop      = "stop"
	IconEye       = "eye"
	IconFolder    = "folder"
	IconJob       = "job"
	IconWorkflow  = "workflow"
	IconConnector = "connector"
	DotRunning    = "dot-running"
	DotFailed     = "dot-failed"
	DotWaiting    = "dot-waiting"
	SwitchOn      = "switch-on"
	SwitchOff     = "switch-off"
)

// glyphs are the icons as text. The menu bar's menus on macOS draw no
// images the tray gives its items (checked on macOS 27, 2026-10-19: none
// of five ways showed one), so the icons are characters in the titles,
// alike on every system. The headings and the workspaces have none. U+FE0E
// asks for the text form, not an emoji.
var glyphs = map[string]string{
	IconPlay:   "▶\uFE0E",
	IconStop:   "■",
	IconEye:    "↗",
	DotRunning: "●",
	DotFailed:  "⚠\uFE0E",
	DotWaiting: "◔",
	SwitchOn:   "◉",
	SwitchOff:  "○",
}

// Label is an entry's title, its icon's character before it.
func Label(e Entry) string {
	if g := glyphs[e.Icon]; g != "" {
		return g + " " + e.Title
	}
	return e.Title
}

// Space is a workspace the tray has a submenu for.
type Space struct {
	Dir  string
	Name string
}

// Spaces are the workspaces the tray shows: those open in the app, in its
// order (named as it names them), then any other with jobs, and the home
// workspace's (the global jobs) last, as Global.
func Spaces(open []Space, workers []*pb.Worker) []Space {
	var out []Space
	seen := map[string]bool{}
	add := func(s Space) {
		if s.Dir == "" || seen[s.Dir] {
			return
		}
		seen[s.Dir] = true
		if s.Name == "" {
			s.Name = filepath.Base(s.Dir)
		}
		out = append(out, s)
	}
	home := ""
	for _, s := range open {
		if !config.IsHomeWorkspace("", s.Dir) {
			add(s)
		}
	}
	for _, w := range workers {
		if config.IsHomeWorkspace("", w.Workspace) {
			home = w.Workspace
			continue
		}
		add(Space{Dir: w.Workspace})
	}
	if home != "" {
		add(Space{Dir: home, Name: i18n.T("tray.workers.global")})
	}
	return out
}

// Flow is one of a workspace's workflows, as its file says.
type Flow struct {
	Name        string
	Description string
}

// Flows are dir's workflows (.agents/workflows/<name>.flow.yaml), by
// name, read from disk: the service needn't open the workspace for the
// tray to list them.
func Flows(dir string) []Flow {
	files, _ := filepath.Glob(filepath.Join(dir, ".agents", "workflows", "*.flow.yaml"))
	var out []Flow
	for _, f := range files {
		fl := Flow{Name: strings.TrimSuffix(filepath.Base(f), ".flow.yaml")}
		if data, err := os.ReadFile(f); err == nil {
			var head struct {
				Description string `yaml:"description"`
			}
			if yaml.Unmarshal(data, &head) == nil {
				fl.Description = strings.TrimSpace(head.Description)
			}
		}
		out = append(out, fl)
	}
	slices.SortFunc(out, func(a, b Flow) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Live is what's going on, as the service says.
type Live struct {
	// Runs are workflow runs going on and lately ended (WatchWorkflowRuns).
	Runs []*pb.WorkflowRun
	// JobRuns are jobs' runs going on (ListRunningWorkerRuns).
	JobRuns []*pb.WorkerRun
}

// going reports whether a workflow run is going on.
func going(r *pb.WorkflowRun) bool { return r.Status == "running" || r.Status == "waiting" }

// Busy reports whether anything runs: the icon pulses while it does.
func (l Live) Busy() bool {
	return len(l.JobRuns) > 0 || slices.ContainsFunc(l.Runs, going)
}

// jobFlow is the workflow run a job's run is (it shares the job run's
// session), if it runs a workflow.
func (l Live) jobFlow(jr *pb.WorkerRun) *pb.WorkflowRun {
	for _, r := range l.Runs {
		if going(r) && r.SessionId != "" && r.SessionId == jr.SessionId {
			return r
		}
	}
	return nil
}

// progress is "2/5": a run's steps done of those it has.
func progress(r *pb.WorkflowRun) string {
	if r == nil || len(r.Steps) == 0 {
		return ""
	}
	done := 0
	for _, s := range r.Steps {
		if s.Status == "done" || s.Status == "skipped" {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, len(r.Steps))
}

// since is how long ago t was, short: "40s", "3m", "1h 5m".
func since(t time.Time, now time.Time) string {
	d := now.Sub(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// Going are the menu's running entries: each job's run (with its
// workflow's progress when it runs one), then each other workflow run
// going on, named with its workspace when there's more than one; each
// opens on Show in Blitz and Stop. A run waiting on a person or a timer
// has the waiting dot.
func Going(l Live, spaces []Space, now time.Time) []Entry {
	name := spaceNamer(spaces)
	var out []Entry
	jobSessions := map[string]bool{}
	for _, jr := range l.JobRuns {
		jobSessions[jr.SessionId] = true
		parts := []string{jr.Worker, i18n.T("tray.running.job")}
		if n := name(jr.Workspace); n != "" {
			parts = append(parts, n)
		}
		if p := progress(l.jobFlow(jr)); p != "" {
			parts = append(parts, p)
		}
		if jr.Started != nil {
			parts = append(parts, since(jr.Started.AsTime(), now))
		}
		out = append(out, Entry{Title: strings.Join(parts, " · "), Icon: DotRunning, Children: []Entry{
			{Title: i18n.T("tray.show"), Icon: IconEye, Action: Action{Kind: ShowWorkers, Workspace: jr.Workspace, Name: jr.Worker}},
			{Title: i18n.T("tray.stop_run"), Icon: IconStop, Action: Action{Kind: StopRun, RunID: jr.Id}},
		}})
	}
	for _, r := range l.Runs {
		if !going(r) || (r.SessionId != "" && jobSessions[r.SessionId]) {
			continue
		}
		parts := []string{r.Workflow}
		if n := name(r.Workspace); n != "" {
			parts = append(parts, n)
		}
		if p := progress(r); p != "" {
			parts = append(parts, p)
		}
		if r.Started != nil {
			parts = append(parts, since(r.Started.AsTime(), now))
		}
		icon := DotRunning
		if r.Status == "waiting" {
			icon = DotWaiting
		}
		out = append(out, Entry{Title: strings.Join(parts, " · "), Icon: icon, Children: []Entry{
			{Title: i18n.T("tray.show"), Icon: IconEye, Action: Action{Kind: ShowWorkflow, Workspace: r.Workspace, Name: r.Workflow, RunID: r.Id}},
			{Title: i18n.T("tray.stop_run"), Icon: IconStop, Action: Action{Kind: StopWorkflow, Workspace: r.Workspace, RunID: r.Id}},
		}})
	}
	return out
}

// spaceNamer names a run's workspace when the tray shows more than one
// ("" when it shows one or none: it goes without saying).
func spaceNamer(spaces []Space) func(dir string) string {
	return func(dir string) string {
		if len(spaces) <= 1 {
			return ""
		}
		for _, s := range spaces {
			if s.Dir == dir {
				return s.Name
			}
		}
		return filepath.Base(dir)
	}
}

// SpaceEntries are a workspace's submenu: Jobs, each job with Run now,
// Stop while it runs, Enabled and Show in Blitz, marked running or failed;
// then Workflows, each with Run, Stop while it runs and Show in Blitz,
// marked running; then Connectors, each connection it reaches checked
// while it's on there (connEntries). A section with nothing in it is left
// out, and a workspace with none has no submenu (none).
func SpaceEntries(s Space, workers []*pb.Worker, flows []Flow, conns []Conn, l Live) []Entry {
	return append(jobAndFlowEntries(s, workers, flows, l), connEntries(s, conns)...)
}

// jobAndFlowEntries are a workspace's Jobs and Workflows sections.
func jobAndFlowEntries(s Space, workers []*pb.Worker, flows []Flow, l Live) []Entry {
	var out []Entry
	for _, w := range workers {
		if w.Workspace != s.Dir {
			continue
		}
		if len(out) == 0 {
			out = append(out, Entry{Title: i18n.T("tray.jobs"), Icon: IconJob, Disabled: true})
		}
		out = append(out, jobEntry(w, l))
	}
	if len(flows) == 0 {
		return out
	}
	// No line between the sections: a submenu's lines can't be taken away as it changes; the headings part them.
	out = append(out, Entry{Title: i18n.T("tray.workflows"), Icon: IconWorkflow, Disabled: true})
	for _, f := range flows {
		var run *pb.WorkflowRun
		for _, r := range l.Runs {
			if going(r) && r.Workspace == s.Dir && r.Workflow == f.Name {
				run = r
				break
			}
		}
		e := Entry{Title: f.Name, Children: []Entry{{Title: i18n.T("tray.run"), Icon: IconPlay, Action: Action{Kind: RunWorkflow, Workspace: s.Dir, Name: f.Name}}}}
		show := Action{Kind: ShowWorkflow, Workspace: s.Dir, Name: f.Name}
		if run != nil {
			e.Icon = DotRunning
			if p := progress(run); p != "" {
				e.Title += " · " + p
			}
			e.Children = append(e.Children, Entry{Title: i18n.T("tray.stop_run"), Icon: IconStop, Action: Action{Kind: StopWorkflow, Workspace: s.Dir, RunID: run.Id}})
			show.RunID = run.Id
		}
		e.Children = append(e.Children, Entry{Title: i18n.T("tray.show"), Icon: IconEye, Action: show})
		out = append(out, e)
	}
	return out
}

// jobEntry is a job's item: marked running or failed (its last run), with
// when it runs next, or "off". Its switch says what it is and what a click
// does, first: On (it runs on its schedule) turns it off, Off (it doesn't run,
// not even by hand) turns it on, at the content reviewed; a job not yet
// reviewed (new, changed, invalid) is reviewed in the app. Run now only
// while it's on: the service refuses a job that's off.
func jobEntry(w *pb.Worker, l Live) Entry {
	enabled := w.State == pb.WorkerState_WORKER_STATE_ENABLED
	reviewed := enabled || w.State == pb.WorkerState_WORKER_STATE_DISABLED
	var toggle Entry
	switch {
	case enabled:
		toggle = Entry{Title: i18n.T("tray.job.on"), Icon: SwitchOn, Action: Action{Kind: DisableWorker, Workspace: w.Workspace, Name: w.Name}}
	case reviewed:
		toggle = Entry{Title: i18n.T("tray.job.off"), Icon: SwitchOff, Action: Action{Kind: EnableWorker, Workspace: w.Workspace, Name: w.Name, Hash: w.Hash}}
	default:
		toggle = Entry{Title: i18n.T("tray.job.review"), Icon: IconEye, Action: Action{Kind: ShowWorkers, Workspace: w.Workspace}}
	}
	title := w.Name
	switch {
	case enabled && w.NextRun != nil:
		title += " · " + i18n.T("tray.next", "when", w.NextRun.AsTime().Local().Format("Mon 15:04"))
	case !enabled:
		title += " · " + i18n.T("tray.job.off_short")
	}
	// Its state first, then what it does: Run, Stop while it runs, Open (the owner's order).
	e := Entry{Title: title, Children: []Entry{toggle, {Title: i18n.T("tray.run"), Icon: IconPlay, Disabled: !enabled, Action: Action{Kind: RunWorker, Workspace: w.Workspace, Name: w.Name}}}}
	if w.LastRun != nil && w.LastRun.Status == pb.RunStatus_RUN_STATUS_FAILED {
		e.Icon = DotFailed
	}
	for _, jr := range l.JobRuns {
		if jr.Workspace == w.Workspace && jr.Worker == w.Name {
			e.Icon = DotRunning
			e.Children = append(e.Children, Entry{Title: i18n.T("tray.stop_run"), Icon: IconStop, Action: Action{Kind: StopRun, RunID: jr.Id}})
			break
		}
	}
	e.Children = append(e.Children, Entry{Title: i18n.T("tray.show"), Icon: IconEye, Action: Action{Kind: ShowWorkers, Workspace: w.Workspace, Name: w.Name}})
	return e
}

// WorkflowLink is the blitz:// link that opens the app on a workflow's
// canvas, and on one of its runs when run is given.
func WorkflowLink(workspace, workflow, run string) string {
	q := url.Values{"dir": {workspace}, "workflow": {workflow}}
	if run != "" {
		q.Set("run", run)
	}
	return "blitz://workflow?" + q.Encode()
}

// PulseFrames are the icon's brightness while something runs, one frame
// after another, a breath of about a second and a half.
var PulseFrames = []float64{1, 0.82, 0.62, 0.45, 0.62, 0.82}

// Pulse is a PNG icon at a brightness: its alpha scaled by alpha.
func Pulse(icon []byte, alpha float64) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	img := image.NewNRGBA(b)
	draw.Draw(img, b, src, b.Min, draw.Src)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = uint8(float64(img.Pix[i]) * alpha)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
