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
	"fmt"
	"log/slog"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
)

// LoggerKey is the attribute naming a record's logger ("gateway"), which
// log.levels sets a level for (LOG-01b).
const LoggerKey = "logger"

// Logger is the default logger named name, a dotted path ("gateway",
// "gateway.token"): its records carry the name, and log.levels can set its
// level apart from the rest. It's looked up at each call, so a package can
// keep a name before the service has opened its log.
func Logger(name string) *slog.Logger {
	return slog.Default().With(LoggerKey, name)
}

// offLevel is a level no record reaches: "off".
const offLevel = slog.Level(1 << 30)

// levels is the log's root level and its loggers'.
type levels struct {
	root  slog.Level
	named map[string]slog.Level
}

// parseLevels reads cfg's level and log.levels.
func parseLevels(cfg config.LogConfig) (levels, error) {
	ls := levels{root: offLevel}
	if l, on, err := ParseLevel(cfg.Level); err != nil {
		return levels{}, err
	} else if on {
		ls.root = l
	}
	for name, s := range cfg.Levels {
		l, on, err := ParseLevel(s)
		if err != nil {
			return levels{}, fmt.Errorf("log.levels.%s: %w", name, err)
		}
		if !on {
			l = offLevel
		}
		if ls.named == nil {
			ls.named = map[string]slog.Level{}
		}
		ls.named[name] = l
	}
	return ls, nil
}

// of is the level of the logger named name: its most specific name's in
// the table ("gateway.token", then "gateway"), else the root's.
func (ls levels) of(name string) slog.Level {
	for name != "" {
		if l, ok := ls.named[name]; ok {
			return l
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[:i]
	}
	return ls.root
}

// lowest is the lowest level any logger writes at.
func (ls levels) lowest() slog.Level {
	low := ls.root
	for _, l := range ls.named {
		low = min(low, l)
	}
	return low
}

// loggerHandler applies the level of the logger its records come from (the
// LoggerKey attribute given to With) to a handler that takes them all.
type loggerHandler struct {
	slog.Handler
	levels levels
	name   string
}

// Enabled reports whether the logger writes at l.
func (h loggerHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.levels.of(h.name) && h.Handler.Enabled(ctx, l)
}

// WithAttrs keeps the logger's name when attrs give one.
func (h loggerHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	name := h.name
	for _, a := range attrs {
		if a.Key == LoggerKey && a.Value.Kind() == slog.KindString {
			name = a.Value.String()
		}
	}
	return loggerHandler{h.Handler.WithAttrs(attrs), h.levels, name}
}

// WithGroup keeps the logger's name.
func (h loggerHandler) WithGroup(group string) slog.Handler {
	return loggerHandler{h.Handler.WithGroup(group), h.levels, h.name}
}
