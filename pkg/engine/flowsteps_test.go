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

package engine

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// testRun is a run of f for calling the scheduler's parts directly.
func testRun(t *testing.T, f flows.Flow) *flowRun {
	t.Helper()
	order, err := flows.Order(f)
	require.NoError(t, err)
	return &flowRun{f: f, order: order, on: func(api.Event) {}, runID: "run-test"}
}

// An API call's requests and failures: an API key in the query or a
// header, basic auth, the input as a POST's body, a text body, and each
// way a call fails before or after it's made.
func TestFlowAPIStepCases(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		got = append(got, r.Method+" "+r.URL.RawQuery+" "+r.Header.Get("X-Key")+" "+user+":"+pass+" "+string(body))
		switch r.URL.Path {
		case "/fail":
			http.Error(rw, "broken", http.StatusInternalServerError)
		case "/token":
			http.Error(rw, "no", http.StatusUnauthorized)
		default:
			_, _ = rw.Write([]byte("plain"))
		}
	}))
	defer srv.Close()
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gone := "http://" + closed.Addr().String()
	require.NoError(t, closed.Close())
	w := openCalls(t, nil)

	ok := []struct {
		name string
		node flows.Node
		want string
	}{
		{name: "an API key in the query", node: flows.Node{URL: srv.URL + "/a", Auth: &flows.Auth{Type: "api_key", In: "query", Name: "key", Value: "{secrets:shop}"}}, want: "GET key=tok-12345678  : "},
		{name: "an API key in a header", node: flows.Node{URL: srv.URL + "/a", Auth: &flows.Auth{Type: "api_key", Name: "X-Key", Value: "k1"}}, want: "GET  k1 : "},
		{name: "basic auth", node: flows.Node{URL: srv.URL + "/a", Auth: &flows.Auth{Type: "basic", Username: "ada", Password: "pw"}}, want: "GET   ada:pw "},
		{name: "the input as a POST's body", node: flows.Node{URL: srv.URL + "/a", Method: "post"}, want: "POST   : the input"},
		{name: "a text body", node: flows.Node{URL: srv.URL + "/a", Method: "PUT", Body: "hello"}, want: "PUT   : hello"},
		{name: "a JSON list", node: flows.Node{URL: srv.URL + "/a", Method: "POST", JSON: []any{"{secrets:shop}", 1}}, want: `POST   : ["tok-12345678",1]`},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			got = nil
			res, err := w.apiStep(context.Background(), c.node, "the input")
			require.NoError(t, err)
			assert.Equal(t, "plain", res.Text)
			assert.Equal(t, "plain", res.Data.(map[string]any)["body"], "a body that isn't JSON is text")
			require.Len(t, got, 1)
			assert.Equal(t, c.want, got[0])
		})
	}

	fails := []struct {
		name string
		node flows.Node
		want string
	}{
		{name: "a bad URL", node: flows.Node{URL: "http://[::1"}, want: "the URL"},
		{name: "JSON that can't be written", node: flows.Node{URL: srv.URL, JSON: map[string]any{"n": math.Inf(1)}}, want: "the JSON"},
		{name: "a bad method", node: flows.Node{URL: srv.URL, Method: "NO GOOD"}, want: "invalid method"},
		{name: "an error status", node: flows.Node{URL: srv.URL + "/fail"}, want: "500 Internal Server Error"},
		{name: "nobody listening", node: flows.Node{URL: gone}, want: "connect"},
		{name: "a bad token URL", node: flows.Node{URL: srv.URL, Auth: &flows.Auth{Type: "oauth2", TokenURL: "http://[::1"}}, want: "the token URL"},
		{name: "a token refused", node: flows.Node{URL: srv.URL, Auth: &flows.Auth{Type: "oauth2", TokenURL: srv.URL + "/token", ClientID: "c", ClientSecret: "s"}}, want: "getting an OAuth2 token"},
	}
	for _, c := range fails {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.apiStep(context.Background(), c.node, "")
			assert.ErrorContains(t, err, c.want)
		})
	}

	// The token URL's host is asked about as a request's is: refused.
	_, err = w.apiStep(context.Background(), flows.Node{URL: srv.URL, Auth: &flows.Auth{Type: "oauth2", TokenURL: "https://auth.example/token"}}, "")
	assert.Error(t, err)

	off, _ := openTestWith(t, configtest.RunTools)
	err = off.authorize(context.Background(), httptest.NewRequest("GET", "/", nil), &flows.Auth{Type: "oauth2", TokenURL: srv.URL})
	assert.Error(t, err, "nobody can approve it")
}

// An A2A call reaches only approved hosts: its card's, and the ones it
// names once they're approved; a card that isn't there fails the step.
func TestFlowA2AStepCases(t *testing.T) {
	_, err := hostsOnly{hosts: []string{"a.example"}}.RoundTrip(httptest.NewRequest("GET", "http://b.example/", nil))
	assert.ErrorContains(t, err, "b.example wasn't approved")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := &url.URL{Scheme: "http", Host: ln.Addr().String()}
	card := &a2a.AgentCard{Name: "elsewhere", Version: "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{{URL: "https://agents.example/invoke", ProtocolBinding: a2a.TransportProtocolJSONRPC, ProtocolVersion: a2a.Version}},
		DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"}}
	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	hs := &http.Server{Handler: mux}
	go func() { _ = hs.Serve(ln) }()
	defer hs.Close()
	none := httptest.NewServer(http.NotFoundHandler())
	defer none.Close()
	w := openCalls(t, nil)

	cases := []struct{ name, url, want string }{
		{name: "a bad URL", url: "http://[::1", want: "the URL"},
		{name: "no card", url: none.URL, want: "the agent card"},
		{name: "a host the card names, refused", url: base.String(), want: "not approved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.a2aStep(context.Background(), flows.Node{ID: "ask", URL: c.url}, "hi")
			assert.ErrorContains(t, err, c.want)
		})
	}
	off, _ := openTestWith(t, configtest.RunTools)
	_, err = off.a2aStep(context.Background(), flows.Node{ID: "ask", URL: base.String()}, "hi")
	assert.Error(t, err, "nobody can approve it")
}

// Every templated setting of a step is filled in, and a bad template in
// any of them is the step's error.
func TestFillStepErrors(t *testing.T) {
	vars := flows.Vars{Input: "42"}
	bad := "{{ nope }}"
	cases := []struct {
		name string
		node flows.Node
	}{
		{name: "instructions", node: flows.Node{Instructions: bad}},
		{name: "path", node: flows.Node{Path: bad}},
		{name: "args", node: flows.Node{Args: []string{"ok", bad}}},
		{name: "url", node: flows.Node{URL: bad}},
		{name: "body", node: flows.Node{Body: bad}},
		{name: "query", node: flows.Node{Query: []flows.Pair{{Name: "q", Value: bad}}}},
		{name: "headers", node: flows.Node{Headers: []flows.Pair{{Name: "h", Value: bad}}}},
		{name: "arguments", node: flows.Node{Arguments: map[string]any{"a": bad}}},
		{name: "json", node: flows.Node{JSON: map[string]any{"a": bad}}},
		{name: "auth", node: flows.Node{Auth: &flows.Auth{Type: "bearer", Token: bad}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := fillStep(c.node, vars)
			assert.ErrorContains(t, err, "nope")
		})
	}
	n, err := fillStep(flows.Node{Args: []string{"{{ input }}"}, Arguments: map[string]any{"n": "{{ input }}"}, JSON: []any{"{{ input }}"}, Auth: &flows.Auth{Token: "t{{ input }}"}}, vars)
	require.NoError(t, err)
	assert.Equal(t, []string{"42"}, n.Args)
	assert.Equal(t, map[string]any{"n": "42"}, n.Arguments)
	assert.Equal(t, "t42", n.Auth.Token)
}

// Which links a step's result takes: an exclusive gateway's first link
// that holds (not a later one), its unconditioned link when none holds
// and there's no default, and a condition that can't be evaluated.
func TestFlowRoute(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	f := flows.Flow{Name: "r", Nodes: []flows.Node{{ID: "g", Type: flows.Exclusive}, {ID: "a"}, {ID: "b"}, {ID: "c"}}, Edges: []flows.Edge{
		{From: "g", To: "a", When: "output.data.n > 1"},
		{From: "g", To: "b", When: "output.data.n > 2"},
		{From: "g", To: "c"},
	}}
	r := testRun(t, f)
	out := flows.Outputs(f, "g")
	cases := []struct {
		name string
		n    float64
		want map[int]bool
	}{
		{name: "the first that holds", n: 5, want: map[int]bool{0: true}},
		{name: "none holds: the unconditioned link", n: 0, want: map[int]bool{2: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			taken, err := w.route(context.Background(), r, f.Nodes[0], out, flows.Result{Data: map[string]any{"n": c.n}}, map[string]flows.Result{})
			require.NoError(t, err)
			for i := range f.Edges {
				assert.Equal(t, c.want[i], taken[i], "link %d", i)
			}
		})
	}
	f.Edges[0].When = "output.text"
	_, err := w.route(context.Background(), testRun(t, f), f.Nodes[0], out, flows.Result{Text: "x"}, map[string]flows.Result{})
	assert.ErrorContains(t, err, "the link to a")
}

// A link whose condition can't be evaluated stops the run, and the steps
// running beside it are let end.
func TestFlowConditionStopsRun(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	writeFlow(t, w, "bad", `nodes:
  - {id: a, type: accumulate}
  - {id: b, type: accumulate}
  - {id: c, type: accumulate}
edges:
  - {from: a, to: c, when: "output.text"}
`)
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/bad x", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "the link to c")
}

// A repeated step fails with the item that failed: its list, a template,
// or the step itself.
func TestFlowForEachFailures(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	r := testRun(t, flows.Flow{Name: "e", Nodes: []flows.Node{{ID: "a"}}})
	vars := flows.Vars{Input: "x"}
	cases := []struct {
		name string
		node flows.Node
		want string
	}{
		{name: "a bad list", node: flows.Node{ID: "a", Type: flows.Accumulate, ForEach: "nope"}, want: "for_each"},
		{name: "a bad template", node: flows.Node{ID: "a", Type: flows.ReadFile, ForEach: "[1, 2]", Path: "{{ nope }}"}, want: "item 1"},
		{name: "a failing item", node: flows.Node{ID: "a", Type: flows.ReadFile, ForEach: "[1]", Path: "missing-{{ item }}.md"}, want: "item 1: reading missing-1.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.forEach(context.Background(), r, c.node, "x", vars)
			assert.ErrorContains(t, err, c.want)
		})
	}
	res, err := w.forEach(context.Background(), r, flows.Node{ID: "a", Type: flows.Accumulate, ForEach: "[1, 2]"}, "in", vars)
	require.NoError(t, err)
	assert.Equal(t, []any{"in", "in"}, res.Data, "each run's text, when it has no data")
}

// A call step: a workflow that isn't there, one with a problem, one too
// deep, one that fails, and one whose several last steps' texts are joined.
func TestFlowCallStepCases(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	writeFlow(t, w, "two", "nodes: [{id: a, type: accumulate, title: A}, {id: b, type: accumulate}]\n")
	writeFlow(t, w, "broken", "nodes: [{id: a, type: agent}]\n")
	writeFlow(t, w, "fails", "nodes: [{id: a, type: read_file, title: Read, path: missing.md}]\n")
	writeFlow(t, w, "waits", "nodes: [{id: a, type: timer, wait: 1h}]\n")
	r := testRun(t, flows.Flow{Name: "top", Nodes: []flows.Node{{ID: "c"}}})

	res, err := w.callStep(context.Background(), r, flows.Node{Workflow: "two"}, "in")
	require.NoError(t, err)
	assert.Equal(t, "in\n\nin", res.Text)

	cases := []struct {
		name, workflow string
		depth          int
		want           string
	}{
		{name: "missing", workflow: "nowhere", want: `no workflow "nowhere"`},
		{name: "a problem", workflow: "broken", want: "can't run"},
		{name: "too deep", workflow: "two", depth: maxCallDepth, want: "deep"},
		{name: "a step fails", workflow: "fails", want: "fails stopped at Read"},
		{name: "a timer", workflow: "waits", want: "only in a workflow run itself"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r.depth = c.depth
			_, err := w.callStep(context.Background(), r, flows.Node{Workflow: c.workflow}, "in")
			assert.ErrorContains(t, err, c.want)
		})
	}
}

// A workflow with a problem, or steps in a loop, doesn't run.
func TestDriveFlowRefuses(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	_, err := w.runFlow(context.Background(), "s", flows.Flow{Name: "p", Problem: "it's broken"}, "", func(api.Event) {})
	assert.ErrorContains(t, err, "it's broken")
	loop := flows.Flow{Name: "loop", Nodes: []flows.Node{{ID: "a"}, {ID: "b"}}, Edges: []flows.Edge{{From: "a", To: "b"}, {From: "b", To: "a"}}}
	_, err = w.runFlow(context.Background(), "s", loop, "", func(api.Event) {})
	assert.Error(t, err)
}

// An extract field's value as its type, or why it isn't.
func TestFieldValue(t *testing.T) {
	cases := []struct {
		name  string
		typ   string
		in    any
		want  any
		error string
	}{
		{name: "nothing", typ: "number", in: nil, want: nil},
		{name: "a number", typ: "number", in: 2.5, want: 2.5},
		{name: "a number as text", typ: "number", in: " 3 ", want: 3.0},
		{name: "not a number", typ: "number", in: "many", error: "isn't a number"},
		{name: "a boolean", typ: "boolean", in: true, want: true},
		{name: "not a boolean", typ: "boolean", in: "yes", error: "isn't true or false"},
		{name: "a list", typ: "list", in: []any{"a", 1.0}, want: []any{"a", "1"}},
		{name: "not a list", typ: "list", in: "a", error: "isn't a list"},
		{name: "text", typ: "", in: "hi", want: "hi"},
		{name: "a number as text field", typ: "string", in: 4.0, want: "4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := fieldValue(flows.Field{Name: "f", Type: c.typ}, c.in)
			if c.error != "" {
				assert.ErrorContains(t, err, c.error)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, v)
		})
	}
}

// A condition in plain English, and an extract step, when the model's
// answer can't be used (or there's no model).
func TestFlowModelAnswers(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	conds := []struct{ name, reply, want string }{
		{name: "not JSON", reply: "maybe", want: "couldn't be judged"},
		{name: "no verdict", reply: `{"verdict": "perhaps"}`, want: "couldn't be judged"},
	}
	for _, c := range conds {
		t.Run(c.name, func(t *testing.T) {
			withJudgeReplies(w, c.reply)
			_, err := w.judgeCondition(context.Background(), "it's urgent", "out")
			assert.ErrorContains(t, err, c.want)
		})
	}
	withJudgeReplies(w, `{"verdict": "no"}`)
	ok, err := w.judgeCondition(context.Background(), "it's urgent", "out")
	require.NoError(t, err)
	assert.False(t, ok)

	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no model") }
	_, err = w.judgeCondition(context.Background(), "it's urgent", "out")
	assert.ErrorContains(t, err, "couldn't be judged")
	_, err = w.extractStep(context.Background(), flows.Node{Fields: []flows.Field{{Name: "n"}}}, "x")
	assert.Error(t, err)

	node := flows.Node{Instructions: "Amounts in dollars.", Fields: []flows.Field{{Name: "n", Type: "number", Description: "the amount"}}}
	judge := withJudgeReplies(w, "no fields here", `{"n": "lots"}`)
	_, err = w.extractStep(context.Background(), node, "x")
	assert.ErrorContains(t, err, "isn't the fields")
	_, err = w.extractStep(context.Background(), node, "x")
	assert.ErrorContains(t, err, "field n")
	prompt := judge.Requests[0].Contents[0].Parts[0].Text
	assert.Contains(t, prompt, "n (number): the amount")
	assert.Contains(t, prompt, "Also: Amounts in dollars.")
}

// withJudgeReplies has the rule model answer with replies, in turn.
func withJudgeReplies(w *Workspace, replies ...string) *runtime.MockLLM {
	var cs []*genai.Content
	for _, r := range replies {
		cs = append(cs, text(r))
	}
	judge := runtime.NewMockLLM("judge", cs...)
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	return judge
}

// A step's rules beyond its turn's gates: an unknown one is said, an
// instruction rule warns, a revise rule asks once more (and says when it
// still fails), and a rule that can't be judged is said.
func TestFlowStepRules(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools, text("First."), text("Second."), text("Third."))
	writeRule(t, w, "plain", "Be brief.\n")
	writeRule(t, w, "fix", "---\nwhen: after_agent\naction: revise\nagents: [editor]\n---\nOne word.\n")
	writeFlow(t, w, "rules", "nodes: [{id: a, type: agent, instructions: Answer., rules: [missing, plain, fix]}]\n")
	withJudge(t, w, "fail: too long", "fail: still long", "fail: not one word")
	s := newSession(t, w)
	var notices []string
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/rules", Command: true}, func(e api.Event) {
		if e.Notice != nil {
			notices = append(notices, e.Notice.Text)
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "Second.", res.Output, "revised once")
	joined := strings.Join(notices, "\n")
	assert.Contains(t, joined, "missing")
	assert.Contains(t, joined, "too long", "the instruction rule warned")
	assert.Contains(t, joined, "not one word", "still failing after the revision")

	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no model") }
	writeFlow(t, w, "unjudged", "nodes: [{id: a, type: agent, instructions: Answer., rules: [plain]}]\n")
	notices = nil
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/unjudged", Command: true}, func(e api.Event) {
		if e.Notice != nil {
			notices = append(notices, e.Notice.Text)
		}
	})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(notices, "\n"), "plain")
}

// Paused runs' records: only .json files that parse, one workspace's or
// every one's; a timer's bad time fails its step; a timer's answer and a
// form's are the step's result; a task that isn't the run's is refused.
func TestFlowRecordsAndAnswers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"run": "a", "workspace": "/w1"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.json"), []byte(`{"run": "b", "workspace": "/w2"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c.json"), []byte(`not json`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(`{}`), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "d.json"), 0o700))
	assert.Len(t, ListFlowRecords(dir, ""), 2)
	recs := ListFlowRecords(dir, "/w2")
	require.Len(t, recs, 1)
	assert.Equal(t, "b", recs[0].Run)

	times := []struct {
		name  string
		node  flows.Node
		error string
	}{
		{name: "until", node: flows.Node{Until: "2026-10-20T09:00:00Z"}},
		{name: "a bad until", node: flows.Node{Until: "Tuesday"}, error: "a time like"},
		{name: "a bad wait", node: flows.Node{Wait: "soon"}, error: "soon"},
	}
	for _, c := range times {
		t.Run(c.name, func(t *testing.T) {
			at, err := fireAt(c.node)
			if c.error != "" {
				assert.ErrorContains(t, err, c.error)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 2026, at.Year())
		})
	}
	timer := answerResult(flows.Node{Type: flows.Timer}, map[string]any{"payload": map[string]any{"fired_at": "9:00"}})
	assert.Equal(t, "9:00", timer.Text)
	form := answerResult(flows.Node{Type: flows.HumanTask}, map[string]any{"b": 2, "a": "x"})
	assert.Equal(t, "a: x\nb: 2", form.Text)

	w, _ := openTestWith(t, configtest.RunTools)
	writeFlow(t, w, "bad-timer", "nodes: [{id: t, type: timer, until: \"{{ input }}\"}]\n")
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/bad-timer Tuesday", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "a time like")

	writeFlow(t, w, "later", "nodes: [{id: wait, type: timer, wait: 1h}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/later", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	tasks := w.FlowTasks()
	require.Len(t, tasks, 1)
	_, err = w.ResumeFlow(context.Background(), tasks[0].Run+"/other", nil, func(api.Event) {})
	assert.ErrorIs(t, err, ErrNoTask)
	_, err = w.ResumeFlow(context.Background(), "nothing/here", nil, func(api.Event) {})
	assert.ErrorIs(t, err, ErrNoTask)
	assert.False(t, tasks[0].Started.After(time.Now()))
}

// A run that pauses twice keeps when it started; answered while another
// chat is open, it resumes in its own (WF-16). A template that fails as
// the step starts fails it; a timeout holds a step that finishes in time.
func TestFlowPausesTwice(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	writeFlow(t, w, "twice", `nodes:
  - {id: one, type: timer, wait: 1h}
  - {id: two, type: timer, wait: 2h}
  - {id: save, type: save_file, path: out/twice.md, timeout: 1m}
edges:
  - {from: one, to: two}
  - {from: two, to: save}
`)
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/twice", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	first := w.FlowTasks()
	require.Len(t, first, 1)
	newSession(t, w) // another chat is open now

	_, err = w.ResumeFlow(context.Background(), first[0].Tasks[0].ID, map[string]any{"fired_at": "1"}, func(api.Event) {})
	require.NoError(t, err)
	second := w.FlowTasks()
	require.Len(t, second, 1)
	require.Len(t, second[0].Tasks, 1)
	assert.Equal(t, "two", second[0].Tasks[0].Step)
	assert.True(t, second[0].Started.Equal(first[0].Started), "it keeps when it started")
	_, err = w.ResumeFlow(context.Background(), second[0].Tasks[0].ID, map[string]any{"fired_at": "2"}, func(api.Event) {})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(w.Dir(), "out", "twice.md"))

	writeFlow(t, w, "divide", "nodes: [{id: a, type: save_file, path: \"out/{{ 1 / int(input) }}.md\"}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/divide 0", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "division")
}
