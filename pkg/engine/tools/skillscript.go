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

package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	scriptOutputLimit = 64 << 10 // per stream returned to the model
	scriptOutputFiles = 50       // files listed from the output directory
	// SkillOutputDir is where scripts write, relative to the workspace.
	SkillOutputDir = ".blitz/skill-output"
)

// SkillScripts runs skills' scripts: each is checked against
// skills.policy, approved by its tier, given its Python environment, and run
// in the script sandbox. Scripts can read the workspace but never write it;
// they write into their own output directory, and the agent applies any
// changes with the file tools (so diffs, approvals and /undo still work).
type SkillScripts struct {
	provider *skills.Provider
	policy   config.SkillPolicy
	ws       *Workspace
	hooks    *Hooks
	envs     *PyEnvs
	nodes    *NodeEnvs   // TypeScript scripts' npm environments
	web      *webFetcher // storage_uri scripts' fetches (nil: web off)
	cacheDir string      // storage_uri scripts' cache ("": ~/.blitz/skill-cache)
	boxCfg   ScriptBoxConfig

	boxOnce sync.Once
	box     ScriptBox
	boxErr  error
}

// NewSkillScripts sets up script runs for the skills of provider.
func NewSkillScripts(provider *skills.Provider, policy config.SkillPolicy, ws *Workspace, hooks *Hooks, envs *PyEnvs, boxCfg ScriptBoxConfig) *SkillScripts {
	return &SkillScripts{provider: provider, policy: policy, ws: ws, hooks: hooks, envs: envs, nodes: NewNodeEnvs("", policy.Packages.NPM), boxCfg: boxCfg}
}

// Box returns the script sandbox, set up on first use (a gVisor test run
// takes a moment).
func (s *SkillScripts) Box() (ScriptBox, error) {
	s.boxOnce.Do(func() { s.box, _, s.boxErr = NewScriptBox(s.boxCfg) })
	return s.box, s.boxErr
}

// Envs is the environment manager.
func (s *SkillScripts) Envs() *PyEnvs { return s.envs }

// RunSkillScriptInput is the run_skill_script request.
type RunSkillScriptInput struct {
	Skill  string   `json:"skill" jsonschema:"Skill name"`
	Script string   `json:"script" jsonschema:"Script name, as listed by activate_skill"`
	Args   []string `json:"args,omitempty" jsonschema:"Command-line arguments for the script"`
}

// RunSkillScriptOutput is how the script ran.
type RunSkillScriptOutput struct {
	ExitCode  int      `json:"exit_code"`
	Stdout    string   `json:"stdout,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
	TimedOut  bool     `json:"timed_out,omitempty"`
	Sandbox   string   `json:"sandbox,omitempty"`
	Tier      string   `json:"tier,omitempty"`
	OutputDir string   `json:"output_dir,omitempty"` // workspace-relative; read results with read_file
	Files     []string `json:"output_files,omitempty"`
	// Changed are the workspace files a writes_workspace script changed,
	// kept once approved.
	Changed []string `json:"changed_files,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// NewRunSkillScriptTool creates run_skill_script.
func NewRunSkillScriptTool(s *SkillScripts) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "run_skill_script",
			Description: "Run a script that a skill ships (see activate_skill for its scripts). It runs sandboxed, " +
				"with its own Python packages; it can read the workspace but writes only to its output directory, " +
				"whose files you can read and then apply with the file tools.",
		},
		func(ctx agent.Context, in RunSkillScriptInput) (RunSkillScriptOutput, error) {
			return s.Run(ctx, in), nil
		},
	)
}

// Run runs one script.
func (s *SkillScripts) Run(ctx context.Context, in RunSkillScriptInput) (out RunSkillScriptOutput) {
	fail := func(format string, a ...any) RunSkillScriptOutput {
		out.Error = fmt.Sprintf(format, a...)
		return out
	}
	skill, ok := s.provider.Get(in.Skill)
	if !ok {
		return fail("no skill named %q", in.Skill)
	}
	idx := slices.IndexFunc(skill.Scripts, func(sc skills.ScriptDefinition) bool { return sc.Name == in.Script })
	if idx < 0 {
		return fail("skill %q has no script %q", in.Skill, in.Script)
	}
	sc := skill.Scripts[idx]
	ev := skills.Evaluate(skill, s.policy)
	verdict := ev.Scripts[idx]
	if !verdict.Allowed {
		return fail("skills.policy doesn't allow this script: %s", strings.Join(verdict.Reasons, "; "))
	}
	ts := sc.Language == skills.LanguageTypeScript
	if sc.Language != skills.LanguagePython && !ts {
		return fail("%s scripts aren't supported", sc.Language)
	}
	out.Tier = ev.Tier.String()

	box, err := s.Box()
	if err != nil {
		return fail("%v", err)
	}
	out.Sandbox = box.Name()
	// A script from storage_uri: fetched (or from the cache), checked
	// against its pinned hash, then run as inline code would be.
	if sc.StorageURI != "" {
		src, err := s.remoteScript(ctx, skill, sc)
		if err != nil {
			return fail("%v", err)
		}
		sc.InlineCode, sc.StorageURI = string(src), ""
	}
	var python string // the script's runtime: python3 (or a managed Python), or node
	if ts {
		python, err = SystemNode()
	} else {
		python, err = s.pythonFor(ctx, box, skill.Name, requiresPython(skill, sc))
	}
	if err != nil {
		return fail("%v", err)
	}

	// Approval by tier: 3 asks every time and can't be remembered.
	detail := fmt.Sprintf("Run %s from skill %s (%s)%s", sc.Name, skill.Name, ev.Tier, argsNote(in.Args))
	switch {
	case ev.Bypass:
		s.audit(detail, "bypass")
	case ev.Tier >= skills.Tier3MandatoryApproval:
		if err := s.hooks.Approve(ctx, api.ApprovalRequest{Tool: "run_skill_script", Kind: api.ActionCommand, Detail: detail}); err != nil {
			return fail("%v", err)
		}
	default:
		s.audit(detail, "auto-"+strings.ToLower(ev.Tier.String()))
	}

	interp := python
	readOnly := append([]string{s.ws.Dir()}, MountsFor(python)...)
	var nodeModules string
	if ts && len(sc.Dependencies) > 0 {
		env, ready := s.nodes.Lookup(python, sc.Dependencies)
		if !ready {
			if err := s.hooks.Approve(ctx, api.ApprovalRequest{
				Tool: "run_skill_script", Kind: api.ActionNetwork,
				Detail: fmt.Sprintf("Install packages for skill %s into an isolated environment (%s): %s", skill.Name, box.Name(), s.nodes.InstallCommand(sc.Dependencies)),
				Key:    "nodeenv:" + env.Key, KeyLabel: "installing exactly these packages",
			}); err != nil {
				return fail("%v", err)
			}
		}
		if env, err = s.nodes.Ensure(ctx, box, python, skill.Name, sc.Dependencies); err != nil {
			return fail("setting up the environment: %v", err)
		}
		nodeModules = env.Modules()
		readOnly = append(readOnly, env.Dir)
	}
	if !ts && len(sc.Dependencies) > 0 {
		env, ready := s.envs.Lookup(python, sc.Dependencies)
		if !ready {
			// Installing reaches the network and runs installers: approved on
			// its own, rememberable for exactly this package list.
			if err := s.hooks.Approve(ctx, api.ApprovalRequest{
				Tool: "run_skill_script", Kind: api.ActionNetwork,
				Detail: fmt.Sprintf("Install packages for skill %s into an isolated environment (%s): %s", skill.Name, box.Name(), s.envs.InstallCommands(sc.Dependencies)),
				Key:    "pyenv:" + env.Key, KeyLabel: "installing exactly these packages",
			}); err != nil {
				return fail("%v", err)
			}
		}
		if env, err = s.envs.Ensure(ctx, box, python, skill.Name, sc.Dependencies); err != nil {
			return fail("setting up the environment: %v", err)
		}
		interp = env.Interpreter()
		readOnly = append(readOnly, env.Dir)
	}

	materialize := materializeScript
	if ts {
		materialize = func(skill *skills.Skill, sc skills.ScriptDefinition) (string, string, func(), error) {
			return materializeTypeScript(skill, sc, nodeModules)
		}
	}
	scriptPath, scriptDir, cleanup, err := materialize(skill, sc)
	if err != nil {
		return fail("%v", err)
	}
	defer cleanup()
	readOnly = append(readOnly, scriptDir)

	var writable []string
	env := []string{"SKILL_DIR=" + scriptDir}
	if !ts {
		env = append(env, "PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1")
	}
	if ev.Tier >= skills.Tier2AuditedWrite || ev.Bypass {
		dir := filepath.Join(s.ws.Dir(), SkillOutputDir, skill.Name, fmt.Sprintf("%s-%s", sc.Name, time.Now().Format("20060102-150405.000")))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fail("%v", err)
		}
		writable = append(writable, dir)
		out.OutputDir, _ = filepath.Rel(s.ws.Dir(), dir)
		env = append(env, "SKILL_OUTPUT="+dir)
	}
	// A skill that writes the workspace works in a copy of it; what it
	// changed is kept only once approved (BL-SK-02).
	runDir := s.ws.Dir()
	var cp *wsCopy
	if skill.ExecutionHints != nil && skill.ExecutionHints.WritesWorkspace {
		if cp, err = copyWorkspace(s.ws); err != nil {
			return fail("%v", err)
		}
		defer cp.remove()
		runDir = cp.dir
		writable = append(writable, cp.dir)
	}
	for _, name := range ev.Env {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	for k, v := range sc.EnvironmentVariables {
		env = append(env, k+"="+v)
	}

	argv := []string{interp, scriptPath}
	switch {
	case ts && sc.EntryPoint != "":
		// Import the module and call the named export; a number it returns
		// (or resolves to) is the exit code.
		argv = append(append([]string{interp}, nodeFlags...), "--input-type=module", "-e",
			"const {pathToFileURL}=await import('node:url'); const m=await import(pathToFileURL(process.argv[1]).href); const r=await m["+pyQuote(sc.EntryPoint)+"](); process.exit(typeof r==='number'?r:0)",
			scriptPath)
	case ts:
		argv = append(append([]string{interp}, nodeFlags...), scriptPath)
	}
	if !ts && sc.EntryPoint != "" {
		// Call the named function, with the script's module run under a
		// name other than __main__.
		argv = []string{interp, "-c",
			"import runpy,sys; sys.argv=sys.argv[1:]; ns=runpy.run_path(sys.argv[0], run_name='__skill__'); r=ns[" + pyQuote(sc.EntryPoint) + "](); sys.exit(r if isinstance(r, int) else 0)",
			scriptPath}
	}
	argv = append(argv, in.Args...)

	stdout, stderr := newCappedBuffer(scriptOutputLimit), newCappedBuffer(scriptOutputLimit)
	res, err := box.Run(ctx, ScriptRequest{
		Argv: argv, Dir: runDir, Env: env, Network: ev.Network,
		ReadOnly: readOnly, Writable: writable, Stdout: stdout, Stderr: stderr,
		Timeout: time.Duration(verdict.TimeoutSeconds) * time.Second,
	})
	out.Stdout, out.Stderr = stdout.String(), stderr.String()
	if err != nil {
		return fail("%v", err)
	}
	out.ExitCode, out.TimedOut = res.ExitCode, res.TimedOut
	if res.TimedOut {
		out.Error = fmt.Sprintf("stopped after %ds (timeout)", verdict.TimeoutSeconds)
	}
	if out.OutputDir != "" {
		out.Files = listFiles(s.ws.Dir(), filepath.Join(s.ws.Dir(), out.OutputDir), scriptOutputFiles)
	}
	if cp != nil {
		s.keepWorkspaceChanges(ctx, cp, skill.Name, &out)
	}
	return out
}

// keepWorkspaceChanges offers what a writes_workspace script changed in
// its copy, once it succeeded.
func (s *SkillScripts) keepWorkspaceChanges(ctx context.Context, cp *wsCopy, skill string, out *RunSkillScriptOutput) {
	changes, err := cp.changes()
	switch {
	case err != nil:
		out.Error = fmt.Sprintf("reading the script's changes: %v", err)
		return
	case len(changes) == 0:
		return
	case out.ExitCode != 0 || out.TimedOut:
		out.Error = strings.TrimSpace(out.Error + "; the script failed, so its changes to the workspace weren't kept")
		return
	}
	written, err := cp.keepChanges(ctx, s.ws, s.hooks, skill, changes)
	out.Changed = written
	if err != nil {
		out.Error = fmt.Sprintf("the script's changes weren't kept: %v", err)
	}
}

func (s *SkillScripts) audit(detail, decision string) {
	s.hooks.Audit().Log(audit.Entry{Kind: audit.KindApproval, Tool: "run_skill_script", Detail: detail, Decision: decision})
}

// materializeScript returns the script's host path and the directory to
// mount for it: the skill's own directory for skills on disk (so a script
// can import its neighbours), else a temporary copy.
func materializeScript(skill *skills.Skill, sc skills.ScriptDefinition) (path, dir string, cleanup func(), err error) {
	cleanup = func() {}
	if sc.RelativePath != "" && skill.HostDir() != "" {
		path, err = skill.ScriptPath(sc.RelativePath)
		return path, skill.HostDir(), cleanup, err
	}
	tmp, err := os.MkdirTemp("", "blitz-skill-*")
	if err != nil {
		return "", "", cleanup, err
	}
	cleanup = func() { os.RemoveAll(tmp) }
	switch {
	case sc.InlineCode != "":
		path = filepath.Join(tmp, sc.Name+".py")
		err = os.WriteFile(path, []byte(sc.InlineCode), 0o600)
	case sc.RelativePath != "":
		if err = skill.CopyTo(tmp); err == nil {
			path = filepath.Join(tmp, filepath.FromSlash(sc.RelativePath))
		}
	default:
		err = errors.New("the script has no source")
	}
	if err != nil {
		cleanup()
		return "", "", func() {}, err
	}
	if c, err := filepath.EvalSymlinks(tmp); err == nil { // macOS: /var -> /private/var
		path = filepath.Join(c, strings.TrimPrefix(path, tmp))
		tmp = c
	}
	return path, tmp, cleanup, nil
}

// requiresPython is the Python a script needs: its definition's
// requires_python, else its PEP 723 metadata's ("" when neither says).
func requiresPython(skill *skills.Skill, sc skills.ScriptDefinition) string {
	if sc.RequiresPython != "" {
		return sc.RequiresPython
	}
	src := sc.InlineCode
	if src == "" && sc.RelativePath != "" {
		b, err := skill.ReadScript(sc.RelativePath)
		if err != nil {
			return ""
		}
		src = string(b)
	}
	return scriptRequiresPython(src)
}

// nodeFlags run a TypeScript script: its types stripped (Node 22.6+), and
// no warning about the feature.
var nodeFlags = []string{"--experimental-strip-types", "--no-warnings"}

// materializeTypeScript copies a TypeScript script (and, on disk, its
// skill's files, so it can import its neighbours) into a temporary
// directory, beside a node_modules link to its packages' environment,
// where node looks for them.
func materializeTypeScript(skill *skills.Skill, sc skills.ScriptDefinition, nodeModules string) (path, dir string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "blitz-skill-*")
	if err != nil {
		return "", "", func() {}, err
	}
	cleanup = func() { os.RemoveAll(tmp) }
	switch {
	case sc.InlineCode != "":
		path = filepath.Join(tmp, sc.Name+".ts")
		err = os.WriteFile(path, []byte(sc.InlineCode), 0o600)
	case sc.RelativePath != "":
		if err = skill.CopyTo(tmp); err == nil {
			path = filepath.Join(tmp, filepath.FromSlash(sc.RelativePath))
		}
	default:
		err = errors.New("the script has no source")
	}
	if err == nil && nodeModules != "" {
		_ = os.RemoveAll(filepath.Join(tmp, "node_modules")) // a copied one mustn't win
		err = os.Symlink(nodeModules, filepath.Join(tmp, "node_modules"))
	}
	if err != nil {
		cleanup()
		return "", "", func() {}, err
	}
	if c, err := filepath.EvalSymlinks(tmp); err == nil { // macOS: /var -> /private/var
		path = filepath.Join(c, strings.TrimPrefix(path, tmp))
		tmp = c
	}
	return path, tmp, cleanup, nil
}

// listFiles lists up to max regular files under dir, relative to ws.
func listFiles(ws, dir string, max int) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if len(files) == max {
			return filepath.SkipAll
		}
		if rel, err := filepath.Rel(ws, p); err == nil {
			files = append(files, rel)
		}
		return nil
	})
	return files
}

func argsNote(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return " with arguments " + shellJoin(args)
}

// pyQuote quotes s as a Python string literal.
func pyQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`).Replace(s) + "'"
}
