---
title: Safety
weight: 20
---

Blitz's guardrails are layered: approvals and permission rules decide what may run, the file sandbox and the OS sandbox limit what it can reach when it does, and checkpoints and the audit log let you see and undo what happened. [Sandboxing and trust](../architecture/security.md) explains the design. Specs: [approvals](../about/specs/spec_approvals_005.md), [file tools](../about/specs/spec_filetools_006.md), [shell](../about/specs/spec_shell_007.md).

## Approvals

File edits show a coloured diff before you approve them. Answer `y` for once, `s` for the rest of the session, `a` for always (saved to `~/.blitz/approvals.json`), or `n` for no. Commands are remembered by their exact text within a workspace, edits per workspace, web requests per host, and MCP tools per server and tool. With no one to ask, sensitive actions are denied unless allowed in the settings.

## Plans and task lists

In plan mode, with `/plan`, or when the agent chooses to plan first, it presents a plan for approval: approving it carries it out, and typing feedback makes it revise. `[blitz] plan_review` sets when plans are required: `always`, `agent-decides` (the default) or `never`. For work with several steps, the agent keeps a task list, shown as a checklist that updates as it goes.

## Permission modes

| Mode | What runs without asking |
|---|---|
| `default` | Reads; everything else asks |
| `accept-edits` | File changes in the workspace too; commands still ask |
| `plan` | Every prompt is planned; tools that change anything are refused |
| `dont-ask` | Anything that would ask is refused instead (for CI and scripts) |
| `bypass` | Everything; only while the OS sandbox is active, and deny rules, blocked paths and the sandboxes still apply |

Choose one with `--permission-mode`, `[blitz] permission_mode`, or `/mode` in a session. Workers ignore the mode: they get exactly their own permissions.

## Permission rules

Rules say what runs without asking (`allow`), always asks (`ask`), or never runs (`deny`), in every mode. Deny wins over ask, and ask over allow:

```toml
[permissions]
allow = ["shell(go test *)", "shell(git status)", "write(docs/**)", "web(*.go.dev)"]
ask   = ["shell(git push *)", "write(.github/**)"]
deny  = ["shell(rm -rf *)", "read(secrets/**)", "mcp(github:delete_*)", "web_search"]
```

A shell rule names a command with any arguments: `shell(ls)` covers `ls -la`, `shell(git log)` covers `git log --oneline` (but not `git logs`). With `*` or `?` it's a glob over the whole command (`shell(go test *)`, `shell(git * --force*)`), and `re:` starts a regular expression that must match the whole command (`shell(re:git (log|show)( .*)?)`). A command that writes a file through a redirection (`ls > out.txt`) always asks, whatever allows the command.

Rules live in the global settings, and a workspace's own add to them: in the desktop app, **Settings › Permissions** for the global rules and the run settings panel's **Permission rules** for the workspace's, where a rule is checked as you type it and can be tried on a command before it's saved. From the command line:

```bash
blitz config permissions                                   # the rules, and the built-in ones
blitz config permissions allow 'shell(make)' --workspace   # this workspace only
blitz config permissions deny 'shell(git push --force)'
blitz config permissions check 'shell(git log)' 'git log --oneline | head'
blitz config permissions remove 'shell(make)' --workspace
```

**Built-in read-only rules.** Unless `[permissions] read_only_defaults = false` (or `blitz config permissions defaults off`), these run without asking: `ls`, `pwd`, `cat`, `head`, `tail`, `wc`, `stat`, `du`, `df`, `which`, `grep`, `rg`, `diff`, `git status`, `git log`, `git show`, `git diff`, `git blame` and `git rev-parse`. They still ask with `git … --output` or `--ext-diff` and `rg … --pre` (which write a file or run a program), and with a redirection to a file. A deny or ask rule of your own overrides any of them; a workspace can turn them on or off for itself.

Kinds: `shell(…)`, checked on every sub-command, through pipes, `bash -c` and wrappers; `write(…)`, `delete(…)` and `read(…)`, path globs relative to the workspace (`read` rules only deny); `web(host)`, `search(provider)`, `mcp(server:tool)`, `skill(name)`, `agent(name)`, or a bare tool name. Claude Code's spellings (`Bash(…)`, `Edit(…)`) work too. `/permissions` lists and changes the rules for the session (`--save` writes them to the global settings, `--workspace` to the workspace's), and `--allow` and `--deny` add them for one run. Every rule is checked before it's saved, in the settings file too.

## File sandbox

File tools reach only the workspace, plus `sandbox.allowed_paths` (read-write) and `sandbox.read_only_paths`, enforced with Go's `os.Root`: no `..` or symlink escapes. `sandbox.blocked_paths` (by default `.env` files, keys, `~/.ssh`, cloud credentials and more) are never readable or writable, including through symlinks, `grep` and `list_files`.

## Command policy

Every shell command is parsed and each sub-command checked: pipes, `$(…)`, `bash -c`, `find -exec`, and wrappers such as `env`, `xargs` and `timeout`. Disguises like `s\udo` or `{r,}m` are caught. `sandbox.commands.deny` always wins; if `sandbox.commands.allow` is set, only matching commands run. This is a guardrail; the OS sandbox is the boundary.

## OS sandbox

`sandbox.shell` is `auto`, `required` or `off`. Shell commands, forged tools and stdio MCP servers run under Seatbelt on macOS or bubblewrap on Linux: writes go only to the writable roots, temporary and cache directories and `shell_writable_paths`; blocked paths are unreadable; the network is off when `allow_network = false`. `required` refuses to start without the sandbox.

On Linux, install `bubblewrap` and allow unprivileged user namespaces. Ubuntu 24.04 restricts them through AppArmor, and Docker's default seccomp profile blocks them; in `auto` mode Blitz then runs commands unsandboxed, and `/sandbox` or `blitz doctor` says why. bubblewrap can only hide paths that exist when a command starts, so a blocked file that a command creates is visible to that same command; macOS blocks it at once.

## Background processes

Background processes never outlive Blitz. Exiting with processes running asks whether to kill them or wait; a second Ctrl+C force-quits. Each process group is guarded so it's killed if Blitz dies, even by `SIGKILL`.

## Secrets

Child processes don't inherit credential variables (`sandbox.scrub_env`: `*_API_KEY`, `*_SECRET` and more). The audit log, the diagnostic log and telemetry mask secrets. Sessions, history, approvals and audit files are owner-only.

## Checkpoints and undo

Before a file tool changes a file, Blitz keeps a copy, grouped by prompt, so `/undo` can put it back. Checkpoints are kept between runs in `~/.blitz/checkpoints/<workspace>/` (owner-only, stored by content hash), up to `[checkpoints] max_bytes` (64 MiB) and `max_age_days` (30). `/rewind` goes back to before any earlier prompt: its files, the conversation, or both. Only the file tools' changes are tracked: if a file changed since, by a command or by you, `/undo` stops and says so (`--force` overwrites).

## Audit log

`~/.blitz/audit/audit-YYYY-MM-DD.jsonl` records prompts, tool calls and results, approvals, denials, hook decisions and undos, plus your own `!` commands and `/search web` queries.

## Web

`web_fetch` only reaches public addresses, checked after DNS resolution and on every redirect: no `localhost`, private ranges or cloud metadata. It needs approval per host unless the host is in `web.allow_domains`, and caps the response size. The agent's `web_search` asks before each query leaves your machine.
