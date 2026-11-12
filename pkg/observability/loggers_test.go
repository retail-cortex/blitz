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
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A logger writes at its most specific name's level in log.levels, else at
// log.level (LOG-01b).
func TestLoggerLevels(t *testing.T) {
	cfg := config.LogConfig{Level: "warn", Levels: map[string]string{"gateway": "debug", "gateway.token": "error", "quiet": "off"}}
	tests := []struct {
		logger string // "" is the root
		level  slog.Level
		want   bool
	}{
		{"", slog.LevelInfo, false},
		{"", slog.LevelWarn, true},
		{"gateway", slog.LevelDebug, true},
		{"gateway.call", slog.LevelDebug, true},
		{"gateway.token", slog.LevelWarn, false},
		{"gateway.token.script", slog.LevelError, true},
		{"gatewayish", slog.LevelInfo, false},
		{"engine", slog.LevelWarn, true},
		{"quiet", slog.LevelError, false},
	}
	for _, tc := range tests {
		t.Run(tc.logger+"/"+tc.level.String(), func(t *testing.T) {
			dir := t.TempDir()
			logger, closer, err := OpenLog(config.LogConfig{Level: cfg.Level, Levels: cfg.Levels, Dir: dir}, redact.New())
			require.NoError(t, err)
			if tc.logger != "" {
				logger = logger.With(LoggerKey, tc.logger)
			}
			logger.WithGroup("g").Log(t.Context(), tc.level, "line")
			require.NoError(t, closer.Close())
			if !tc.want {
				files, _ := filepath.Glob(filepath.Join(dir, logPrefix+"*"))
				assert.Empty(t, files, "nothing written, so no file")
				return
			}
			lines := readLines(t, dir)
			require.Len(t, lines, 1)
			if tc.logger != "" {
				assert.Equal(t, tc.logger, lines[0][LoggerKey])
			}
		})
	}
}

// A logger's level opens the file with log.level off, which still keeps
// the rest out; the extra handlers get the rest at info, the logger's at
// its level.
func TestLoggerLevelWithRootOff(t *testing.T) {
	dir := t.TempDir()
	extra := &recorder{}
	logger, closer, err := OpenLog(config.LogConfig{Level: "off", Levels: map[string]string{"gateway": "debug"}, Dir: dir}, redact.New(), extra)
	require.NoError(t, err)
	logger.Info("root info")
	logger.Debug("root debug")
	logger.With(LoggerKey, "gateway").Debug("gateway debug")
	require.NoError(t, closer.Close())

	lines := readLines(t, dir)
	require.Len(t, lines, 1)
	assert.Equal(t, "gateway debug", lines[0]["msg"])
	var got []string
	for _, r := range extra.recs {
		got = append(got, r.Message)
	}
	assert.Equal(t, []string{"root info", "gateway debug"}, got)
}

// Logger names the default logger's records.
func TestLogger(t *testing.T) {
	dir := t.TempDir()
	logger, closer, err := OpenLog(config.LogConfig{Level: "info", Levels: map[string]string{"gateway": "debug"}, Dir: dir}, redact.New())
	require.NoError(t, err)
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })

	Logger("gateway").Debug("call")
	Logger("engine").Debug("dropped")
	require.NoError(t, closer.Close())
	lines := readLines(t, dir)
	require.Len(t, lines, 1)
	assert.Equal(t, "gateway", lines[0][LoggerKey])
}

// A bad level in log.levels is an error naming it.
func TestLoggerLevelBad(t *testing.T) {
	_, _, err := OpenLog(config.LogConfig{Level: "info", Levels: map[string]string{"gateway": "loud"}, Dir: t.TempDir()}, redact.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "log.levels.gateway")
}
