// Package client is a workspace held by the Blitz service, reached
// over its socket: an api.Backend, so front ends drive it exactly as they
// drive a local *app.Workspace.
package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/socket"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/images"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/retail-cortex/blitz/proto/blitz/v1/blitzv1connect"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Remote is one workspace in the service.
type Remote struct {
	dir        string
	sessions   blitzv1connect.SessionServiceClient
	workspaces blitzv1connect.WorkspaceServiceClient
	// warn reports calls that failed where Backend has no error to return
	// (a listing comes back empty instead).
	warn     func(string)
	modelErr error

	mu      sync.Mutex
	approve api.Approver
	ask     api.UserPromptFunc
}

var _ api.Backend = (*Remote)(nil)

// Attach opens dir (made absolute) in the service listening on sock.
func Attach(ctx context.Context, sock, dir string, warn func(string)) (*Remote, error) {
	return AttachHTTP(ctx, socket.Client(sock), socket.BaseURL, dir, warn)
}

// AttachHTTP is Attach over any HTTP client, e.g. in tests.
func AttachHTTP(ctx context.Context, hc connect.HTTPClient, baseURL, dir string, warn func(string)) (*Remote, error) {
	abs, err := filepath.Abs(config.ExpandHome(dir))
	if err != nil {
		return nil, err
	}
	if warn == nil {
		warn = func(string) {}
	}
	r := &Remote{
		dir:        abs,
		sessions:   blitzv1connect.NewSessionServiceClient(hc, baseURL),
		workspaces: blitzv1connect.NewWorkspaceServiceClient(hc, baseURL),
		warn:       warn,
	}
	// Opens the workspace in the service, and says whether its model works.
	m, err := r.workspaces.GetModel(ctx, connect.NewRequest(&pb.GetModelRequest{Workspace: abs}))
	if err != nil {
		return nil, fromAPI(err)
	}
	if m.Msg.Unavailable != "" {
		r.modelErr = errors.New(m.Msg.Unavailable)
	}
	return r, nil
}

// failed reports a call Backend can't return an error from.
func (r *Remote) failed(what string, err error) {
	r.warn(fmt.Sprintf("%s: %v", what, fromAPI(err)))
}

func (r *Remote) Dir() string     { return r.dir }
func (r *Remote) ModelErr() error { return r.modelErr }

// Close leaves the workspace open in the service, for other clients.
func (r *Remote) Close() error { return nil }

func (r *Remote) SetUI(approve api.Approver, ask api.UserPromptFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.approve, r.ask = approve, ask
}

// Processes is nil: background processes belong to the service, and stay
// when this client exits.
func (r *Remote) Processes() api.Processes { return nil }

// AuditShell does nothing: the command ran here, outside the service.
func (r *Remote) AuditShell(string, int, error) {}

func (r *Remote) ImagesEnabled() bool { return r.getSettings().ImagesEnabled }

// Run runs a turn in the service, answering its approval requests and
// questions through the UI set with SetUI. OnFinished runs when the turn's
// last event arrives, after the service has collected unread steer
// messages; a message sent later waits for the agent's next turn.
func (r *Remote) Run(ctx context.Context, sessionID string, t api.Turn, on func(api.Event)) (api.TurnResult, error) {
	turn := &pb.Turn{
		Text: t.Text, Prompt: t.Prompt, Plan: t.Plan, ReadOnly: t.ReadOnly, Aside: t.Aside, Accepted: t.Accepted,
		MaxTurns: int32(t.MaxTurns), FetchGrants: t.FetchGrants, MaxCostUsd: t.MaxCostUSD, Command: t.Command,
	}
	if t.Timeout > 0 {
		turn.Timeout = durationpb.New(t.Timeout)
	}
	for _, img := range t.Images {
		turn.ImageIds = append(turn.ImageIds, img.SHA256)
	}
	stream, err := r.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: r.dir, SessionId: sessionID, Turn: turn}))
	if err != nil {
		return api.TurnResult{}, fromAPI(err)
	}
	defer stream.Close()
	r.mu.Lock()
	approve, ask := r.approve, r.ask
	r.mu.Unlock()

	for stream.Receive() {
		ev := stream.Msg().Event
		switch k := ev.Kind.(type) {
		case *pb.TurnEvent_Accepted:
			if t.OnAccepted != nil {
				t.OnAccepted()
			}
		case *pb.TurnEvent_ApprovalRequest:
			r.answerApproval(ctx, approve, k.ApprovalRequest)
		case *pb.TurnEvent_Question:
			r.answerQuestion(ctx, ask, k.Question)
		case *pb.TurnEvent_Finished:
			if t.OnFinished != nil {
				t.OnFinished()
			}
			f := k.Finished
			res := api.TurnResult{Output: f.Output, Before: usage(f.Before), After: usage(f.After), Leftover: f.Leftover}
			return res, errorFromInfo(f.Error)
		default:
			if e, ok := event(ev); ok {
				on(e)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return api.TurnResult{}, fromAPI(err)
	}
	return api.TurnResult{}, errors.New("the service ended the turn without finishing it")
}

func (r *Remote) answerApproval(ctx context.Context, approve api.Approver, req *pb.ApprovalRequest) {
	d := api.DecisionDeny
	if approve != nil {
		var err error
		if d, err = approve(ctx, api.ApprovalRequest{
			Tool: req.Tool, Kind: actionKind(req.Kind), Detail: req.Detail, Diff: req.Diff, KeyLabel: req.ScopeLabel,
		}); err != nil {
			d = api.DecisionDeny
		}
	}
	if _, err := r.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: r.dir, RequestId: req.RequestId, Decision: decisionMsg(d)})); err != nil {
		r.failed("answering an approval", err)
	}
}

func (r *Remote) answerQuestion(ctx context.Context, ask api.UserPromptFunc, q *pb.Question) {
	answer := ""
	if ask != nil {
		answer, _ = ask(ctx, q.Question, q.Options)
	}
	if _, err := r.sessions.Answer(ctx, connect.NewRequest(&pb.AnswerRequest{Workspace: r.dir, RequestId: q.RequestId, Answer: answer})); err != nil {
		r.failed("answering a question", err)
	}
}

func (r *Remote) Steer(ctx context.Context, sessionID, text string) error {
	_, err := r.sessions.Steer(ctx, connect.NewRequest(&pb.SteerRequest{Workspace: r.dir, SessionId: sessionID, Text: text}))
	return fromAPI(err)
}

func (r *Remote) OpenSession(resume string, cont bool) (api.SessionInfo, bool, error) {
	res, err := r.sessions.OpenSession(context.Background(), connect.NewRequest(&pb.OpenSessionRequest{Workspace: r.dir, Resume: resume, ContinueLatest: cont}))
	if err != nil {
		return api.SessionInfo{}, false, fromAPI(err)
	}
	return session(res.Msg.Session), res.Msg.Resumed, nil
}

func (r *Remote) ActiveSession() (api.SessionInfo, bool) {
	res, err := r.sessions.GetActiveSession(context.Background(), connect.NewRequest(&pb.GetActiveSessionRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("reading the active session", err)
		return api.SessionInfo{}, false
	}
	if res.Msg.Session == nil {
		return api.SessionInfo{}, false
	}
	return session(res.Msg.Session), true
}

func (r *Remote) ListSessions(all bool) ([]api.SessionInfo, error) {
	res, err := r.sessions.ListSessions(context.Background(), connect.NewRequest(&pb.ListSessionsRequest{Workspace: r.dir, All: all}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return sessions(res.Msg.Sessions), nil
}

func (r *Remote) NewSession() (api.SessionInfo, error) {
	res, err := r.sessions.NewSession(context.Background(), connect.NewRequest(&pb.NewSessionRequest{Workspace: r.dir}))
	if err != nil {
		return api.SessionInfo{}, fromAPI(err)
	}
	return session(res.Msg.Session), nil
}

func (r *Remote) LoadSession(ref string) (api.SessionInfo, bool, error) {
	res, err := r.sessions.LoadSession(context.Background(), connect.NewRequest(&pb.LoadSessionRequest{Workspace: r.dir, Ref: ref}))
	if err != nil {
		return api.SessionInfo{}, false, fromAPI(err)
	}
	return session(res.Msg.Session), res.Msg.Branched, nil
}

func (r *Remote) SaveSnapshot(name string, force bool) (api.SessionInfo, error) {
	res, err := r.sessions.SaveSnapshot(context.Background(), connect.NewRequest(&pb.SaveSnapshotRequest{Workspace: r.dir, Name: name, Force: force}))
	if err != nil {
		return api.SessionInfo{}, fromAPI(err)
	}
	return session(res.Msg.Snapshot), nil
}

func (r *Remote) RenameSession(title string) (api.SessionInfo, error) {
	res, err := r.sessions.RenameSession(context.Background(), connect.NewRequest(&pb.RenameSessionRequest{Workspace: r.dir, Title: title}))
	if err != nil {
		return api.SessionInfo{}, fromAPI(err)
	}
	return session(res.Msg.Session), nil
}

func (r *Remote) ListAgents() []api.AgentInfo {
	res, err := r.workspaces.ListAgents(context.Background(), connect.NewRequest(&pb.ListAgentsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing agents", err)
		return nil
	}
	out := make([]api.AgentInfo, len(res.Msg.Agents))
	for i, a := range res.Msg.Agents {
		out[i] = agent(a)
	}
	return out
}

func (r *Remote) ActiveAgent() api.AgentInfo {
	for _, a := range r.ListAgents() {
		if a.Active {
			return a
		}
	}
	return api.AgentInfo{}
}

func (r *Remote) SetAgent(ctx context.Context, name string) (api.AgentInfo, error) {
	res, err := r.workspaces.SetAgent(ctx, connect.NewRequest(&pb.SetAgentRequest{Workspace: r.dir, Name: name}))
	if err != nil {
		return api.AgentInfo{}, fromAPI(err)
	}
	return agent(res.Msg.Agent), nil
}

func (r *Remote) Model() api.ModelInfo {
	res, err := r.workspaces.GetModel(context.Background(), connect.NewRequest(&pb.GetModelRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("reading the model", err)
		return api.ModelInfo{}
	}
	return api.ModelInfo{Name: res.Msg.Name, Provider: res.Msg.Provider}
}

func (r *Remote) SetModel(ctx context.Context, ref string) (string, error) {
	res, err := r.workspaces.SetModel(ctx, connect.NewRequest(&pb.SetModelRequest{Workspace: r.dir, Ref: ref}))
	if err != nil {
		return "", fromAPI(err)
	}
	return res.Msg.ActivePin, nil
}

func (r *Remote) PinModel(ctx context.Context, agent, ref string) (api.PinResult, error) {
	res, err := r.workspaces.PinModel(ctx, connect.NewRequest(&pb.PinModelRequest{Workspace: r.dir, Agent: agent, Ref: ref}))
	if err != nil {
		return api.PinResult{}, fromAPI(err)
	}
	return api.PinResult{Agent: res.Msg.Agent, Model: res.Msg.Model, Saved: saved(res.Msg.Saved)}, nil
}

func (r *Remote) Unpin(ctx context.Context, agent string) (api.PinResult, error) {
	res, err := r.workspaces.UnpinModel(ctx, connect.NewRequest(&pb.UnpinModelRequest{Workspace: r.dir, Agent: agent}))
	if err != nil {
		return api.PinResult{}, fromAPI(err)
	}
	return api.PinResult{Agent: res.Msg.Agent, Model: res.Msg.Model, Saved: saved(res.Msg.Saved)}, nil
}

func (r *Remote) AllModelSettings() map[string]config.ModelSettings {
	res, err := r.workspaces.GetModelSettings(context.Background(), connect.NewRequest(&pb.GetModelSettingsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("reading model settings", err)
		return nil
	}
	out := map[string]config.ModelSettings{}
	for name, s := range res.Msg.All {
		out[name] = modelSettings(s)
	}
	return out
}

func (r *Remote) ModelSettings(ref string) (api.ModelSettingsInfo, error) {
	res, err := r.workspaces.GetModelSettings(context.Background(), connect.NewRequest(&pb.GetModelSettingsRequest{Workspace: r.dir, Ref: ref}))
	if err != nil {
		return api.ModelSettingsInfo{}, fromAPI(err)
	}
	return modelSettingsInfo(res.Msg.Model), nil
}

func (r *Remote) UpdateModelSettings(ref string, reset bool, changes []api.Setting) (api.ModelSettingsChange, error) {
	req := &pb.UpdateModelSettingsRequest{Workspace: r.dir, Ref: ref, Reset_: reset}
	for _, c := range changes {
		req.Changes = append(req.Changes, &pb.Setting{Key: c.Key, Value: c.Value})
	}
	res, err := r.workspaces.UpdateModelSettings(context.Background(), connect.NewRequest(req))
	if err != nil {
		return api.ModelSettingsChange{}, fromAPI(err)
	}
	return api.ModelSettingsChange{ModelSettingsInfo: modelSettingsInfo(res.Msg.Model), Unsupported: res.Msg.Unsupported, Saved: saved(res.Msg.Saved)}, nil
}

// settings is the service's settings plus whether images are enabled.
type settings struct {
	api.Settings
	ImagesEnabled bool
}

func (r *Remote) getSettings() settings {
	res, err := r.workspaces.GetSettings(context.Background(), connect.NewRequest(&pb.GetSettingsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("reading settings", err)
		return settings{}
	}
	m := res.Msg
	return settings{Settings: api.Settings{
		Agency: m.Agency,
		Model:  api.ModelInfo{Name: m.Model, Provider: m.Provider}, Agent: m.Agent, Locale: m.Locale,
		PermissionMode: m.PermissionMode, Effort: m.Effort,
	}, ImagesEnabled: m.ImagesEnabled}
}

func (r *Remote) Settings() api.Settings { return r.getSettings().Settings }

func (r *Remote) Set(ctx context.Context, key, value string) (string, error) {
	res, err := r.workspaces.SetSetting(ctx, connect.NewRequest(&pb.SetSettingRequest{Workspace: r.dir, Key: key, Value: value}))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(key)), fromAPI(err)
	}
	return res.Msg.Key, nil
}

func (r *Remote) ListCommands() []api.CommandInfo {
	res, err := r.workspaces.ListCommands(context.Background(), connect.NewRequest(&pb.ListCommandsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing commands", err)
		return nil
	}
	var out []api.CommandInfo
	for _, c := range res.Msg.Commands {
		out = append(out, api.CommandInfo{Name: c.Name, Description: c.Description, ArgumentHint: c.ArgumentHint, Source: c.Source})
	}
	return out
}

func (r *Remote) ListPermissionRules() []api.PermissionRule {
	res, err := r.workspaces.ListPermissionRules(context.Background(), connect.NewRequest(&pb.ListPermissionRulesRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing permission rules", err)
		return nil
	}
	var out []api.PermissionRule
	for _, x := range res.Msg.Rules {
		out = append(out, api.PermissionRule{Effect: x.Effect, Rule: x.Rule, Source: x.Source})
	}
	return out
}

func (r *Remote) AddPermissionRule(effect, rule string, save bool) (api.PermissionChange, error) {
	res, err := r.workspaces.AddPermissionRule(context.Background(), connect.NewRequest(&pb.AddPermissionRuleRequest{Workspace: r.dir, Effect: effect, Rule: rule, Save: save}))
	if err != nil {
		return api.PermissionChange{}, fromAPI(err)
	}
	return api.PermissionChange{Rule: res.Msg.Rule, NextStart: res.Msg.NextStart, Saved: saved(res.Msg.Saved)}, nil
}

func (r *Remote) RemovePermissionRule(rule string, save bool) (api.PermissionChange, error) {
	res, err := r.workspaces.RemovePermissionRule(context.Background(), connect.NewRequest(&pb.RemovePermissionRuleRequest{Workspace: r.dir, Rule: rule, Save: save}))
	if err != nil {
		return api.PermissionChange{}, fromAPI(err)
	}
	return api.PermissionChange{Rule: res.Msg.Rule, Removed: int(res.Msg.Removed), Saved: saved(res.Msg.Saved)}, nil
}

func (r *Remote) SetPermissionMode(mode string) (string, error) {
	res, err := r.workspaces.SetPermissionMode(context.Background(), connect.NewRequest(&pb.SetPermissionModeRequest{Workspace: r.dir, Mode: mode}))
	if err != nil {
		return "", fromAPI(err)
	}
	return res.Msg.Mode, nil
}

func (r *Remote) listSkills(query string) []api.SkillInfo {
	res, err := r.workspaces.ListSkills(context.Background(), connect.NewRequest(&pb.ListSkillsRequest{Workspace: r.dir, Query: query}))
	if err != nil {
		r.failed("listing skills", err)
		return nil
	}
	out := make([]api.SkillInfo, len(res.Msg.Skills))
	for i, s := range res.Msg.Skills {
		out[i] = skill(s)
	}
	return out
}

func (r *Remote) ListSkills() []api.SkillInfo               { return r.listSkills("") }
func (r *Remote) SearchSkills(query string) []api.SkillInfo { return r.listSkills(query) }

func (r *Remote) Skill(name string) (api.SkillInfo, bool) {
	res, err := r.workspaces.GetSkill(context.Background(), connect.NewRequest(&pb.GetSkillRequest{Workspace: r.dir, Name: name}))
	if err != nil {
		if connect.CodeOf(err) != connect.CodeNotFound {
			r.failed("reading a skill", err)
		}
		return api.SkillInfo{}, false
	}
	return skill(res.Msg.Skill), true
}

func (r *Remote) ListEnvs() ([]api.Env, error) {
	res, err := r.workspaces.ListEnvs(context.Background(), connect.NewRequest(&pb.ListEnvsRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	var out []api.Env
	for _, e := range res.Msg.Envs {
		out = append(out, api.Env{Key: e.Key, Deps: e.Deps, Skills: e.Skills, Size: e.Size, LastUsed: timeOf(e.LastUsed), Ready: e.Ready})
	}
	return out, nil
}

func (r *Remote) RemoveEnv(key string) error {
	_, err := r.workspaces.RemoveEnv(context.Background(), connect.NewRequest(&pb.RemoveEnvRequest{Workspace: r.dir, Key: key}))
	return fromAPI(err)
}

func (r *Remote) PruneEnvs() (api.PruneResult, error) {
	res, err := r.workspaces.PruneEnvs(context.Background(), connect.NewRequest(&pb.PruneEnvsRequest{Workspace: r.dir}))
	if err != nil {
		return api.PruneResult{}, fromAPI(err)
	}
	out := api.PruneResult{Removed: int(res.Msg.Removed), Freed: res.Msg.Freed}
	for _, f := range res.Msg.Failed {
		out.Failed = append(out.Failed, api.EnvError{Key: f.Key, Err: errors.New(f.Error)})
	}
	return out, nil
}

func (r *Remote) ListMCPServers() []api.MCPServer {
	res, err := r.workspaces.ListMCPServers(context.Background(), connect.NewRequest(&pb.ListMCPServersRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing MCP servers", err)
		return nil
	}
	var out []api.MCPServer
	for _, m := range res.Msg.Servers {
		out = append(out, api.MCPServer{Name: m.Name, Target: m.Target, AutoApprove: m.AutoApprove})
	}
	return out
}

func (r *Remote) ActiveAgentTools() api.AgentTools {
	res, err := r.workspaces.ListTools(context.Background(), connect.NewRequest(&pb.ListToolsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing tools", err)
		return api.AgentTools{}
	}
	out := api.AgentTools{Agent: res.Msg.Agent}
	for _, t := range res.Msg.Tools {
		out.Tools = append(out.Tools, api.ToolInfo{Name: t.Name, Description: t.Description, PlanAllowed: t.PlanAllowed})
	}
	for _, m := range res.Msg.Mcp {
		out.MCP = append(out.MCP, api.MCPOffer{Server: m.Server, Tools: m.Tools, Prefix: m.Prefix})
	}
	return out
}

func (r *Remote) usage() (*pb.GetUsageResponse, error) {
	res, err := r.sessions.GetUsage(context.Background(), connect.NewRequest(&pb.GetUsageRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg, nil
}

func (r *Remote) SessionUsage() (api.Usage, error) {
	u, err := r.usage()
	if err != nil {
		return api.Usage{}, err
	}
	return usage(u.Usage), nil
}

func (r *Remote) Context() (api.ContextInfo, error) {
	u, err := r.usage()
	if err != nil {
		return api.ContextInfo{}, err
	}
	return api.ContextInfo{Tokens: u.Usage.GetLastPrompt(), AutoCompact: u.AutoCompact, Threshold: int(u.Threshold), Keep: int(u.Keep)}, nil
}

func (r *Remote) Compact(ctx context.Context, focus string) (api.CompactResult, error) {
	res, err := r.sessions.Compact(ctx, connect.NewRequest(&pb.CompactRequest{Workspace: r.dir, Focus: focus}))
	if err != nil {
		return api.CompactResult{}, fromAPI(err)
	}
	return api.CompactResult{EventsCompacted: int(res.Msg.EventsCompacted), SummaryChars: int(res.Msg.SummaryChars), Before: usage(res.Msg.Before), After: usage(res.Msg.After)}, nil
}

// RewindPoints lists the active session's prompts.
func (r *Remote) RewindPoints() ([]api.RewindPoint, error) {
	res, err := r.sessions.ListRewindPoints(context.Background(), connect.NewRequest(&pb.ListRewindPointsRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	var out []api.RewindPoint
	for _, p := range res.Msg.Points {
		out = append(out, api.RewindPoint{Index: int(p.Index), Text: p.Text, Time: p.Time.AsTime(), Files: p.Files, Conversation: p.Conversation})
	}
	return out, nil
}

// Rewind takes the active session back to one of its prompts.
func (r *Remote) Rewind(ctx context.Context, index int, mode api.RewindMode, force bool) (api.RewindResult, error) {
	res, err := r.sessions.Rewind(ctx, connect.NewRequest(&pb.RewindRequest{Workspace: r.dir, Index: int32(index), Mode: string(mode), Force: force}))
	if err != nil {
		return api.RewindResult{}, fromAPI(err)
	}
	c := res.Msg.Compacted
	return api.RewindResult{Mode: api.RewindMode(res.Msg.Mode), Restored: res.Msg.Restored, Prompt: res.Msg.Prompt,
		Compacted: api.CompactResult{EventsCompacted: int(c.GetEventsCompacted()), SummaryChars: int(c.GetSummaryChars()), Before: usage(c.GetBefore()), After: usage(c.GetAfter())}}, nil
}

func (r *Remote) memory(ctx context.Context) (*pb.ReloadMemoryResponse, error) {
	res, err := r.workspaces.ReloadMemory(ctx, connect.NewRequest(&pb.ReloadMemoryRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg, nil
}

func (r *Remote) MemoryFiles() []string {
	m, err := r.memory(context.Background())
	if err != nil {
		r.failed("reading project memory", err)
		return nil
	}
	return m.Files
}

func (r *Remote) ReloadMemory(ctx context.Context) ([]string, error) {
	m, err := r.memory(ctx)
	if err != nil {
		return nil, err
	}
	return m.Paths, nil
}

func (r *Remote) AddMemory(ctx context.Context, text string) (string, error) {
	res, err := r.workspaces.AddMemory(ctx, connect.NewRequest(&pb.AddMemoryRequest{Workspace: r.dir, Text: text}))
	if err != nil {
		return "", fromAPI(err)
	}
	return res.Msg.Path, nil
}

func (r *Remote) AvailableLocales() ([]api.LocaleInfo, string) {
	res, err := r.workspaces.ListLocales(context.Background(), connect.NewRequest(&pb.ListLocalesRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing languages", err)
		return nil, ""
	}
	var out []api.LocaleInfo
	for _, l := range res.Msg.Locales {
		out = append(out, api.LocaleInfo{Tag: l.Tag, Name: l.Name})
	}
	return out, res.Msg.CustomDir
}

func (r *Remote) SetLocale(ctx context.Context, input string) (api.LocaleChange, error) {
	res, err := r.workspaces.SetLocale(ctx, connect.NewRequest(&pb.SetLocaleRequest{Workspace: r.dir, Input: input}))
	if err != nil {
		return api.LocaleChange{}, fromAPI(err)
	}
	m := res.Msg
	return api.LocaleChange{Tag: m.Tag, NativeName: m.NativeName, LanguageName: m.LanguageName, HasCatalog: m.HasCatalog, Saved: saved(m.Saved)}, nil
}

func (r *Remote) SandboxSummary() []string {
	res, err := r.workspaces.GetSandbox(context.Background(), connect.NewRequest(&pb.GetSandboxRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("describing the sandbox", err)
		return nil
	}
	return res.Msg.Summary
}

func (r *Remote) ListCheckpoints() []api.Checkpoint {
	res, err := r.workspaces.ListCheckpoints(context.Background(), connect.NewRequest(&pb.ListCheckpointsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing checkpoints", err)
		return nil
	}
	var out []api.Checkpoint
	for _, c := range res.Msg.Checkpoints {
		out = append(out, api.Checkpoint{ID: int(c.Id), Label: c.Label, Time: timeOf(c.Time), Files: c.Files})
	}
	return out
}

func (r *Remote) Undo(force bool) (api.UndoResult, error) {
	res, err := r.workspaces.Undo(context.Background(), connect.NewRequest(&pb.UndoRequest{Workspace: r.dir, Force: force}))
	if err != nil {
		return api.UndoResult{}, fromAPI(err)
	}
	return api.UndoResult{Label: res.Msg.Label, Restored: res.Msg.Restored}, errorFromInfo(res.Msg.Error)
}

func (r *Remote) SessionDiff() string {
	res, err := r.workspaces.GetDiff(context.Background(), connect.NewRequest(&pb.GetDiffRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("reading the diff", err)
		return ""
	}
	return res.Msg.Diff
}

func (r *Remote) GitDiff(ctx context.Context, color bool) (string, error) {
	res, err := r.workspaces.GetDiff(ctx, connect.NewRequest(&pb.GetDiffRequest{Workspace: r.dir, Git: true, Color: color}))
	if err != nil {
		return "", fromAPI(err)
	}
	return res.Msg.Diff, nil
}

func (r *Remote) ListApprovals() []api.Approval {
	res, err := r.workspaces.ListApprovals(context.Background(), connect.NewRequest(&pb.ListApprovalsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing approvals", err)
		return nil
	}
	var out []api.Approval
	for _, a := range res.Msg.Approvals {
		out = append(out, api.Approval{Key: a.Key, Kind: a.Kind, Subject: a.Subject, Dir: a.Dir, Always: a.Always, Added: timeOf(a.Added)})
	}
	return out
}

func (r *Remote) revoke(req *pb.RevokeApprovalsRequest) int {
	req.Workspace = r.dir
	res, err := r.workspaces.RevokeApprovals(context.Background(), connect.NewRequest(req))
	if err != nil {
		r.failed("revoking approvals", err)
		return 0
	}
	return int(res.Msg.Revoked)
}

func (r *Remote) RevokeApprovals(keys ...string) int {
	return r.revoke(&pb.RevokeApprovalsRequest{Keys: keys})
}
func (r *Remote) ClearApprovals() int { return r.revoke(&pb.RevokeApprovalsRequest{All: true}) }

func (r *Remote) LoadImage(path string) (*images.Image, error) {
	res, err := r.workspaces.LoadImage(context.Background(), connect.NewRequest(&pb.LoadImageRequest{Workspace: r.dir, Path: path}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return imageOf(res.Msg.Image), nil
}

func (r *Remote) AddImage(name string, data []byte) (*images.Image, error) {
	res, err := r.workspaces.AddImage(context.Background(), connect.NewRequest(&pb.AddImageRequest{Workspace: r.dir, Name: name, Data: data}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return imageOf(res.Msg.Image), nil
}

// LoadAttachments loads image files (failures are errors naming the path)
// and @image mentions in prompt (failures are warnings), as the local
// workspace does.
func (r *Remote) LoadAttachments(paths []string, prompt string, warn func(string)) ([]*images.Image, error) {
	var out []*images.Image
	seen := map[string]bool{}
	add := func(img *images.Image) {
		if !seen[img.SHA256] {
			seen[img.SHA256] = true
			out = append(out, img)
		}
	}
	for _, p := range paths {
		img, err := r.LoadImage(strings.TrimPrefix(p, "@"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		add(img)
	}
	for _, p := range images.Mentions(prompt) {
		img, err := r.LoadImage(p)
		if err != nil {
			warn(i18n.T("attach.failed", "path", p, "error", err.Error()))
			continue
		}
		add(img)
	}
	return out, nil
}

func (r *Remote) SearchProvider() (string, error) {
	res, err := r.workspaces.GetSearchProvider(context.Background(), connect.NewRequest(&pb.GetSearchProviderRequest{Workspace: r.dir}))
	if err != nil {
		return "", fromAPI(err)
	}
	return res.Msg.Provider, nil
}

func (r *Remote) SearchWeb(ctx context.Context, terms string) (api.WebSearch, error) {
	res, err := r.workspaces.SearchWeb(ctx, connect.NewRequest(&pb.SearchWebRequest{Workspace: r.dir, Terms: terms}))
	if err != nil {
		return api.WebSearch{}, fromAPI(err)
	}
	out := api.WebSearch{Prompt: res.Msg.Prompt}
	for _, l := range res.Msg.Links {
		out.Links = append(out.Links, api.Link{Title: l.Title, URL: l.Url})
	}
	return out, nil
}

func (r *Remote) SearchSession(terms string) (int, string) {
	res, err := r.sessions.SearchSession(context.Background(), connect.NewRequest(&pb.SearchSessionRequest{Workspace: r.dir, Terms: terms}))
	if err != nil {
		r.failed("searching the session", err)
		return 0, ""
	}
	return int(res.Msg.Found), res.Msg.Prompt
}
