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

package tui

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genai"
)

// /btw answers from the session's context and leaves nothing behind: not
// in the transcript, not in what the next prompt sends.
func TestBtwIsAnsweredAndForgotten(t *testing.T) {
	app, llm := newCommandApp(t, "remember pineapple\n/btw\n/btw what was the word?\ncarry on\n/exit\n",
		genai.NewContentFromText("noted", genai.RoleModel),
		genai.NewContentFromText("it was pineapple", genai.RoleModel),
		genai.NewContentFromText("carrying on", genai.RoleModel))
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	if !strings.Contains(out, "Usage: /btw <question>") || !strings.Contains(out, "Side question") {
		t.Fatalf("output:\n%s", out)
	}
	if llm.Calls() != 3 {
		t.Fatalf("%d model calls", llm.Calls())
	}
	aside := requestTextAt(llm, 1)
	if !strings.Contains(aside, "remember pineapple") || !strings.Contains(aside, "what was the word?") {
		t.Fatalf("the side question lacked context:\n%s", aside)
	}
	next := requestTextAt(llm, 2)
	if strings.Contains(next, "what was the word") || strings.Contains(next, "it was pineapple") {
		t.Fatalf("the next prompt saw the side question:\n%s", next)
	}
	var recorded []string
	for _, m := range local(app).Storage().Active().Messages {
		recorded = append(recorded, m.Content)
	}
	if got := strings.Join(recorded, "|"); got != "remember pineapple|noted|carry on|carrying on" {
		t.Fatalf("transcript: %s", got)
	}
}
