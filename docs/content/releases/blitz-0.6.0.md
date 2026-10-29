---
title: Blitz 0.6.0
weight: 14
---

**Your documents, everywhere they need to go.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.6.0).

**v0.6.0 sends your work out the door looking right.** Export any Markdown file as a Word document, with real Word equations, your pictures and your Mermaid diagrams drawn in. PDFs now keep every picture, even in documents full of maths. Skills install from registries such as your organization's Castor, checked before they land. And your project's history becomes a graph you can explore: every file's, every folder's, the whole workspace's, with branches, merges and the power to put things back.

**See it in a minute:**

1. Open a Markdown file with an equation, a picture and a diagram.
2. Choose **Export › Export as Word** in the file's bar, and open the `.docx` beside it in Word.
3. Choose **Changes › Workspace history…** to see your project's commits as a graph, then Shift-click two of them to compare.

### Word documents

- **Export › Export as Word** in a Markdown file's bar writes `<name>.docx` beside it, from what's in the editor.
- **Equations you can edit:** `$…$` and `$$…$$` become Word equations: fractions, roots, sums and integrals, matrices, cases and more. `$5 and $10` stay as text.
- **Pictures and diagrams:** pictures (SVG and HTML `<img>` tags too) are embedded, and Mermaid diagrams are drawn in.
- **Word's own styles:** headings, lists, quotes, code and tables use Word's styles, so the navigation pane, a table of contents and restyling all work.
- **Anywhere:** `/export notes.docx` writes a conversation, `blitz sessions export <id> -o notes.docx` does it from the shell, and the agent has `export_docx`.

### PDFs keep their pictures

- **Every picture prints**, including in documents with maths, which used to print pictures as blank space: printing now waits for the page's fonts and pictures.
- **Pictures are found as the preview finds them**, by `my%20figs/plot.png`, `/figs/plot.png` or an HTML `<img>`, in the app's PDFs, Word documents and the agent's `export_pdf`.

### Skills from registries

- **Add a registry** such as Castor with `blitz skills registry add <url>`, or in **Settings › Skills**. Its token is kept in your keychain.
- **Find and install skills** with `blitz skills search` and `blitz skills install castor/lit-review`, `/skills install` in a session, or **Install** in Settings, for you or for the project. Each skill is checked against its registry's hash first.
- **No restart needed:** a new skill is ready in the conversations already open. `blitz skills update` keeps skills current.

### History as a graph

- **See how your work got here:** **View revisions…** on a file or folder (or **Changes › Workspace history…** for everything) shows its commits as a graph: branches in their own colours, merges where they join, grouped by day, with who made each change and how big it was.
- **Compare anything:** see what a commit changed, how it differs from now, or Shift-click two commits to compare them.
- **Put things back:** restore a file, or a whole folder, as it was in any commit. A folder's restore shows you which files come back first, and keeps the files you've added since. Nothing is staged, so you review before you commit.
- **Follows renames,** and loads long histories 100 commits at a time.

### Files

- **View revisions** is on an editor tab's right-click menu too.
- **Remove from .gitignore** takes back what **Add to .gitignore** did, and names the rule when something else ignores the file.
- **Tabs:** right-click a tab to close it, the others, those to its right, the saved ones or all of them, with one question for any unsaved changes.
- **Tidier tree:** AGENTS.md, CLAUDE.md, GEMINI.md and BLITZ.md are hidden like dotfiles; **Show hidden files** shows them.
- **Messages up top:** the snackbar now appears at the top, out of the way of the chat box.

### Linux

- **NVIDIA on Wayland:** the desktop app no longer quits on startup with a Wayland protocol error on NVIDIA GPUs, and Export as PDF works there.

### Good to know

- **Diagrams in Word come from the desktop app,** which draws them. The agent's `export_docx` and `/export` keep a diagram as its code, with a note.
- **SVG pictures in Word** show in Word 2016 and later; older readers show a blank space.
