---
title: Packages
weight: 58
---

Your workspace's agents, skills, workflows, rules, guardrails and evals (its `.agents` folder) can be **packages**: added from your organization's registry, from GitHub or from a folder, each recorded with where it came from, its version and a digest of its files, so you can tell when one changes. It's what Castor's `cstr` does for skills, for every kind of asset. `blitz pkg` does it, from the workspace's folder (or `-d`).

Spec: [packages](../about/specs/spec_065_packages.md).

## Adding one

```sh
blitz pkg add blitz://blitz.acme.com/skill/sql-review@1.x
blitz pkg add github://acme/skills@v2/sql
blitz pkg add ../shared/notes
```

- `blitz://<host>/<kind>/<name>@<range>` is your organization's registry: the latest version the range takes (`1.x`, `^1.4`, `1.4.2`, or none for the latest), as you're signed in.
- `github://<owner>/<repo>[@<ref>]/<folder>` is a folder of a repository (`main` when you name no branch or tag): a skill's folder, a folder of skills, or a folder laid out as `.agents` is. `GITHUB_TOKEN` is sent for a private one.
- A folder or file here (or `file://…`): a skill's folder, a `.flow.yaml`, or a Markdown file (an agent unless its folder or `--kind` says it's a rule, guardrail or eval).

Each goes where Blitz reads it, and into `.agents/blitz.lock`. Adding one again from the same address brings it up to date; one of the workspace's own of that name is kept unless you add `--force`. `castor://`, `mod://` and `maven://` addresses are Castor's: use its `cstr` for those.

## Checking them

- `blitz pkg verify` says whether each package is as it was added, and exits 1 when one changed or is missing: run it in CI.
- `blitz pkg validate` checks every asset as Blitz reads it, and says what's worth fixing (no description, a link to a file the skill hasn't); it exits 1 on a problem.
- `blitz pkg list` lists the workspace's assets and where each package came from.

## For loaders outside Blitz

`blitz pkg compile` writes `.agents/agents_manifest.json`: every asset, its files, version, digest and source, in one file. `--castor` also writes Castor's `skills_manifest.json` beside it, so Castor's Go, Python and Java loaders read the same skills.

## Your organization's registry

`blitz pkg search <words>` searches what your organization offers you, and `blitz pkg publish skill/<name> [--bump minor] [--note …]` publishes one of the workspace's as its next version, if you're an owner or admin: its evals run first, and it's published when they pass. In CI, use a registry token your admins make (the console's Organization › Registry tokens), with the service's address:

```sh
BLITZ_REGISTRY_TOKEN=blitzreg_… blitz pkg add blitz://blitz.acme.com/skill/sql-review@1.x --registry https://blitz.acme.com
```

In a conversation, the agent finds what your organization offers with its `search_library` tool, and tells you how to switch one on.
