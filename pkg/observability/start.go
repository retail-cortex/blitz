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

// SecretRedactor masks configured credentials and the values of scrubbed
// environment variables in audit entries, logs and telemetry.
func SecretRedactor(cfg *config.Config) *redact.Redactor {
	secrets := []string{cfg.LLM.Gemini.APIKey, cfg.LLM.OpenAI.APIKey, cfg.LLM.Anthropic.APIKey, cfg.Web.SearchAPIKey}
	for _, s := range cfg.MCP.Servers {
		for _, v := range s.Env {
			secrets = append(secrets, v)
		}
	}
	return redact.FromEnv(cfg.Sandbox.ScrubEnv, secrets...)
}
