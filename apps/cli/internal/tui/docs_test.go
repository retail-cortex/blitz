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
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/userdocs"
)

func plainRender(md string) string { return md }

// /docs lists the pages, shows one by name, or the sections that match
// (CLP-21).
func TestShowDocs(t *testing.T) {
	show := func(words string) string {
		var b bytes.Buffer
		ShowDocs(&b, "/docs", words, plainRender)
		return stripANSI(b.String())
	}
	list := show("")
	for _, want := range []string{"Blitz docs", "getting-started", "guide", "mcp", "releases", "/docs <page>", userdocs.Site} {
		assert.Contains(t, list, want)
	}
	page := show("mcp")
	assert.Contains(t, page, "# MCP servers")
	assert.Contains(t, page, userdocs.Site+"guide/mcp/")
	assert.NotContains(t, page, "{{<", "no shortcodes")
	assert.NotContains(t, page, "](../", "links point at the site")
	found := show("add an mcp server that needs a token")
	assert.Contains(t, found, "MCP servers ›")
	assert.Contains(t, found, "/docs guide/mcp reads the first in full")
	assert.Contains(t, show("xylophonics"), "Nothing in the docs matches")
}

func TestForTerminal(t *testing.T) {
	p := &userdocs.Page{Path: "guide/mcp", Body: "See [safety](safety.md), [the CLI](../products/cli.md#flags) and [home](../_index.md).\n\n{{< release >}}\n\nThe end {{< hint >}}."}
	got := forTerminal(p)
	assert.Contains(t, got, "[safety]("+userdocs.Site+"guide/safety/)")
	assert.Contains(t, got, "[the CLI]("+userdocs.Site+"products/cli/#flags)")
	assert.NotContains(t, got, "{{<")
	assert.Contains(t, got, "The end .")
}

// Without a model, a question gets the docs' sections and the commands
// that match, and how to ask in your own words (CLP-23).
func TestAnswerOffline(t *testing.T) {
	var b bytes.Buffer
	AnswerOffline(&b, "how do I change the permission mode?", "no model")
	out := stripANSI(b.String())
	assert.Contains(t, out, "no model")
	assert.Contains(t, out, "From the docs")
	assert.Contains(t, out, "Commands")
	assert.Contains(t, out, "/mode")
	assert.NotContains(t, out, "/btw", "a word in a description alone isn't enough")
	b.Reset()
	AnswerOffline(&b, "zzqx", "")
	assert.Contains(t, stripANSI(b.String()), "Nothing in the docs or commands matches")
}

func TestHelpPrompt(t *testing.T) {
	p := HelpPrompt("how do I add an MCP server?")
	assert.Contains(t, p, "<blitz-docs>")
	assert.Contains(t, p, userdocs.Site+"guide/mcp/")
	assert.True(t, strings.HasSuffix(p, "The question: how do I add an MCP server?"))
	assert.Contains(t, p, "don't invent commands")
	assert.Contains(t, HelpPrompt("zzqx"), "No page of the docs matches")
}

func TestIsHelpQuestion(t *testing.T) {
	app := newTestApp(t, nil)
	cases := map[string]bool{
		"":                           false,
		"mode":                       false,
		"/mode":                      false,
		"keys":                       false,
		"modle":                      false, // a slip: did you mean /model?
		"permissions allow":          false,
		"how do I add an MCP server": true,
		"mode?":                      false,
		"mode of transport?":         true,
		"tokens":                     true,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) { assert.Equal(t, want, isHelpQuestion(app, in)) })
	}
}

// ? <question> asks the model from the docs, as an aside; without a model
// it answers from the docs (CLP-22, CLP-23).
func TestAskBlitz(t *testing.T) {
	ctx := context.Background()
	t.Run("with a model", func(t *testing.T) {
		app, s := stubApp(t, "")
		turns := recordTurns(s)
		s.modelErr = func() error { return nil }
		_, err := app.Workspace.NewSession()
		require.NoError(t, err)
		out := stripANSI(captureStdout(t, func() { askBlitz(ctx, app, "how do I add an MCP server?", make(chan os.Signal)) }))
		assert.Contains(t, out, "Asking Blitz")
		require.Len(t, *turns, 1)
		turn := (*turns)[0]
		assert.True(t, turn.Aside, "recorded nowhere, read-only")
		assert.Equal(t, "? how do I add an MCP server?", turn.Text)
		assert.Contains(t, turn.Prompt, "<blitz-docs>")
	})
	t.Run("without one", func(t *testing.T) {
		app, s := stubApp(t, "")
		turns := recordTurns(s)
		s.modelErr = func() error { return errors.New("no key") }
		out := stripANSI(captureStdout(t, func() { askBlitz(ctx, app, "how do I add an MCP server?", make(chan os.Signal)) }))
		assert.Empty(t, *turns)
		assert.Contains(t, out, "No model is set up")
		assert.Contains(t, out, "MCP servers")
	})
	t.Run("the model fails", func(t *testing.T) {
		app, s := stubApp(t, "")
		s.modelErr = func() error { return nil }
		_, err := app.Workspace.NewSession()
		require.NoError(t, err)
		s.run = func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error) {
			return api.TurnResult{}, errors.New("Error 404: model not found\nmore")
		}
		out := stripANSI(captureStdout(t, func() { askBlitz(ctx, app, "how do I add an MCP server?", make(chan os.Signal)) }))
		assert.Contains(t, out, "The model couldn't answer (Error 404: model not found)")
		assert.Contains(t, out, "MCP servers", "the docs answer instead")
		assert.NotContains(t, out, "more")
	})
	t.Run("no question", func(t *testing.T) {
		app, _ := stubApp(t, "")
		assert.Contains(t, stripANSI(captureStdout(t, func() { askBlitz(ctx, app, " ", make(chan os.Signal)) })), "? <question>")
	})
}

// In the REPL, "? …" and "/help <question>" ask; "/help <command>" is the
// command's help.
func TestTheREPLAsks(t *testing.T) {
	app, s := stubApp(t, "")
	turns := recordTurns(s)
	s.modelErr = func() error { return nil }
	app.Input = &scriptInput{steps: lines("? what is unsafe mode", "/help how do I resume a chat", "/help mode")}
	out := stripANSI(captureStdout(t, func() { require.NoError(t, RunREPL(context.Background(), app)) }))
	require.Len(t, *turns, 2)
	assert.Equal(t, "? what is unsafe mode", (*turns)[0].Text)
	assert.Equal(t, "? how do I resume a chat", (*turns)[1].Text)
	assert.Contains(t, out, "/mode [auto|default|", "/help mode is the command's help")
}

// On a terminal, a page is rendered: its heading styled, not "# ".
func TestRenderMarkdown(t *testing.T) {
	md := "# MCP servers\n\nAdd one with `blitz mcp add`.\n"
	assert.Equal(t, md, RenderMarkdown(md, "dark", 80, false), "plain without a terminal")
	got := RenderMarkdown(md, "dark", 80, true)
	assert.NotContains(t, got, "# MCP servers")
	assert.Contains(t, stripANSI(got), "MCP servers")
	assert.Contains(t, stripANSI(got), "blitz mcp add")
}
