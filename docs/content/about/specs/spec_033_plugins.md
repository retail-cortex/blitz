---
aliases: ["/about/specs/spec_plugins_033/"]
title: "033 · Plugins"
weight: 33
---

*Plugins, marketplaces and importers* (`spec_033_plugins`)

| | |
|---|---|
| Status | **Implemented** (2026-09-30). It details [spec_027_parity](spec_027_parity.md) §7.2 (PAR-PLG-01–04). |
| Source | `pkg/plugins` (`plugins.go`, `store.go`, `apply.go`, `marketplace.go`, `importer.go`); `apps/cli/plugin.go`; `pkg/engine/workspace.go` (`loadPlugins`), `commands.go`; `pkg/config/project.go` (`[plugins]`) |
| Tests | `pkg/plugins/plugins_test.go`, `importer_test.go`; `apps/cli/plugin_test.go`; `pkg/engine/plugins_test.go` |
| Depends on | [spec_013_skills](spec_013_skills.md), [spec_014_agents](spec_014_agents.md), [spec_010_hooks](spec_010_hooks.md), [spec_009_mcp](spec_009_mcp.md), [spec_031_project_config](spec_031_project_config.md) |

## 1. Purpose

A plugin bundles what extends Blitz (skills, commands, agents, hooks, MCP servers) so it can be shared, installed in one step, turned on and off, and updated. Because hooks and MCP servers run code, installing shows exactly what a plugin adds, and an installed plugin is pinned by the hash of its content.

## 2. A plugin

- **PLG-01** A directory with `plugin.toml` (`name`: lower-case letters, digits, `.`, `_`, `-`; `version`, default `0.0.0`; `description`, `author`, `homepage`) and any of: `skills/<name>/SKILL.md`, `commands/*.md`, `agents/*.md` (the formats Blitz reads elsewhere), `hooks.toml` (`[[pre_tool]]` and the other events, as `[hooks]`), `mcp.toml` (`[[servers]]`, as `[mcp]`; each needs a name and one of command or url). `${BLITZ_PLUGIN_ROOT}` in hooks' `command` and `args`, and MCP servers' `command`, `args` and `env`, is the plugin's directory when it loads.
- **PLG-43** *2026-11-05 (scan N26-pkg-small-2#11: `[[hooks.pre_tool]]` copied from config.toml into hooks.toml, or `[[mcp.servers]]` in mcp.toml, installed and never ran, without a word).* A key `hooks.toml` or `mcp.toml` doesn't have is an error naming it: the plugin doesn't install, and an installed one isn't loaded, with why.
- **PLG-02** `Summary` lists what it adds, what runs code first: each hook, each MCP server (a command runs code; a URL is named), each skill (its scripts run code when used), command and agent.

## 3. Installing

- **PLG-10** `blitz plugin install <src>`: a directory, a `.zip` (one top folder allowed; entries leaving the archive refused; links skipped; 64 MB unpacked), an http(s) URL of a zip (64 MB), a git repository (`git@…`, `…/x.git`, or a GitHub, GitLab or Codeberg repository URL; `#ref` picks a branch or tag; shallow clone, no prompts), or `name` / `name@marketplace` (§5). It prints the summary and asks (the question says when it runs code); without a terminal it refuses unless `--yes`.
- **PLG-44** *2026-11-05 (scan N26-pkg-small-2#5: over plain http a network attacker could swap a marketplace's index, with the hash it gives, and the archive together).* A plugin or marketplace URL needs https, as a skill registry does (SK-91): plain `http://` only to this machine (`localhost`, `127.0.0.1`, `::1`), `git://` never, and a download's redirect follows the same rule.
- **PLG-11** It's copied (without `.git` or links) to `~/.blitz/plugins/<name>/<version>`, its hash (SHA-256 over each file's path, size and content, in path order; 64 MB) recorded in `~/.blitz/plugins/installed.toml` with its source and time (owner-only). A plugin already installed is replaced, keeping whether it was enabled; another version's files go.
- **PLG-12** `list` (name, version, enabled or disabled, whether its files changed, source), `show <name>`, `enable`/`disable <name>` (every workspace), `remove <name>`, `update [name…]` (fetches from the source again; says when nothing changed, else shows the summary and asks, or `--yes`).

## 4. Loading

- **PLG-20** When a workspace opens, after its project settings: the store's enabled plugins, plus those `[plugins] enable` names (the user's settings, or a trusted project's: a tier B item needing trust), less those `[plugins] disable` names (the user's, or any project's: tier A), then `--plugin-dir` directories (repeatable; the run is local, not in the service).
- **PLG-21** An installed plugin whose files don't hash as recorded isn't loaded (a warning says to update it). An enabled name that isn't installed is a warning.
- **PLG-45** *2026-11-05 (scan N26-pkg-small-2#27: the hash skips links, so a link added after install changed what loaded while the hash matched).* An installed plugin holding a symbolic link isn't loaded either (installing copies none).
- **PLG-22** A plugin's skills directory joins `skills.paths` (the skill policy applies to its scripts), its agents are loaded like `~/.blitz/agents`, its commands come after the user's and before the project's (a project's command of the same name wins; `/help` shows the source `plugin <name>`), its hooks run after the user's and the project's (`/hooks` shows `plugin <name>`), and its MCP servers are added unless one of the same name exists (a warning).
- **PLG-48** *2026-11-05 (scan N26 policy-mode: an organization couldn't keep people's own plugins out, though they bring hooks and MCP servers that run code).* Under an organization whose policy says `[plugins] own = false` ([spec_053_enterprise](spec_053_enterprise.md) EN-35), or whose policy couldn't be read, none of the person's plugins loads (installed, from a marketplace, or `--plugin-dir`): no skills, agents, commands, hooks or MCP servers. The workspace's warnings say "your organization turns off your own plugins" once, when one would have loaded; `blitz plugin list` and `install` say so too. A policy that doesn't say allows them. The organization's catalog has no plugins of its own: what it provides (agents, skills, commands, MCP servers) loads as before.
- **PLG-23** `blitz doctor` lists installed plugins: version, disabled, or changed since installed.

## 5. Marketplaces

- **PLG-30** A marketplace is a `marketplace.toml`: `name`, and `[[plugins]]` with `name`, `version`, `description`, `source` (a git repository, an https zip, or a path relative to the marketplace) and `hash`.
- **PLG-31** `blitz plugin marketplace add <url|git repo|dir>` reads it and remembers it by its name (`~/.blitz/plugins/marketplaces.toml`); `list`, `remove <name>`.
- **PLG-32** `install name` looks it up in every marketplace (several offering it: say `name@marketplace`); the plugin fetched must have that name and the listed hash, else nothing is installed. An entry without a hash can't be installed. Updates fetch the index again.
- **PLG-46** *2026-11-05 (scan N26-pkg-small-2#22: one marketplace that couldn't be read stopped the lookup).* A marketplace that can't be read is skipped and the others looked in; with none offering the name, the error names those that couldn't be read. `name@m` with no marketplace `m` added says so.

## 6. Importers

- **PLG-40** `blitz plugin import claude <dir> [--out dir]` converts a Claude Code plugin: `.claude-plugin/plugin.json` (name made a valid plugin name), `commands/`, `skills/` (copied), `agents/` (frontmatter rewritten: `tools` mapped to Blitz's tool names, `model` and other keys dropped), `hooks/hooks.json` (events mapped, `PreToolUse` to `pre_tool` and so on; matchers such as `Edit|Write` become one hook per Blitz tool; `mcp__server__tool` matchers become globs; only `command` hooks), `.mcp.json` (stdio and HTTP servers). `${CLAUDE_PLUGIN_ROOT}` becomes `${BLITZ_PLUGIN_ROOT}`.
- **PLG-47** *2026-11-05 (scan N26-pkg-small-2#3, #6: a scoped tool such as `Bash(git tag:*)` became the whole shell, unreported, and `PostCompact` hooks were dropped).* An agent's scoped tool isn't given (a Blitz tool can't be limited to some uses); the report names it, to add with an allow or deny rule. `PostCompact` maps to `post_compact`. The hook events are listed once, `HooksConfig.Events`.
- **PLG-41** `import gemini <dir>` converts a Gemini CLI extension: `gemini-extension.json` (MCP servers: command, args, env, `httpUrl`, timeout; `${extensionPath}` becomes `${BLITZ_PLUGIN_ROOT}`) and `commands/**/*.toml` (`a/b.toml` becomes `/a-b`; `{{args}}` becomes `$ARGUMENTS`).
- **PLG-42** Each prints what it added and what it didn't convert, and why: unknown tools and events, SSE servers, prompt hooks, `cwd`, context files (plugins don't add instructions), `excludeTools`, output styles, Gemini's `!{…}` and `@{…}` (kept as text). The result must load as a plugin; the output directory must be new or empty.

## 7. Known limitations

- No output styles in plugins yet (they come with [spec_027_parity](spec_027_parity.md) §3.3), and no dependencies between plugins.
- The desktop app has no plugin view: plugins are managed from the shell.
