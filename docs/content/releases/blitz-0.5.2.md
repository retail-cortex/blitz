---
title: Blitz 0.5.2
weight: 7
---

**Go code in your docs, steady as you hover.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.5.2).

**v0.5.2 is a quick fix for anyone with Go code in their Markdown.** Hovering over a Go code block in the visual editor could close the editor and make it look as if Blitz had crashed. Now hovering just shows you the help you were after. And Go blocks now get that help in many more projects.

**See it in a minute:**

1. Open a Markdown file with a Go code block, in a project whose Go module lives in a folder (say `backend/go.mod`) rather than at the top.
2. Switch to **Visual** and hover over a name in the block, such as `fmt.Println` or `net.Listener`: its documentation appears.
3. Type in the block: completion and real errors show up as you go.

### What's fixed

- **Hovering never closes the editor.** When a language server can't answer, Blitz now treats it as that server's hiccup, not as Blitz itself going away. The editor stays open and your place in the document stays put.
- **Go blocks work wherever your module lives.** Before, Go blocks only got hover, completion and error checking when the project had a `go.mod` at its top. Now monorepos and projects with their Go code in a subfolder get them too.
- **No false alarms.** A Go block no longer shows a "No packages found" warning just for being a block. Real errors still show.

### Good to know

- Nothing is written to your project: Blitz keeps an empty scratch folder in your system's temp directory for Go to look at, and removes it when the language servers stop.
- In a project without a top-level module, a Go block sees the standard library but not the project's own packages or its dependencies, since no module ties them together there.
- Tested on macOS. Linux should work the same; let us know if anything looks off.
