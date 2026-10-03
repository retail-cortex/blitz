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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// LogEntry is one record of a log file.
type LogEntry struct {
	Time  time.Time
	Level string // DEBUG, INFO, WARN or ERROR
	Msg   string
	// Attrs are the record's other fields, in the file's order, values as
	// text (strings bare, the rest as JSON).
	Attrs []LogAttr
}

// LogAttr is one field of a log record.
type LogAttr struct{ Key, Value string }

// LogQuery picks records from a day's log.
type LogQuery struct {
	// Day is YYYY-MM-DD; "" is the newest day there is.
	Day string
	// MinLevel keeps records at this level and above ("" or DEBUG: all).
	MinLevel string
	// Text keeps records containing every word of it, in any field,
	// ignoring case.
	Text string
	// Limit is the most records returned, newest first (0: 500).
	Limit int
}

// LogPage is what ReadLog found.
type LogPage struct {
	Day  string
	Path string
	// Entries match the query, newest first, at most Limit of them.
	Entries []LogEntry
	// Matched counts every record that matched (more than Entries when
	// Limit cut them).
	Matched int
}

// LogDays lists the days dir has a log for (YYYY-MM-DD), newest first.
func LogDays(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var days []string
	for _, e := range entries {
		day, ok := strings.CutPrefix(e.Name(), logPrefix)
		if day, ok2 := strings.CutSuffix(day, logSuffix); ok && ok2 && !e.IsDir() {
			if _, err := time.Parse(time.DateOnly, day); err == nil {
				days = append(days, day)
			}
		}
	}
	slices.Sort(days)
	slices.Reverse(days)
	return days, nil
}

// ErrLogInUse is returned when deleting the log being written: today's.
var ErrLogInUse = errors.New("today's log is still being written")

// ErrNoLogDay reports a day dir has no log for.
var ErrNoLogDay = errors.New("no log for that day")

// ErrBadLogDay reports a day that isn't a date (YYYY-MM-DD).
var ErrBadLogDay = errors.New("not a log day: use YYYY-MM-DD")

// DeleteLogDay deletes day's log (YYYY-MM-DD) in dir. Today's, by now's
// date, is the one being written and is refused (ErrLogInUse).
func DeleteLogDay(dir, day string, now time.Time) error {
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return fmt.Errorf("%w: %q", ErrBadLogDay, day)
	}
	if day >= now.Format(time.DateOnly) {
		return ErrLogInUse
	}
	err := os.Remove(filepath.Join(dir, logPrefix+day+logSuffix))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNoLogDay, day)
	}
	return err
}

var levelRank = map[string]slog.Level{"DEBUG": slog.LevelDebug, "INFO": slog.LevelInfo, "WARN": slog.LevelWarn, "ERROR": slog.LevelError}

// ReadLog reads the records of a day's log in dir that match q, newest
// first. Lines that aren't records (a torn last line) are skipped.
func ReadLog(dir string, q LogQuery) (LogPage, error) {
	day := q.Day
	if day == "" {
		days, err := LogDays(dir)
		if err != nil {
			return LogPage{}, err
		}
		if len(days) == 0 {
			return LogPage{}, nil
		}
		day = days[0]
	}
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return LogPage{}, fmt.Errorf("log day %q: use YYYY-MM-DD", day)
	}
	min, ok := levelRank[strings.ToUpper(q.MinLevel)]
	if q.MinLevel == "" {
		min, ok = slog.LevelDebug, true
	}
	if !ok {
		return LogPage{}, fmt.Errorf("log level %q: use debug, info, warn or error", q.MinLevel)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	words := strings.Fields(strings.ToLower(q.Text))
	page := LogPage{Day: day, Path: filepath.Join(dir, logPrefix+day+logSuffix)}
	f, err := os.Open(page.Path)
	if errors.Is(err, os.ErrNotExist) {
		return page, nil
	}
	if err != nil {
		return page, err
	}
	defer f.Close()
	var all []LogEntry
	err = eachLine(bufio.NewReaderSize(f, 64*1024), maxLogLine, func(line string) {
		e, ok := parseRecord(line)
		if !ok || levelRank[e.Level] < min || !containsAll(strings.ToLower(line), words) {
			return
		}
		all = append(all, e)
	})
	if err != nil {
		return page, err
	}
	page.Matched = len(all)
	for i := len(all) - 1; i >= 0 && len(page.Entries) < limit; i-- {
		page.Entries = append(page.Entries, all[i])
	}
	return page, nil
}

// maxLogLine is the longest record ReadLog reads; a longer one is skipped.
const maxLogLine = 4 * 1024 * 1024

// eachLine calls fn with each line of r (without its line ending), skipping
// lines longer than max rather than giving up on the rest.
func eachLine(r *bufio.Reader, max int, fn func(string)) error {
	var line []byte
	long := false
	for {
		chunk, err := r.ReadSlice('\n')
		if long = long || len(line)+len(chunk) > max; !long {
			line = append(line, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if !long && len(line) > 0 {
			fn(strings.TrimRight(string(line), "\r\n"))
		}
		line, long = line[:0], false
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func containsAll(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// parseRecord reads one line of slog's JSON, keeping its fields' order.
func parseRecord(line string) (LogEntry, bool) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return LogEntry{}, false
	}
	var e LogEntry
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return LogEntry{}, false
		}
		key, _ := t.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return LogEntry{}, false
		}
		var s string
		isString := json.Unmarshal(raw, &s) == nil
		switch {
		case key == slog.TimeKey && isString:
			e.Time, _ = time.Parse(time.RFC3339Nano, s)
		case key == slog.LevelKey && isString:
			e.Level = s
		case key == slog.MessageKey && isString:
			e.Msg = s
		case isString:
			e.Attrs = append(e.Attrs, LogAttr{key, s})
		default:
			e.Attrs = append(e.Attrs, LogAttr{key, string(raw)})
		}
	}
	return e, e.Level != "" && !e.Time.IsZero()
}
