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
	"path/filepath"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/secretscan"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// InstallSecretHookOutput is what install_secret_hook returns.
type InstallSecretHookOutput struct {
	// Status is installed, present or other_hooks (nothing changed).
	Status string `json:"status,omitempty"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// NewInstallSecretHookTool is install_secret_hook: project-setup adds, as
// it makes the harness, the pre-commit hook that stops commits adding API
// keys and other secrets (spec_secret_hooks_051 SH-02), shown and asked
// about as a write. A repository with other hooks is left alone.
func NewInstallSecretHookTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "install_secret_hook", Description: "Add the pre-commit hook that stops commits adding API keys or other secrets to the workspace's git repository (.githooks/pre-commit, versioned, and core.hooksPath). Call it once the workspace is a repository. A repository that uses other hooks (husky, pre-commit) is left alone: say so to the user."},
		func(ctx agent.Context, _ struct{}) (InstallSecretHookOutput, error) {
			dir := r.Workspace().Dir()
			hook := filepath.Join(dir, secretscan.HooksDir, "pre-commit")
			if err := r.hooks.Approve(ctx, api.ApprovalRequest{
				Tool: "install_secret_hook", Kind: api.ActionWrite,
				Detail:  "Add a pre-commit hook that stops commits adding API keys or other secrets, and set core.hooksPath to .githooks",
				Diff:    unifiedDiff(r.Workspace().display(hook), "", secretscan.HookScript),
				Targets: []string{r.Workspace().display(hook)},
			}); err != nil {
				return InstallSecretHookOutput{Error: err.Error()}, nil
			}
			res, err := secretscan.Install(ctx, dir)
			if err != nil {
				return InstallSecretHookOutput{Error: err.Error()}, nil
			}
			return InstallSecretHookOutput{Status: res.Status, Path: r.Workspace().display(res.Path), Detail: res.Detail}, nil
		})
}
