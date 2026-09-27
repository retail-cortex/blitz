package engine

import (
	"context"
	"log/slog"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/observability"
)

// StartObservability opens the diagnostic log and, when enabled, OpenTelemetry
// export, and makes the log the slog default. The returned func flushes both.
// Failures only disable the affected part: diagnostics must never stop a session.
func StartObservability(ctx context.Context, cfg *config.Config, version string, warn func(string)) func() {
	r := SecretRedactor(cfg)
	tel, err := observability.StartTelemetry(ctx, cfg.Telemetry, version, r)
	if err != nil {
		warn("telemetry disabled: " + err.Error())
	}
	logger, logFile, err := observability.OpenLog(cfg.Log, r, tel.LogHandler())
	if err != nil {
		warn("diagnostic log disabled: " + err.Error())
		logger, logFile, _ = observability.OpenLog(config.LogConfig{Level: "off"}, r, tel.LogHandler())
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
