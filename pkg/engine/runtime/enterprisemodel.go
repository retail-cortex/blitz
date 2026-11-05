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

package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"google.golang.org/adk/v2/model"
)

// enterpriseClient is the process's connection to the organization's
// service in cfg: blitzd's, registered with its store, or one reading
// this user's keychain.
func enterpriseClient(cfg *config.Config) (*enterprise.Client, error) {
	u, err := enterprise.Normalize(cfg.Enterprise.URL)
	if err != nil {
		return nil, fmt.Errorf("[enterprise] url: %w", err)
	}
	return enterprise.Shared(u, func() secrets.Store { return secrets.Default(config.ConfigDir("")) }, enterprise.Options{}), nil
}

// modelsTimeout bounds asking the organization for its models as a
// workspace opens.
var modelsTimeout = 8 * time.Second

// newEnterpriseModel is one of the organization's models, through its
// service's gateway, signed with the person's own session
// (spec_053_enterprise EN-20). A model the settings name that the
// organization doesn't offer gives way to its default.
func newEnterpriseModel(ctx context.Context, cfg *config.Config, name string, pol retryPolicy) (model.LLM, error) {
	c, err := enterpriseClient(cfg)
	if err != nil {
		return nil, err
	}
	st := c.Status()
	switch {
	case cfg.Org.Problem != "" && st.SignedIn:
		// Never without the organization's rules (EN-33).
		return nil, fmt.Errorf("your organization's policy can't be read (%s): its models wait until Blitz fetches it from %s again", cfg.Org.Problem, c.URL())
	case cfg.Org.Problem == "" && cfg.Org.Expired(time.Now()):
		return nil, fmt.Errorf("your organization's catalog is out of date (it expired %s): its models wait until Blitz reaches %s again", cfg.Org.Expires.Local().Format(time.DateTime), c.URL())
	case !st.SignedIn:
		return nil, fmt.Errorf("sign in to your organization (%s) to use its models: Settings › Organization, or blitz enterprise login", c.URL())
	}
	// The workspace opens on this: a service that doesn't answer leaves
	// it without the organization's models (EN-37), never waiting long.
	mctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	list, err := c.Models(mctx)
	if err != nil {
		return nil, fmt.Errorf("your organization's models: %w", err)
	}
	m, ok := enterprise.Find(list, name)
	if !ok {
		return nil, fmt.Errorf("your organization (%s) offers you no models: ask its admins", c.URL())
	}
	headers := map[string]string{"Authorization": "Bearer " + config.TokenPlaceholder}
	if cfg.Tools.OrgWorkspace != "" {
		// The organization's workspace the call is for, so the service
		// applies its guardrails and limits (EN-22).
		headers["Blitz-Workspace"] = cfg.Tools.OrgWorkspace
	}
	p := &apiProvider{
		cfg:        config.APIConfig{BaseURL: c.URL() + "/api/v1", Model: m.ID, Headers: headers},
		format:     formatResponses,
		source:     c,
		messagesAt: "/messages",
	}
	if m.API == enterprise.APIMessages {
		p.format = formatMessages
	}
	return buildAPIModel(ctx, p, m.ID, pol)
}
