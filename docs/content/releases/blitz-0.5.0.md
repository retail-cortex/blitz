---
title: Blitz 0.5.0
weight: 4
---

**Write Markdown the way it looks, with code that understands your project.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.5.0).

**v0.5.0 turns Blitz's editor into a real workspace for docs and code.** Markdown files open as the finished document, ready to edit: no more switching between source and preview. Every code block inside them gets completion, error checking and documentation from your project's own language servers. And regular code files get the same help.

**Try it in a minute:**

1. Turn on **Settings › Show advanced settings**.
2. Open any Markdown file. It opens in **Visual**: headings, lists, tables and diagrams, all editable in place.
3. Add a code block from the toolbar (or type ```` ``` ````), pick **Go**, **Python** or **TypeScript**, and start typing. Completions come from your project, mistakes are underlined, and hovering a name shows its documentation.

Want the raw Markdown? **Source** is one click away, and both views stay in sync.

### Visual Markdown editing

- **Edit the document, not the syntax.** Bold, links, images, tables, task lists, quotes and Mermaid diagrams, from a toolbar at the bottom of the editor, a right-click menu, or `/` for a quick block menu. Typing Markdown still works: `# ` makes a heading, `- ` a list.
- **Your files stay yours.** Blitz rewrites only what you change. Everything else is saved byte for byte, so your git diffs show your edits and nothing more. Front matter, raw HTML and footnotes are kept as editable source.
- **Code blocks are real code editors**, with highlighting, a language picker (R included), and diagrams that redraw as you type.
- **Paste and copy Markdown**: pasted Markdown becomes formatted text, and copying gives you Markdown back.

### Code that knows your project

- **In files and in code blocks**: completion with signatures and the imports you need, problems underlined and counted in the status bar, documentation on hover, and **⌘-click** or **F12** to jump to a definition.
- **Code blocks see your workspace.** A Go block in your README can call your own packages, and a short snippet of statements just works, without a `package` line or a `main`.
- **Tab completes.** After a word or a `.`, Tab brings up completions; at the start of a line it indents, as always.
- **Shared with the agent.** The editor uses the same language servers the agent already runs, so nothing extra starts in the background. **Settings › Workspaces › Language servers** shows how each one is doing, with **Restart**.

Supported out of the box: **Go** (gopls), **TypeScript and JavaScript** (typescript-language-server) and **Python** (pyright). Have them installed and Blitz finds them. If one is missing, the status bar tells you how to install it.

### Also in this release

- **Your tools are found when Blitz starts at login.** The background service now uses your terminal's PATH, so language servers and the tools the agent runs are found even when you installed them with nvm, Homebrew or `go install`.
- **Google sign-in is simpler.** Gemini uses one credential, in this order: your API key, then Google Cloud's application credentials, then your Google sign-in. Each request carries only that one.
- **Security updates**: built with Go 1.27.2 and the latest networking libraries, closing eleven published advisories.
- HTML tags get proper syntax colours, Markdown source highlights its code blocks, and tables have clearer borders.

### Good to know

- Visual editing is an **advanced** feature. With advanced settings off, Markdown files still open in **Preview** and **Source**, as before.
- Language servers in code blocks cover **Go, TypeScript, JavaScript and Python**. Rust blocks get highlighting and word completion, since rust-analyzer needs a full Cargo project.
- Language servers start only in workspaces whose project settings you trust.
- Windows support for language servers is new and lightly tested so far.

Try it and tell us what you think. Issues and ideas are very welcome. 
