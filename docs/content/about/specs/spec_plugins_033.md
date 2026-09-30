---
title: "033 · Plugins"
weight: 33
---

*Plugins, marketplaces and importers* (`spec_plugins_033`)

| | |
|---|---|
| Status | **Implemented** (2026-09-30). It details [spec_parity_027](spec_parity_027.md) §7.2 (PAR-PLG-01–04). |
| Source | `pkg/plugins` (`plugins.go`, `store.go`, `apply.go`, `marketplace.go`, `importer.go`); `apps/cli/plugin.go`; `pkg/engine/workspace.go` (`loadPlugins`), `commands.go`; `pkg/config/project.go` (`[plugins]`) |
| Tests | `pkg/plugins/plugins_test.go`, `importer_test.go`; `apps/cli/plugin_test.go`; `pkg/engine/plugins_test.go` |
| Depends on | [spec_skills_013](spec_skills_013.md), [spec_agents_014](spec_agents_014.md), [spec_hooks_010](spec_hooks_010.md), [spec_mcp_009](spec_mcp_009.md), [spec_project_config_031](spec_project_config_031.md) |

## 1. Purpose

A plugin bundles what extends Blitz (skills, commands, agents, hooks, MCP servers) so it can be shared, installed in one step, turned on and off, and updated. Because hooks and MCP servers run code, installing shows exactly what a plugin adds, and an installed plugin is pinned by the hash of its content.

## 2. A plugin

- **PLG-01** A directory with `plugin.toml` (`name`: lower-case letters, digits, `.`, `_`, `-`; `version`, default `0.0.0`; `description`, `author`, `homepage`) and any of: `skills/<name>/SKILL.md`, `commands/*.md`, `agents/*.md` (the formats Blitz reads elsewhere), `hooks.toml` (`[[pre_tool]]` and the other events, as `[hooks]`), `mcp.toml` (`[[servers]]`, as `[mcp]`; each needs a name and one of command or url). `${BLITZ_PLUGIN_ROOT}` in hooks' `command` and `args`, and MCP servers' `command`, `args` and `env`, is the plugin's directory when it loads.
- **PLG-02** `Summary` lists what it adds, what runs code first: each hook, each MCP server (a command runs code; a URL is named), each skill (its scripts run code when used), command and agent.

## 3. Installing

- **PLG-10** `blitz plugin install <src>`: a directory, a `.zip` (one top folder allowed; entries leaving the archive refused; links skipped; 64 MB unpacked), an http(s) URL of a zip (64 MB), a git repository (`git@…`, `…/x.git`, or a GitHub, GitLab or Codeberg repository URL; `#ref` picks a branch or tag; shallow clone, no prompts), or `name` / `name@marketplace` (§5). It prints the summary and asks (the question says when it runs code); without a terminal it refuses unless `--yes`.
- **PLG-11** It's copied (without `.git` or links) to `~/.blitz/plugins/<name>/<version>`, its hash (SHA-256 over each file's path, size and content, in path order; 64 MB) recorded in `~/.blitz/plugins/installed.toml` with its source and time (owner-only). A plugin already installed is replaced, keeping whether it was enabled; another version's files go.
- **PLG-12** `list` (name, version, enabled or disabled, whether its files changed, source), `show <name>`, `enable`/`disable <name>` (every workspace), `remove <name>`, `update [name…]` (fetches from the source again; says when nothing changed, else shows the summary and asks, or `--yes`).

## 4. Loading

- **PLG-20** When a workspace opens, after its project settings: the store's enabled plugins, plus those `[plugins] enable` names (the user's settings, or a trusted project's: a tier B item needing trust), less those `[plugins] disable` names (the user's, or any project's: tier A), then `--plugin-dir` directories (repeatable; the run is local, not in the service).
- **PLG-21** An installed plugin whose files don't hash as recorded isn't loaded (a warning says to update it). An enabled name that isn't installed is a warning.
- **PLG-22** A plugin's skills directory joins `skills.paths` (the skill policy applies to its scripts), its agents are loaded like `~/.blitz/agents`, its commands come after the user's and before the project's (a project's command of the same name wins; `/help` shows the source `plugin <name>`), its hooks run after the user's and the project's (`/hooks` shows `plugin <name>`), and its MCP servers are added unless one of the same name exists (a warning).
- **PLG-23** `blitz doctor` lists installed plugins: version, disabled, or changed since installed.

## 5. Marketplaces

- **PLG-30** A marketplace is a `marketplace.toml`: `name`, and `[[plugins]]` with `name`, `version`, `description`, `source` (a git repository, an https zip, or a path relative to the marketplace) and `hash`.
- **PLG-31** `blitz plugin marketplace add <url|git repo|dir>` reads it and remembers it by its name (`~/.blitz/plugins/marketplaces.toml`); `list`, `remove <name>`.
- **PLG-32** `install name` looks it up in every marketplace (several offering it: say `name@marketplace`); the plugin fetched must have that name and the listed hash, else nothing is installed. An entry without a hash can't be installed. Updates fetch the index again.

## 6. Importers

- **PLG-40** `blitz plugin import claude <dir> [--out dir]` converts a Claude Code plugin: `.claude-plugin/plugin.json` (name made a valid plugin name), `commands/`, `skills/` (copied), `agents/` (frontmatter rewritten: `tools` mapped to Blitz's tool names, `model` and other keys dropped), `hooks/hooks.json` (events mapped, `PreToolUse` to `pre_tool` and so on; matchers such as `Edit|Write` become one hook per Blitz tool; `mcp__server__tool` matchers become globs; only `command` hooks), `.mcp.json` (stdio and HTTP servers). `${CLAUDE_PLUGIN_ROOT}` becomes `${BLITZ_PLUGIN_ROOT}`.
- **PLG-41** `import gemini <dir>` converts a Gemini CLI extension: `gemini-extension.json` (MCP servers: command, args, env, `httpUrl`, timeout; `${extensionPath}` becomes `${BLITZ_PLUGIN_ROOT}`) and `commands/**/*.toml` (`a/b.toml` becomes `/a-b`; `{{args}}` becomes `$ARGUMENTS`).
- **PLG-42** Each prints what it added and what it didn't convert, and why: unknown tools and events, SSE servers, prompt hooks, `cwd`, context files (plugins don't add instructions), `excludeTools`, output styles, Gemini's `!{…}` and `@{…}` (kept as text). The result must load as a plugin; the output directory must be new or empty.

## 7. Known limitations

- No output styles in plugins yet (they come with [spec_parity_027](spec_parity_027.md) §3.3), and no dependencies between plugins.
- The desktop app has no plugin view: plugins are managed from the shell.
