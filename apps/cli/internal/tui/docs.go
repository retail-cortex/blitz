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
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/userdocs"
)

// docSections are the docs' parts, in the order `blitz docs` lists them.
var docSections = []string{"getting-started", "tutorials", "guide", "products", "releases"}

// RenderMarkdown is md for the terminal: rendered when pretty, else as it
// is.
func RenderMarkdown(md, theme string, width int, pretty bool) string {
	if !pretty {
		return md
	}
	var b strings.Builder
	m, err := newMarkdownStream(&b, theme, width)
	if err != nil {
		return md
	}
	m.Write(md)
	m.Flush()
	return b.String()
}

var (
	docLink   = regexp.MustCompile(`\]\((\.\./|\./)?([a-z0-9_/-]+?)(?:/_index)?\.md(#[^)]*)?\)`)
	shortcode = regexp.MustCompile(`(?m)^\s*\{\{[<%].*?[%>]\}\}\s*$\n?|\{\{[<%].*?[%>]\}\}`)
)

// forTerminal is a page's Markdown made to read outside the site: its
// links to other pages point at the site, and the site's shortcodes go.
func forTerminal(p *userdocs.Page) string {
	dir := p.Path[:strings.LastIndex(p.Path, "/")+1]
	md := docLink.ReplaceAllStringFunc(p.Body, func(m string) string {
		sub := docLink.FindStringSubmatch(m)
		target := sub[2] // ../guide/safety.md: from a section's page, another section's
		if sub[1] != "../" {
			target = dir + target
		}
		return "](" + userdocs.Site + strings.TrimSuffix(target, "/_index") + "/" + sub[3] + ")"
	})
	return shortcode.ReplaceAllString(md, "")
}

// ShowDocs is `blitz docs` and /docs (spec_067_cli_parity CLP-21): with
// nothing, the docs' pages by section; with a page's name, the page; else
// the sections that best match the words.
func ShowDocs(out io.Writer, cmd, words string, render func(string) string) {
	ix := userdocs.Docs()
	words = strings.TrimSpace(words)
	if words == "" {
		fmt.Fprintf(out, "\n%s%s%s\n", Bold, i18n.T("docs.title"), Reset)
		for _, sec := range docSections {
			var names []string
			for _, p := range ix.Pages {
				if strings.HasPrefix(p.Path, sec+"/") && !strings.HasSuffix(p.Path, "_index") {
					names = append(names, strings.TrimPrefix(p.Path, sec+"/"))
				}
			}
			if len(names) == 0 {
				if _, ok := ix.Find(sec + "/_index"); !ok {
					continue
				}
				names = []string{sec}
			}
			if sec == "releases" && len(names) > 5 { // the latest few
				slices.Reverse(names)
				names = names[:5]
			}
			fmt.Fprintf(out, "\n  %s%s%s  %s\n", Bold, sec, Reset, strings.Join(names, ", "))
		}
		fmt.Fprintf(out, "\n  %s%s%s\n\n", Dim, i18n.T("docs.usage", "cmd", cmd, "site", userdocs.Site), Reset)
		return
	}
	if p, ok := ix.Find(words); ok {
		fmt.Fprintln(out)
		_ = Page(out, render("# "+p.Title+"\n\n"+forTerminal(p))+"\n"+Dim+p.URL()+Reset+"\n\n")
		return
	}
	hits := ix.Search(words, 6)
	if len(hits) == 0 {
		fmt.Fprintf(out, "%s%s%s\n", Yellow, i18n.T("docs.none", "words", safe(words), "site", userdocs.Site), Reset)
		return
	}
	fmt.Fprintln(out)
	printHits(out, hits)
	fmt.Fprintf(out, "  %s%s%s\n\n", Dim, i18n.T("docs.open", "cmd", cmd, "page", hits[0].Page.Path), Reset)
}

// printHits lists sections found: page › heading, address, and its first
// lines.
func printHits(out io.Writer, hits []userdocs.Hit) {
	for _, h := range hits {
		title := h.Page.Title
		if h.Section.Heading != "" {
			title += " › " + h.Section.Heading
		}
		fmt.Fprintf(out, "  %s%s%s  %s%s%s\n", Bold, safe(title), Reset, Dim, h.URL(), Reset)
		if s := h.Snippet(180); s != "" {
			fmt.Fprintf(out, "    %s\n", safe(s))
		}
	}
	fmt.Fprintln(out)
}

// matchingCommands are the REPL's commands a question names: a word of it
// in a command's name counts 3, in its description 1; those scoring 2 or
// more, best first, at most 5.
func matchingCommands(question string) []*Command {
	var q []string
	for _, w := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r == '_') }) {
		if len(w) > 2 && !askStop[w] {
			q = append(q, strings.TrimSuffix(w, "s"))
		}
	}
	type scored struct {
		c     *Command
		score int
	}
	var found []scored
	for _, c := range Commands {
		names := strings.Join(c.names(), " ")
		help := strings.ToLower(i18n.T(c.Help))
		score := 0
		for _, w := range q {
			switch {
			case strings.Contains(names, w):
				score += 3
			case slices.ContainsFunc(strings.Fields(help), func(h string) bool { return strings.HasPrefix(h, w) }):
				score++
			}
		}
		if score >= 2 {
			found = append(found, scored{c, score})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return b.score - a.score })
	var out []*Command
	for i, f := range found {
		if i == 5 {
			break
		}
		out = append(out, f.c)
	}
	return out
}

// askStop are words of a question that name no command.
var askStop = map[string]bool{"how": true, "the": true, "and": true, "can": true, "what": true, "does": true, "with": true, "for": true, "this": true, "that": true, "use": true, "want": true, "blitz": true}

// AnswerOffline answers a question without a model (CLP-23): the docs'
// sections and the commands that match it, and how to ask in your own
// words.
func AnswerOffline(out io.Writer, question string, why string) {
	hits := userdocs.Docs().Search(question, 5)
	cmds := matchingCommands(question)
	fmt.Fprintln(out)
	if why != "" {
		fmt.Fprintf(out, "  %s%s%s\n\n", Dim, why, Reset)
	}
	if len(hits) == 0 && len(cmds) == 0 {
		fmt.Fprintf(out, "%s%s%s\n\n", Yellow, i18n.T("ask.nothing", "site", userdocs.Site), Reset)
		return
	}
	if len(hits) > 0 {
		fmt.Fprintf(out, "%s%s%s\n", Bold, i18n.T("ask.from_docs"), Reset)
		printHits(out, hits)
	}
	if len(cmds) > 0 {
		fmt.Fprintf(out, "%s%s%s\n", Bold, i18n.T("ask.commands"), Reset)
		for _, c := range cmds {
			usage := "/" + c.Name
			if c.Usage != "" {
				usage += " " + c.Usage
			}
			fmt.Fprintf(out, "  %s%s%s  %s\n", Bold, usage, Reset, i18n.T(c.Help))
		}
		fmt.Fprintln(out)
	}
}

// HelpPrompt is what's sent to the model to answer a question about Blitz
// (CLP-22): the docs' sections that match it, then the question. It's an
// aside: recorded nowhere, its tools read-only.
func HelpPrompt(question string) string {
	excerpt := userdocs.Excerpt(userdocs.Docs().Search(question, 6), 14_000)
	if excerpt == "" {
		excerpt = "(No page of the docs matches the question.)"
	}
	return "The person is asking how to do something in Blitz, the app you're running in. " +
		"Answer from Blitz's documentation below: say what to do, with the exact command or setting to type (in the terminal, " +
		"a /command in this session or `blitz …` in a shell), in a few short steps. Link the page you used (its address is under its " +
		"heading). If the pages don't cover it, say so plainly and point to " + userdocs.Site + "; don't invent commands or settings. " +
		"Don't change anything: this is a question, not a request to act.\n\n" +
		"<blitz-docs>\n" + excerpt + "\n</blitz-docs>\n\nThe question: " + question
}

// isHelpQuestion reports whether /help's words are a question rather than
// a command: more than one word, or one that names no command and is near
// none (a slip is a "did you mean").
func isHelpQuestion(app *App, words string) bool {
	f := strings.Fields(words)
	switch {
	case len(f) == 0:
		return false
	case len(f) > 1:
		name := strings.TrimPrefix(strings.ToLower(f[0]), "/")
		return FindCommand(name) == nil && !isCustomCommand(app, name) || strings.HasSuffix(words, "?")
	}
	name := strings.TrimPrefix(strings.ToLower(f[0]), "/")
	return name != "keys" && FindCommand(name) == nil && !isCustomCommand(app, name) && nearest(Commands, name) == ""
}

// askBlitz answers a question about Blitz (? …, /help …): the model, from
// the docs, as an aside; without a model, the docs and commands that match.
func askBlitz(ctx context.Context, app *App, question string, interrupts <-chan os.Signal) {
	question = strings.TrimSpace(question)
	if question == "" {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("ask.usage"), Reset)
		return
	}
	active, ok := app.Workspace.ActiveSession()
	if err := app.Workspace.ModelErr(); err != nil || !ok {
		AnswerOffline(os.Stdout, question, i18n.T("ask.no_model"))
		return
	}
	fmt.Printf("%s%s%s\n", Dim, i18n.T("ask.asking"), Reset)
	runTurn(ctx, app, active.ID, "? "+question, interrupts, turnOptions{aside: true, prompt: HelpPrompt(question), onFail: func(err error) {
		AnswerOffline(os.Stdout, question, i18n.T("ask.model_failed", "error", safe(FirstLine(err.Error()))))
	}})
}

// FirstLine is s up to its first line break, at most 200 runes.
func FirstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > 200 {
		return string(r[:199]) + "…"
	}
	return s
}
