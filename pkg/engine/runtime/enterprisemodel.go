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

// newEnterpriseModel is one of the organization's models, through its
// service's gateway, signed with the person's own session
// (spec_053_enterprise EN-20). A model the settings name that the
// organization doesn't offer gives way to its default.
func newEnterpriseModel(ctx context.Context, cfg *config.Config, name string, pol retryPolicy) (model.LLM, error) {
	c, err := enterpriseClient(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Org.Expired(time.Now()) {
		// Never without the organization's rules (EN-33).
		return nil, fmt.Errorf("your organization's catalog is out of date (it expired %s): its models wait until Blitz reaches %s again", cfg.Org.Expires.Local().Format(time.DateTime), c.URL())
	}
	st := c.Status()
	if !st.SignedIn {
		return nil, fmt.Errorf("sign in to your organization (%s) to use its models: Settings › Organization, or blitz enterprise login", c.URL())
	}
	list, err := c.Models(ctx)
	if err != nil {
		return nil, fmt.Errorf("your organization's models: %w", err)
	}
	m, ok := enterprise.Find(list, name)
	if !ok {
		return nil, fmt.Errorf("your organization (%s) offers you no models: ask its admins", c.URL())
	}
	p := &apiProvider{
		cfg:        config.APIConfig{BaseURL: c.URL() + "/api/v1", Model: m.ID, Headers: map[string]string{"Authorization": "Bearer " + config.TokenPlaceholder}},
		format:     formatResponses,
		source:     c,
		messagesAt: "/messages",
	}
	if m.API == enterprise.APIMessages {
		p.format = formatMessages
	}
	return buildAPIModel(ctx, p, m.ID, pol)
}
