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

// SecretRedactor masks the settings' credentials (config.EachSecret: the
// providers' API keys, Azure's included, the web search key, the api
// gateway's and http hooks' headers, MCP servers' environment and header
// values) and the values of scrubbed environment variables in audit
// entries, logs and telemetry.
func SecretRedactor(cfg *config.Config) *redact.Redactor {
	keys, named := configSecrets(cfg)
	for _, v := range named {
		keys = append(keys, v.value)
	}
	return redact.FromEnv(cfg.Sandbox.ScrubEnv, withTokens(keys)...)
}

// RememberSecrets has every redactor mask cfg's credentials from now on
// (redact.Remember): the process's diagnostic log and telemetry, whose
// redactor is built from the global settings when the service starts,
// included. Workspaces call it as they open and as their settings change,
// so keys set later in Settings, a workspace's own keys, and the tokens of
// MCP servers added later or defined in a workspace are masked there too.
// Of the headers' and MCP servers' env values, only those that look secret
// are remembered (an Authorization header, a name like a key or token):
// the remembered values are also masked in text shown as it is.
func RememberSecrets(cfg *config.Config) {
	keys, named := configSecrets(cfg)
	for _, v := range named {
		if strings.EqualFold(v.name, "Authorization") || secretName.MatchString(v.name) {
			keys = append(keys, v.value)
		}
	}
	redact.Remember(withTokens(keys)...)
}

// secretName is an environment variable's or header's name that says its
// value is a credential.
var secretName = regexp.MustCompile(`(?i)key|token|secret|passw|credential|auth`)

// namedSecret is a secret value from a map, with its name.
type namedSecret struct{ name, value string }

// configSecrets are cfg's credentials, from config.EachNamedSecret, the one
// list of what's secret: the single ones (keys) and the maps' values with
// their names (named).
func configSecrets(cfg *config.Config) (keys []string, named []namedSecret) {
	cp := *cfg
	cp.EachNamedSecret(func(name, v string) string {
		switch {
		case v == "":
		case name == "":
			keys = append(keys, v)
		default:
			named = append(named, namedSecret{name, v})
		}
		return v
	})
	return keys, named
}

// withTokens is values with a "Bearer <token>" value's token alone too, as
// it may be logged apart.
func withTokens(values []string) []string {
	out := values
	for _, v := range values {
		if _, token, ok := strings.Cut(v, " "); ok {
			out = append(out, strings.TrimSpace(token))
		}
	}
	return out
}
