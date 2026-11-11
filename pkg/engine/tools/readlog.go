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

package tools

import (
	"fmt"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/genai"
)

// maxReadLogAgents bounds how many conversations' agents the read log
// remembers; the oldest is forgotten first, and a forgotten one only means
// its next read of a file returns the content again.
const maxReadLogAgents = 64

// ReadLog remembers which read_file results each agent of a conversation
// was last sent, so a read of an unchanged file it can still see answers
// "unchanged" instead of sending the content again (FS-83). A tool can't
// see the conversation, so the engine's before-model hook gives the log
// each request (Shown), and the log keeps the reads it carries, found by
// the digest and lines each result names: a summary, a rewind or a
// handoff leaves a read out, and that file's next read returns the
// content. Call IDs aren't used: ADK takes the ones it made up out of
// requests, and Gemini sends none.
type ReadLog struct {
	mu     sync.Mutex
	agents map[string][]fileRead
	order  []string // agents' keys, oldest first
}

// fileRead is the part of a file one read_file result holds.
type fileRead struct {
	digest     string // the file's path and content, when it was read
	start, end int    // the lines returned, 1-based and inclusive
}

// ReadKey names the agent ctx runs, in its conversation: a sub-agent or
// the agent handed to has its own context, so its own reads. It's false
// for a call outside a conversation (ADK's contexts panic asking for a
// session they haven't got), whose reads aren't remembered.
func ReadKey(ctx agent.Context) (key string, ok bool) {
	defer func() {
		if recover() != nil {
			key, ok = "", false
		}
	}()
	if ctx == nil {
		return "", false
	}
	session := ctx.SessionID()
	return session + "\x00" + ctx.Branch() + "\x00" + ctx.AgentName(), session != ""
}

// Shown records the read_file results a request to the model carries for
// the agent ctx runs, in place of those the last one carried. Results of
// other agents come as text ("For context: ..."), so only its own count.
func (l *ReadLog) Shown(ctx agent.Context, contents []*genai.Content) {
	key, ok := ReadKey(ctx)
	if l == nil || !ok {
		return
	}
	var reads []fileRead
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			if p == nil || p.FunctionResponse == nil || p.FunctionResponse.Name != "read_file" {
				continue
			}
			if r, ok := shownRead(p.FunctionResponse.Response); ok {
				reads = append(reads, r)
			}
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, known := l.agents[key]; !known {
		if len(reads) == 0 {
			return
		}
		if l.agents == nil {
			l.agents = map[string][]fileRead{}
		}
		if len(l.order) >= maxReadLogAgents {
			delete(l.agents, l.order[0])
			l.order = l.order[1:]
		}
		l.order = append(l.order, key)
	}
	l.agents[key] = reads
}

// shownRead is the read a read_file result names: its digest and lines.
// A result without them (an error, an "unchanged" answer, a truncated
// read, a PDF's text) names none.
func shownRead(resp map[string]any) (fileRead, bool) {
	digest, _ := resp["digest"].(string)
	lines, _ := resp["lines"].(string)
	var r fileRead
	if _, err := fmt.Sscanf(lines, "%d-%d", &r.start, &r.end); digest == "" || err != nil {
		return fileRead{}, false
	}
	r.digest = digest
	return r, true
}

// covering returns a read the agent was last shown that holds lines start
// to end of the file with digest, if there is one.
func (l *ReadLog) covering(key, digest string, start, end int) (fileRead, bool) {
	if l == nil {
		return fileRead{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.agents[key] {
		if r.digest == digest && r.start <= start && end <= r.end {
			return r, true
		}
	}
	return fileRead{}, false
}
