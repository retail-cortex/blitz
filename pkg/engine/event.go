package engine

import (
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	adksession "google.golang.org/adk/v2/session"
)

// events converts an ADK event: one Event per text part, tool call or tool
// result, in order.
func events(ev *adksession.Event) []api.Event {
	if ev == nil || ev.Content == nil {
		return nil
	}
	var out []api.Event
	for _, p := range ev.Content.Parts {
		switch {
		case p.Text != "":
			out = append(out, api.Event{Author: ev.Author, Text: &api.Text{Text: p.Text, Partial: ev.Partial, Thought: p.Thought}})
		case p.FunctionCall != nil:
			out = append(out, api.Event{Author: ev.Author, ToolCall: &api.ToolCall{ID: p.FunctionCall.ID, Name: p.FunctionCall.Name, Args: p.FunctionCall.Args, Partial: ev.Partial}})
		case p.FunctionResponse != nil:
			out = append(out, api.Event{Author: ev.Author, ToolResult: &api.ToolResult{ID: p.FunctionResponse.ID, Name: p.FunctionResponse.Name, Result: p.FunctionResponse.Response}})
			if p.FunctionResponse.Name == "todo" {
				if items := tools.TodoItems(p.FunctionResponse.Response); items != nil {
					tasks := make([]api.Task, len(items))
					for i, it := range items {
						tasks[i] = api.Task{Content: it.Content, Status: it.Status}
					}
					out = append(out, api.Event{Author: ev.Author, Tasks: tasks})
				}
			}
		}
	}
	return out
}
