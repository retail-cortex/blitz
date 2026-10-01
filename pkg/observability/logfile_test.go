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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every level name maps to its slog level; off writes nothing.
func TestParseLevel(t *testing.T) {
	cases := map[string]struct {
		level slog.Level
		on    bool
	}{
		"off": {0, false}, "none": {0, false}, "": {slog.LevelInfo, true}, "INFO": {slog.LevelInfo, true},
		"debug": {slog.LevelDebug, true}, "warn": {slog.LevelWarn, true}, " warning ": {slog.LevelWarn, true},
		"error": {slog.LevelError, true},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			level, on, err := ParseLevel(in)
			require.NoError(t, err)
			assert.Equal(t, want.level, level)
			assert.Equal(t, want.on, on)
		})
	}
}

// OpenLog reports a log directory it can't make.
func TestOpenLogBadDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, _, err := OpenLog(config.LogConfig{Level: "info", Dir: filepath.Join(file, "logs")}, redact.New())
	assert.ErrorContains(t, err, "create log dir")
}

// recorder is a slog handler that keeps what it's given.
type recorder struct {
	attrs  []slog.Attr
	groups []string
	recs   []slog.Record
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.recs = append(r.recs, rec)
	return nil
}
func (r *recorder) WithAttrs(a []slog.Attr) slog.Handler { r.attrs = append(r.attrs, a...); return r }
func (r *recorder) WithGroup(g string) slog.Handler      { r.groups = append(r.groups, g); return r }

// With a file and an extra handler, records go to both at the file's
// level, through WithAttrs and WithGroup too.
func TestOpenLogFileAndExtra(t *testing.T) {
	dir := t.TempDir()
	extra := &recorder{}
	logger, closer, err := OpenLog(config.LogConfig{Level: "warn", Dir: dir}, redact.New(), extra)
	require.NoError(t, err)
	logger = logger.With("component", "test").WithGroup("g")
	logger.Info("dropped: below warn")
	logger.Warn("kept", "k", "v")
	require.NoError(t, closer.Close())

	require.Len(t, extra.recs, 1)
	assert.Equal(t, "kept", extra.recs[0].Message)
	assert.Equal(t, []string{"g"}, extra.groups)
	require.Len(t, extra.attrs, 1)
	assert.Equal(t, "component", extra.attrs[0].Key)
	lines := readLines(t, dir)
	require.Len(t, lines, 1)
	assert.Equal(t, "kept", lines[0]["msg"])
	assert.Equal(t, "test", lines[0]["component"])
	assert.Equal(t, map[string]any{"k": "v"}, lines[0]["g"])
}

// A log file that can't be opened loses its lines, and nothing breaks.
func TestFileSinkCantOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := newFileSink(dir, 0)
	require.NoError(t, err)
	day := time.Now().Format("2006-01-02")
	require.NoError(t, os.Mkdir(filepath.Join(dir, logPrefix+day+logSuffix), 0o700)) // a directory where the file goes
	_, err = s.Write([]byte("line\n"))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	_, err = s.Write([]byte("after close\n"))
	assert.NoError(t, err, "writes after close are ignored")
}
