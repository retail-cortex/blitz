---
title: Blitz 0.6.3
weight: 10
---

**Start a project with one command, pull and push from the Changes page, and keep going when the network hiccups.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.6.3).

**`/init` now asks about your project first and sets up the workspace in the same go.** Before, `/init` told you to run `/spec`, then come back and run `/init` again. Now it asks the specification's questions right away, writes the specs, and then fits the agents, skills and instructions to them, all in one conversation.

**And a dropped connection no longer freezes the chat.** If the network drops the connection to the model, as a VPN, a sleeping router or a Wi-Fi change can, Blitz notices within a minute, tells you it's reconnecting, and carries on. Before, the chat could sit at "Working…" until you pressed Stop.

**Pull, push and settle merge conflicts without leaving Blitz.** With a remote, the Changes page shows how many commits you're behind and ahead, with **Pull** and **Push** right there. When a pull stops on conflicts, Blitz shows each conflicted file and lets you keep your side, take theirs, edit it yourself, or ask the agent to resolve them. Then complete the merge, or back out.

**Who should update:** everyone. Starting a new project is much smoother, git's everyday sync is a click away, anyone on a VPN or a patchy connection gets their turns back, and on the Mac, Blitz stops asking for your Music and Pictures folders.

**See it in a minute:**

1. Make an empty folder and open it in the desktop app, or run `blitz` in it.
2. Type `/init`. Answer the questions about what the project is for.
3. Watch it write the specification in `.agents/specs/`, then carry on to set up the workspace for it, without being asked twice.

### What's new

- **Pull and Push on the Changes page.** On the Git view, Pull and Push show how many commits each would bring or send, checked against the remote when the page opens. The status bar shows ↓ and ↑ beside the branch. Pull follows your git config (merge or rebase) and keeps your uncommitted work. A branch that isn't on the remote yet gets **Publish branch**.
- **Rewrite with AI.** In a worker's form, **Rewrite with AI** turns rough instructions into ones that run well unattended: named sources, the exact file and format to write, what to do when something fails, and the narrowest permissions they need. You see the suggestion and the permission changes before anything changes in the form.
- **Stop a worker that's running.** The Workers menu now lists every worker running right now, in any workspace, under **Running now**, each with a **Stop** button. A worker's run list has Stop too, and so does `blitz workers stop <name>`. The run ends as "stopped", and any files it changed can still be undone.
- **Merge conflicts, settled in place.** A pull that stops on conflicts marks each file. Open it in the editor, keep yours, take theirs, or mark it resolved once you've edited it. Or click **Ask the agent to resolve**, review its work, and press **Complete merge**. **Abort** puts everything back as it was before the pull.

- **`/init` leads with the specification.** In a workspace without specs, `/setup` (and `/init`) asks the specification's questions itself, writes the specs, and then sets up the workspace from them. Running `/spec` again later still revises the specs in place.
- **Reconnecting, said out loud.** When the connection to the model fails and Blitz retries, a message at the top of the window says so ("Lost the connection to the model… Reconnecting…"). It passes once the model answers, and isn't kept in the chat.
- **Versions read as they're tagged.** Settings › About, the status bar, the tray and `blitz status` show Blitz's version as `v0.6.3`, the way releases and the docs name it.

### What's fixed

- **No more frozen chats after a network error.** A connection that dies without a word used to hold each request for up to 10 minutes, and again for every retry. Now a quiet connection is checked every 30 seconds, and a dead one is replaced within a minute.
- **Answers you can read.** When the agent asks a question with long answers, each answer now wraps onto more lines instead of being cut off at the edge of the card.
- **Messages that match the rest of the window.** The message at the top of the window is now a rounded pane of glass, like the panes and menus, instead of a flat dark bar.
- **The docs name the latest release.** Install commands in the docs and the in-app guide show the newest version, filled in when the docs are built, rather than one that had to be updated by hand.
- **Workers may write where they're told.** A permission written as `write:./daily-news/*` matched nothing, so the worker's writes were refused. A leading `./` now works as you'd expect.
- **Fetching from Google News works.** Some sites, Google News among them, failed to fetch with "malformed HTTP response". They load now.
- **No more "Blitz would like to access your Music folder".** An agent searching the whole disk for a program (`find / -name …`) made macOS ask about each of your personal folders. Agents now find programs with `command -v`, searches of the whole disk or your home folder are refused, and on the Mac, commands can't reach into Desktop, Documents, Downloads, Music, Pictures, Movies, iCloud Drive, Mail or Photos. A project kept in one of those folders still works.

### Good to know

- Your settings and workspaces carry over: install the update over 0.6.x.
- Pull and Push never ask for a password: sign in once with git in a terminal, or use an SSH key or a credential helper. If your repository's own `.git/config` names an SSH command or credential helper, Blitz won't run it (the agent can write that file). Keep those in your global git config.
- macOS may still ask once more after this update, because each Blitz build is new to macOS. Commands that read files in your personal folders now need the folder added to `read_only_paths` in `[sandbox]`.
- `/init` in unattended runs (`blitz -p`, workers) doesn't stop to ask questions. Write the specs with `/spec` first there.
- While `/init` writes the specs, the chat shows the questions and approvals, but not each file being written. The specs are all there when it moves on to setting up.
- If every retry fails, the turn ends with the error as before, and you can send your message again. Retries follow `llm.max_retries` (default 3), and with retries off there's no "Reconnecting…" message.
