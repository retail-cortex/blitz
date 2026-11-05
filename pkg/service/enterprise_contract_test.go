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

package service_test

// The enterprise contract: ../blitz-enterprise (a private module) builds
// on these names from blitz's pkg/, and CI here can't build it. This file
// refers to every one, as it uses them, so a change in blitz that would
// break the enterprise build (a name renamed or removed, a field gone, a
// signature changed) fails blitz's own build first. It only has to
// compile. When the enterprise service starts using another name from
// blitz, add it here.

import (
	"context"
	"net/http"

	"github.com/retail-cortex/blitz/pkg/catalog"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/guardrails"
	"github.com/retail-cortex/blitz/pkg/jobspec"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/service"
	"github.com/retail-cortex/blitz/pkg/urn"
)

// pkg/service: the service it runs, its network listener, and its jobs'
// runner (cmd/blitz-enterprise, cmd/blitz-runner, internal/identity,
// internal/rbac, internal/terms, internal/admin).
var (
	_ func(context.Context, service.Options) error = service.Run
	_                                              = service.Options{Version: "", Network: &service.Network{
		Listener: nil, TLS: nil, Plain: false, Auth: nil, Authz: nil, Routes: (*http.ServeMux)(nil), Wrap: func(http.Handler) http.Handler { return nil },
	}}
	_ func(func(string) string) (service.Job, error)                          = service.JobFromEnv
	_ func(context.Context, service.Job, service.JobOptions) error            = service.RunJob
	_                                                                         = service.JobOptions{Client: ""}
	_ service.Caller                                                          = service.Caller{ID: "", Email: "", Roles: []string{}}
	_ error                                                                   = &service.Refusal{Status: 0, Code: "", Message: ""}
	_ func(service.Authenticator, *http.Request) (service.Caller, error)      = service.Authenticator.Authenticate
	_ func(service.Authorizer, context.Context, service.Caller, string) error = service.Authorizer.Authorize
)

// pkg/urn, pkg/guardrails, pkg/catalog, pkg/secrets, pkg/jobspec and
// pkg/enterprise.
var (
	_ urn.Kind = urn.Artifacts
	_          = []urn.Kind{urn.Content, urn.Groups, urn.Jobs, urn.Runs, urn.Users, urn.Workspaces}
	_ urn.Name
	_ = urn.New
	_ = urn.Parse

	_ guardrails.Finding
	_ guardrails.Guardrail
	_ guardrails.Output
	_ guardrails.Set
	_ = guardrails.Block
	_ = guardrails.Defaults
	_ = guardrails.Injection
	_ = guardrails.LimitsID
	_ = guardrails.Parse
	_ = guardrails.Secrets

	_ catalog.Item
	_ = catalog.Check
	_ = catalog.Guardrail
	_ = catalog.Locked
	_ = catalog.Skill
	_ = catalog.SkillFile

	_ secrets.Memory

	_ = jobspec.FileName
	_ = jobspec.Parse
	_ = jobspec.ParseSchedule
	_ = jobspec.Service

	_ enterprise.Options
	_ enterprise.Prompt
	_ = enterprise.APIResponses
	_ = enterprise.Discover
	_ = enterprise.New
)
