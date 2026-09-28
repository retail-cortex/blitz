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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

// searchApp is a REPL app with web access, a SearXNG-style search server
// returning pages served locally, and no auto-approval: only the pages
// /search web hands over may be fetched without asking.
func searchApp(t *testing.T, input string, replies ...*genai.Content) (*App, *runtime.MockLLM, string) {
	t.Helper()
	pages := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "contents of %s", r.URL.Path)
	}))
	t.Cleanup(pages.Close)
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var results []string
		for _, p := range []string{"/one", "/one#dup", "/manual.pdf", "/two", "/three", "/four", "/five", "/six"} {
			results = append(results, fmt.Sprintf(`{"title":"Page %s","url":"%s%s","content":"about %s"}`, p, pages.URL, p, p))
		}
		io.WriteString(w, `{"results":[`+strings.Join(results, ",")+`]}`)
	}))
	t.Cleanup(search.Close)

	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Images.Dir = t.TempDir()
	cfg.Audit.Enabled = false
	cfg.Blitz.AutoApprove = false
	cfg.Tools.ApprovalsFile = ""
	cfg.Sandbox.AllowNetwork = true
	cfg.Web.Enabled, cfg.Web.AllowPrivate = true, true
	cfg.Web.SearchProvider, cfg.Web.SearchURL = "searxng", search.URL
	llm := runtime.NewMockLLM("gemini-3.8-flash", replies...)
	app := openApp(t, cfg, llm)
	app.Input = NewLineReader(strings.NewReader(input), io.Discard)
	return app, llm, pages.URL
}

// toolResults collects the function responses the model was sent, by tool.
func toolResults(llm *runtime.MockLLM) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	last := llm.Requests[len(llm.Requests)-1]
	for _, c := range last.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				out[p.FunctionResponse.Name] = append(out[p.FunctionResponse.Name], p.FunctionResponse.Response)
			}
		}
	}
	return out
}

func TestSearchWebHandsFiveReadableLinksToTheAgent(t *testing.T) {
	replies := []*genai.Content{
		toolCallContent("web_fetch", map[string]any{"url": "PAGES/two"}),                // handed over: no prompt
		toolCallContent("web_fetch", map[string]any{"url": "PAGES/six"}),                // not handed over: needs approval
		toolCallContent("create_file", map[string]any{"path": "x.txt", "content": "x"}), // read-only turn
		genai.NewContentFromText("Pages two says so.", genai.RoleModel),
	}
	app, llm, pages := searchApp(t, "/search\n/search web golang errors\n/exit\n", replies...)
	for _, r := range replies[:2] {
		r.Parts[0].FunctionCall.Args["url"] = strings.Replace(r.Parts[0].FunctionCall.Args["url"].(string), "PAGES", pages, 1)
	}
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	assert.Contains(t, out, "Usage: /search web <terms>", "usage:\n%s", out)
	assert.Contains(t, out, "Searching searxng for golang errors", "results shown:\n%s", out)
	assert.Contains(t, out, "5. Page /five", "results shown:\n%s", out)
	assert.NotContains(t, out, "manual.pdf", "results shown:\n%s", out)
	prompt := llm.Requests[0].Contents[len(llm.Requests[0].Contents)-1].Parts[0].Text
	for _, want := range []string{"I searched the web for: golang errors", pages + "/one", pages + "/five", "about /two"} {
		assert.Contains(t, prompt, want, "prompt lacks %q:\n%s", want, prompt)
	}
	assert.NotContains(t, prompt, "/six", "prompt has links it shouldn't:\n%s", prompt)
	assert.NotContains(t, prompt, "manual.pdf", "prompt has links it shouldn't:\n%s", prompt)
	assert.Equal(t, 1, strings.Count(prompt, pages+"/one"), "prompt has links it shouldn't:\n%s", prompt)

	res := toolResults(llm)
	fetches := res["web_fetch"]
	assert.Len(t, fetches, 2, "web_fetch results: %v", fetches)
	assert.Equal(t, "contents of /two", fetches[0]["content"], "web_fetch results: %v", fetches)
	assert.Contains(t, fmt.Sprint(fetches[1]["error"]), "approv", "web_fetch results: %v", fetches)
	cf := res["create_file"]
	assert.Len(t, cf, 1, "create_file result: %v", cf)
	assert.Contains(t, fmt.Sprint(cf[0]["error"]), "search is read-only", "create_file result: %v", cf)
	msgs := local(app).Storage().Active().Messages
	assert.GreaterOrEqual(t, len(msgs), 1, "transcript: %+v", msgs)
	assert.Equal(t, "/search web golang errors", msgs[0].Content, "transcript: %+v", msgs)
}

func TestSearchSessionSendsMatchingPassages(t *testing.T) {
	app, llm, _ := searchApp(t, "/search session pineapple\n/search session mango\n/exit\n",
		genai.NewContentFromText("You chose pineapple on day one.", genai.RoleModel),
		genai.NewContentFromText("Mango never came up.", genai.RoleModel))
	st := local(app).Storage()
	st.CreateSession("", "t", "blitz")
	st.AddMessage("user", "let's pick a fruit")
	st.AddMessage("model", "I suggest PINEAPPLE for the demo")
	st.AddMessage("user", "ok")

	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	assert.Contains(t, out, "Found 1 message about it in this session", "output:\n%s", out)
	assert.Contains(t, out, "Nothing in this session's transcript mentions it", "output:\n%s", out)
	first := llm.Requests[0].Contents[len(llm.Requests[0].Contents)-1].Parts[0].Text
	assert.Contains(t, first, "Look back through this conversation for: pineapple", "first prompt:\n%s", first)
	assert.Contains(t, first, "[message 2, model,", "first prompt:\n%s", first)
	assert.Contains(t, first, "I suggest PINEAPPLE", "first prompt:\n%s", first)
	second := llm.Requests[1].Contents[len(llm.Requests[1].Contents)-1].Parts[0].Text
	assert.Contains(t, second, "No message in the session transcript contains these words", "second prompt:\n%s", second)
}
