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

package observability

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
)

// StartProcess opens the diagnostic log and, when enabled, OpenTelemetry export,
// and makes the log the slog default. The returned func flushes both.
// Failures only disable the affected part: diagnostics must never stop a
// session.
func StartProcess(ctx context.Context, cfg *config.Config, version string, warn func(string)) func() {
	r := SecretRedactor(cfg)
	tel, err := StartTelemetry(ctx, cfg.Telemetry, version, r)
	if err != nil {
		warn("telemetry disabled: " + err.Error())
	}
	logger, logFile, err := OpenLog(cfg.Log, r, tel.LogHandler())
	if err != nil {
		warn("diagnostic log disabled: " + err.Error())
		logger, logFile, _ = OpenLog(config.LogConfig{Level: "off"}, r, tel.LogHandler())
	}
	slog.SetDefault(logger)
	slog.Info("start", "version", version, "provider", cfg.LLM.Provider, "model", cfg.ModelName(), "telemetry", tel != nil)
	return func() {
		if err := tel.Shutdown(context.WithoutCancel(ctx)); err != nil {
			slog.Debug("telemetry shutdown", "error", err)
		}
		logFile.Close()
	}
}

// SecretRedactor masks configured credentials (the providers' API keys,
// Azure's included, and the web search key), the values of scrubbed
// environment variables, and MCP servers' environment and header values
// (tokens, often) in audit entries, logs and telemetry.
func SecretRedactor(cfg *config.Config) *redact.Redactor {
	keys, mcp := configSecrets(cfg)
	return redact.FromEnv(cfg.Sandbox.ScrubEnv, append(keys, mcp...)...)
}

// RememberSecrets has every redactor mask cfg's credentials from now on
// (redact.Remember): the process's diagnostic log and telemetry, whose
// redactor is built from the global settings when the service starts,
// included. Workspaces call it as they open and as their settings change,
// so keys set later in Settings, a workspace's own keys, and the tokens of
// MCP servers added later or defined in a workspace are masked there too.
// Of MCP servers' values, only those that look secret are remembered (an
// Authorization header, an env or header named like a key or token): the
// remembered values are also masked in text shown as it is.
func RememberSecrets(cfg *config.Config) {
	keys, _ := configSecrets(cfg)
	for _, s := range cfg.MCP.Servers {
		for k, v := range s.Env {
			if secretName.MatchString(k) {
				keys = append(keys, v)
			}
		}
		for k, v := range s.Headers {
			if strings.EqualFold(k, "Authorization") || secretName.MatchString(k) {
				keys = append(keys, v)
				if _, token, ok := strings.Cut(v, " "); ok {
					keys = append(keys, strings.TrimSpace(token))
				}
			}
		}
	}
	redact.Remember(keys...)
}

// secretName is an environment variable's or header's name that says its
// value is a credential.
var secretName = regexp.MustCompile(`(?i)key|token|secret|passw|credential|auth`)

// configSecrets are cfg's credentials (keys) and its MCP servers' env and
// header values, with a header's "Bearer <token>" token alone too (mcp).
func configSecrets(cfg *config.Config) (keys, mcp []string) {
	keys = []string{cfg.LLM.Gemini.APIKey, cfg.LLM.OpenAI.APIKey, cfg.LLM.Anthropic.APIKey, cfg.LLM.Azure.APIKey, cfg.Web.SearchAPIKey}
	for _, s := range cfg.MCP.Servers {
		for _, v := range s.Env {
			mcp = append(mcp, v)
		}
		for _, v := range s.Headers {
			mcp = append(mcp, v)
			// "Bearer <token>": the token alone too, as it may be logged apart.
			if _, token, ok := strings.Cut(v, " "); ok {
				mcp = append(mcp, strings.TrimSpace(token))
			}
		}
	}
	return keys, mcp
}
