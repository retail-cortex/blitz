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
	"io"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
)

// stubBackend is a workspace with some answers replaced, for the outputs
// and failures a real workspace rarely gives. A nil func falls through to
// the real one.
type stubBackend struct {
	api.Backend

	activeSession    func() (api.SessionInfo, bool)
	newSession       func() (api.SessionInfo, error)
	listSessions     func(all bool) ([]api.SessionInfo, error)
	loadSession      func(ref string) (api.SessionInfo, bool, error)
	saveSnapshot     func(name string, force bool) (api.SessionInfo, error)
	renameSession    func(title string) (api.SessionInfo, error)
	forkSession      func(turn int) (api.SessionInfo, error)
	exportSession    func(id string) (string, error)
	undo             func(force bool) (api.UndoResult, error)
	listCheckpoints  func() []api.Checkpoint
	gitDiff          func() (string, error)
	sessionDiff      func() string
	sessionUsage     func() (api.Usage, error)
	contextInfo      func() (api.ContextInfo, error)
	compact          func(focus string) (api.CompactResult, error)
	reloadMemory     func() ([]string, error)
	addMemory        func(text string) (string, error)
	listNotes        func() ([]api.Note, error)
	forgetNote       func(name string) error
	set              func(key, value string) (string, error)
	listStyles       func() []api.StyleInfo
	listHooks        func() []api.HookInfo
	listMCPServers   func() []api.MCPServer
	listAgents       func() []api.AgentInfo
	pinModel         func(agent, ref string) (api.PinResult, error)
	unpin            func(agent string) (api.PinResult, error)
	activeAgentTools func() api.AgentTools
	availableLocales func() ([]api.LocaleInfo, string)
	setLocale        func(input string) (api.LocaleChange, error)
	rewindPoints     func() ([]api.RewindPoint, error)
	rewind           func(index int, mode api.RewindMode, force bool) (api.RewindResult, error)
	setGoal          func(cond string) (api.Goal, error)
	goal             func() (api.Goal, error)
	clearGoal        func() error
	run              func(ctx context.Context, t api.Turn, on func(api.Event)) (api.TurnResult, error)
	steer            func(text string) error
	listTasks        func() []api.TaskInfo
	stopTask         func(id string) (api.TaskInfo, error)
	takeNotices      func() []string
	task             func(id string) (api.TaskInfo, []string, error)
	pendingRequests  func() []api.TaskRequest
	answerRequest    func(id string, d api.Decision, answer string) error
	setModel         func(ref string) (string, error)
	setAgent         func(name string) (api.AgentInfo, error)
	setPermission    func(mode string) (string, error)
	settings         func() api.Settings
	modelErr         func() error
	sandboxSummary   func() []string
	projectSettings  func() api.ProjectSettings
	trustProject     func(hash string, trusted bool) error
	searchSkills     func(q string) []api.SkillInfo
	listSkills       func() []api.SkillInfo
	skill            func(name string) (api.SkillInfo, bool)
	listEnvs         func() ([]api.Env, error)
	removeEnv        func(key string) error
	pruneEnvs        func() (api.PruneResult, error)
	searchProvider   func() (string, error)
	searchWeb        func(terms string) (api.WebSearch, error)
	addPermission    func(effect, rule string, save api.Scope) (api.PermissionChange, error)
	removePermission func(rule string, save api.Scope) (api.PermissionChange, error)
	modelSettings    func(ref string) (api.ModelSettingsInfo, error)
	allModelSettings func() map[string]config.ModelSettings
	updateSettings   func(ref string, reset bool, changes []api.Setting) (api.ModelSettingsChange, error)
}

func (s *stubBackend) ActiveSession() (api.SessionInfo, bool) {
	if s.activeSession != nil {
		return s.activeSession()
	}
	return s.Backend.ActiveSession()
}

func (s *stubBackend) NewSession() (api.SessionInfo, error) {
	if s.newSession != nil {
		return s.newSession()
	}
	return s.Backend.NewSession()
}

func (s *stubBackend) ListSessions(all bool) ([]api.SessionInfo, error) {
	if s.listSessions != nil {
		return s.listSessions(all)
	}
	return s.Backend.ListSessions(all)
}

func (s *stubBackend) LoadSession(ref string) (api.SessionInfo, bool, error) {
	if s.loadSession != nil {
		return s.loadSession(ref)
	}
	return s.Backend.LoadSession(ref)
}

func (s *stubBackend) SaveSnapshot(name string, force bool) (api.SessionInfo, error) {
	if s.saveSnapshot != nil {
		return s.saveSnapshot(name, force)
	}
	return s.Backend.SaveSnapshot(name, force)
}

func (s *stubBackend) RenameSession(title string) (api.SessionInfo, error) {
	if s.renameSession != nil {
		return s.renameSession(title)
	}
	return s.Backend.RenameSession(title)
}

func (s *stubBackend) ForkSession(ctx context.Context, turn int) (api.SessionInfo, error) {
	if s.forkSession != nil {
		return s.forkSession(turn)
	}
	return s.Backend.ForkSession(ctx, turn)
}

func (s *stubBackend) ExportSession(id string) (string, error) {
	if s.exportSession != nil {
		return s.exportSession(id)
	}
	return s.Backend.ExportSession(id)
}

func (s *stubBackend) Undo(force bool) (api.UndoResult, error) {
	if s.undo != nil {
		return s.undo(force)
	}
	return s.Backend.Undo(force)
}

func (s *stubBackend) ListCheckpoints() []api.Checkpoint {
	if s.listCheckpoints != nil {
		return s.listCheckpoints()
	}
	return s.Backend.ListCheckpoints()
}

func (s *stubBackend) GitDiff(ctx context.Context, color bool) (string, error) {
	if s.gitDiff != nil {
		return s.gitDiff()
	}
	return s.Backend.GitDiff(ctx, color)
}

func (s *stubBackend) SessionDiff() string {
	if s.sessionDiff != nil {
		return s.sessionDiff()
	}
	return s.Backend.SessionDiff()
}

func (s *stubBackend) SessionUsage() (api.Usage, error) {
	if s.sessionUsage != nil {
		return s.sessionUsage()
	}
	return s.Backend.SessionUsage()
}

func (s *stubBackend) Context(parts bool) (api.ContextInfo, error) {
	if s.contextInfo != nil {
		return s.contextInfo()
	}
	return s.Backend.Context(parts)
}

func (s *stubBackend) Compact(ctx context.Context, focus string) (api.CompactResult, error) {
	if s.compact != nil {
		return s.compact(focus)
	}
	return s.Backend.Compact(ctx, focus)
}

func (s *stubBackend) ReloadMemory(ctx context.Context) ([]string, error) {
	if s.reloadMemory != nil {
		return s.reloadMemory()
	}
	return s.Backend.ReloadMemory(ctx)
}

func (s *stubBackend) AddMemory(ctx context.Context, text string) (string, error) {
	if s.addMemory != nil {
		return s.addMemory(text)
	}
	return s.Backend.AddMemory(ctx, text)
}

func (s *stubBackend) ListNotes() ([]api.Note, error) {
	if s.listNotes != nil {
		return s.listNotes()
	}
	return s.Backend.ListNotes()
}

func (s *stubBackend) ForgetNote(name string) error {
	if s.forgetNote != nil {
		return s.forgetNote(name)
	}
	return s.Backend.ForgetNote(name)
}

func (s *stubBackend) Set(ctx context.Context, key, value string) (string, error) {
	if s.set != nil {
		return s.set(key, value)
	}
	return s.Backend.Set(ctx, key, value)
}

func (s *stubBackend) ListStyles() []api.StyleInfo {
	if s.listStyles != nil {
		return s.listStyles()
	}
	return s.Backend.ListStyles()
}

func (s *stubBackend) ListHooks() []api.HookInfo {
	if s.listHooks != nil {
		return s.listHooks()
	}
	return s.Backend.ListHooks()
}

func (s *stubBackend) ListMCPServers() []api.MCPServer {
	if s.listMCPServers != nil {
		return s.listMCPServers()
	}
	return s.Backend.ListMCPServers()
}

func (s *stubBackend) ListAgents() []api.AgentInfo {
	if s.listAgents != nil {
		return s.listAgents()
	}
	return s.Backend.ListAgents()
}

func (s *stubBackend) PinModel(ctx context.Context, agent, ref string) (api.PinResult, error) {
	if s.pinModel != nil {
		return s.pinModel(agent, ref)
	}
	return s.Backend.PinModel(ctx, agent, ref)
}

func (s *stubBackend) Unpin(ctx context.Context, agent string) (api.PinResult, error) {
	if s.unpin != nil {
		return s.unpin(agent)
	}
	return s.Backend.Unpin(ctx, agent)
}

func (s *stubBackend) ActiveAgentTools() api.AgentTools {
	if s.activeAgentTools != nil {
		return s.activeAgentTools()
	}
	return s.Backend.ActiveAgentTools()
}

func (s *stubBackend) AvailableLocales() ([]api.LocaleInfo, string) {
	if s.availableLocales != nil {
		return s.availableLocales()
	}
	return s.Backend.AvailableLocales()
}

func (s *stubBackend) SetLocale(ctx context.Context, input string) (api.LocaleChange, error) {
	if s.setLocale != nil {
		return s.setLocale(input)
	}
	return s.Backend.SetLocale(ctx, input)
}

func (s *stubBackend) RewindPoints() ([]api.RewindPoint, error) {
	if s.rewindPoints != nil {
		return s.rewindPoints()
	}
	return s.Backend.RewindPoints()
}

func (s *stubBackend) Rewind(ctx context.Context, index int, mode api.RewindMode, force bool) (api.RewindResult, error) {
	if s.rewind != nil {
		return s.rewind(index, mode, force)
	}
	return s.Backend.Rewind(ctx, index, mode, force)
}

func (s *stubBackend) SetGoal(cond string) (api.Goal, error) {
	if s.setGoal != nil {
		return s.setGoal(cond)
	}
	return s.Backend.SetGoal(cond)
}

func (s *stubBackend) Goal() (api.Goal, error) {
	if s.goal != nil {
		return s.goal()
	}
	return s.Backend.Goal()
}

func (s *stubBackend) ClearGoal() error {
	if s.clearGoal != nil {
		return s.clearGoal()
	}
	return s.Backend.ClearGoal()
}

func (s *stubBackend) Run(ctx context.Context, sessionID string, t api.Turn, on func(api.Event)) (api.TurnResult, error) {
	if s.run != nil {
		return s.run(ctx, t, on)
	}
	return s.Backend.Run(ctx, sessionID, t, on)
}

func (s *stubBackend) Steer(ctx context.Context, sessionID, text string) error {
	if s.steer != nil {
		return s.steer(text)
	}
	return s.Backend.Steer(ctx, sessionID, text)
}

func (s *stubBackend) ListTasks() []api.TaskInfo {
	if s.listTasks != nil {
		return s.listTasks()
	}
	return s.Backend.ListTasks()
}

func (s *stubBackend) StopTask(id string) (api.TaskInfo, error) {
	if s.stopTask != nil {
		return s.stopTask(id)
	}
	return s.Backend.StopTask(id)
}

func (s *stubBackend) TakeProcessNotices(session string) []string {
	if s.takeNotices != nil {
		return s.takeNotices()
	}
	return s.Backend.TakeProcessNotices(session)
}

func (s *stubBackend) Task(id string) (api.TaskInfo, []string, error) {
	if s.task != nil {
		return s.task(id)
	}
	return s.Backend.Task(id)
}

func (s *stubBackend) PendingTaskRequests() []api.TaskRequest {
	if s.pendingRequests != nil {
		return s.pendingRequests()
	}
	return s.Backend.PendingTaskRequests()
}

func (s *stubBackend) AnswerTaskRequest(id string, d api.Decision, answer string) error {
	if s.answerRequest != nil {
		return s.answerRequest(id, d, answer)
	}
	return s.Backend.AnswerTaskRequest(id, d, answer)
}

func (s *stubBackend) SetModel(ctx context.Context, ref string) (string, error) {
	if s.setModel != nil {
		return s.setModel(ref)
	}
	return s.Backend.SetModel(ctx, ref)
}

func (s *stubBackend) SetAgent(ctx context.Context, name string) (api.AgentInfo, error) {
	if s.setAgent != nil {
		return s.setAgent(name)
	}
	return s.Backend.SetAgent(ctx, name)
}

func (s *stubBackend) SetPermissionMode(mode string) (string, error) {
	if s.setPermission != nil {
		return s.setPermission(mode)
	}
	return s.Backend.SetPermissionMode(mode)
}

func (s *stubBackend) Settings() api.Settings {
	if s.settings != nil {
		return s.settings()
	}
	return s.Backend.Settings()
}

func (s *stubBackend) ModelErr() error {
	if s.modelErr != nil {
		return s.modelErr()
	}
	return s.Backend.ModelErr()
}

func (s *stubBackend) SandboxSummary() []string {
	if s.sandboxSummary != nil {
		return s.sandboxSummary()
	}
	return s.Backend.SandboxSummary()
}

func (s *stubBackend) ProjectSettings() api.ProjectSettings {
	if s.projectSettings != nil {
		return s.projectSettings()
	}
	return s.Backend.ProjectSettings()
}

func (s *stubBackend) TrustProject(hash string, trusted bool) error {
	if s.trustProject != nil {
		return s.trustProject(hash, trusted)
	}
	return s.Backend.TrustProject(hash, trusted)
}

func (s *stubBackend) SearchSkills(q string) []api.SkillInfo {
	if s.searchSkills != nil {
		return s.searchSkills(q)
	}
	return s.Backend.SearchSkills(q)
}

func (s *stubBackend) ListSkills() []api.SkillInfo {
	if s.listSkills != nil {
		return s.listSkills()
	}
	return s.Backend.ListSkills()
}

func (s *stubBackend) Skill(name string) (api.SkillInfo, bool) {
	if s.skill != nil {
		return s.skill(name)
	}
	return s.Backend.Skill(name)
}

func (s *stubBackend) ListEnvs() ([]api.Env, error) {
	if s.listEnvs != nil {
		return s.listEnvs()
	}
	return s.Backend.ListEnvs()
}

func (s *stubBackend) RemoveEnv(key string) error {
	if s.removeEnv != nil {
		return s.removeEnv(key)
	}
	return s.Backend.RemoveEnv(key)
}

func (s *stubBackend) PruneEnvs() (api.PruneResult, error) {
	if s.pruneEnvs != nil {
		return s.pruneEnvs()
	}
	return s.Backend.PruneEnvs()
}

func (s *stubBackend) SearchProvider() (string, error) {
	if s.searchProvider != nil {
		return s.searchProvider()
	}
	return s.Backend.SearchProvider()
}

func (s *stubBackend) SearchWeb(ctx context.Context, terms string) (api.WebSearch, error) {
	if s.searchWeb != nil {
		return s.searchWeb(terms)
	}
	return s.Backend.SearchWeb(ctx, terms)
}

func (s *stubBackend) AddPermissionRule(effect, rule string, save api.Scope) (api.PermissionChange, error) {
	if s.addPermission != nil {
		return s.addPermission(effect, rule, save)
	}
	return s.Backend.AddPermissionRule(effect, rule, save)
}

func (s *stubBackend) RemovePermissionRule(rule string, save api.Scope) (api.PermissionChange, error) {
	if s.removePermission != nil {
		return s.removePermission(rule, save)
	}
	return s.Backend.RemovePermissionRule(rule, save)
}

func (s *stubBackend) ModelSettings(ref string) (api.ModelSettingsInfo, error) {
	if s.modelSettings != nil {
		return s.modelSettings(ref)
	}
	return s.Backend.ModelSettings(ref)
}

func (s *stubBackend) AllModelSettings() map[string]config.ModelSettings {
	if s.allModelSettings != nil {
		return s.allModelSettings()
	}
	return s.Backend.AllModelSettings()
}

func (s *stubBackend) UpdateModelSettings(ref string, reset bool, changes []api.Setting) (api.ModelSettingsChange, error) {
	if s.updateSettings != nil {
		return s.updateSettings(ref, reset, changes)
	}
	return s.Backend.UpdateModelSettings(ref, reset, changes)
}

// stubApp is a command app whose workspace is wrapped in a stubBackend for
// the test to fill in.
func stubApp(t *testing.T, input string) (*App, *stubBackend) {
	t.Helper()
	app, _ := newCommandApp(t, input)
	s := &stubBackend{Backend: app.Workspace}
	app.Workspace = s
	return app, s
}

// runCmd runs a slash command on app and returns what it printed.
func runCmd(t *testing.T, app *App, line string) string {
	t.Helper()
	return captureStdout(t, func() { HandleCommand(context.Background(), line, app) })
}

// noSession makes the stub report no active session.
func noSession() (api.SessionInfo, bool) { return api.SessionInfo{}, false }

// discardInput is a line reader with input and no output.
func discardInput(input string) *LineReader {
	return NewLineReader(strings.NewReader(input), io.Discard)
}
