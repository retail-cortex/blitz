---
title: "065 · Packages"
weight: 65
---

*Agentic assets as packages: agents, skills, workflows, rules, guardrails and evals, each versioned, depending on one another by version range, so one agent can run a skill's version 1 while another runs its version 2. Read alike by Blitz, by an organization's library, and, in time, by builders outside Blitz.*

| | |
|---|---|
| Status | **PK-01 to PK-05 built** (2026-11-15, the owner's plan R0: "versioned assets that can grow over time such that an agent may depend on skill version 1, while another agent depends on version 2"); **PK-10 to PK-16 built** 2026-11-15 (R6: Castor's serialization absorbed, "for people building outside Blitz"; the castor repository and its `cstr` stay as they are, the owner's call). |
| Source | `pkg/agentpkg` (`version.go` versions and ranges; `dep.go` a dependency; `uri.go` addresses; `files.go` assets in their folders; `lock.go` the lockfile and manifest); `pkg/packages` (add, verify, validate, compile); `pkg/enterprise/registry.go`; `apps/cli/pkg.go` (`blitz pkg`); `pkg/engine/agents/frontmatter.go` (an agent's `skills:`, spec_014_agents AG-45); `pkg/catalog` (an organization's files checked as Blitz reads them, workflows and evals with them) |
| Tests | `pkg/agentpkg`: `TestVersion`, `TestRange`, `TestDep`, `TestDigest`, `TestParseURI`, `TestAssetsAndLock`; `pkg/packages`: `TestAddAndVerify`, `TestCompileAndValidate`; `pkg/enterprise`: `TestRegistryClient`; `apps/cli`: `TestPkgCommand`; `pkg/engine/agents`: `TestAgentSkills`, `TestParseMarkdownSpecRejects`; `pkg/catalog`: `TestOf`, `TestCheck` |
| Depends on | [spec_014_agents](spec_014_agents.md), [spec_013_skills](spec_013_skills.md), Blitz Enterprise Server |

## 1. Purpose

A skill improves; an agent built on it shouldn't break when it does. So every asset has versions, and what uses it says which versions it takes. An organization's library keeps them and says what's out of date; Blitz runs each agent with the versions it was published with.

## 2. Requirements

- **PK-01** A **version** is `major.minor.patch` (a leading `v` allowed), compared part by part. The next is a **patch** (a fix), a **minor** (an addition) or a **major** (a change others must follow).
- **PK-02** A **range** is the versions a dependency takes: any (empty, `*` or `latest`), one (`1.4.2`), a major (`1`, `1.x`), a minor (`1.4`, `1.4.x`), a caret (`^1.4.2`: from it, before the next major; before 0.5.0 for a 0.4.x) or a tilde (`~1.4.2`: from it, before the next minor). Of the versions there are, a range resolves to the latest it takes.
- **PK-03** A **dependency** names an asset and its range: `name`, `name@range`, or `{name: …, version: …}`; written back as `name` or `name@range`. Its name is Blitz's (lowercase letters, digits, single hyphens or underscores, up to 64), and a range that doesn't read is refused.
- **PK-05** A version's **digest** (`Digest`): for each of its files in path order, the path, a zero byte, its text and a zero byte, hashed with SHA-256; the organization's service and Blitz compute it alike, and Blitz refuses an asset whose files don't match it.
- **PK-04** `pkg/catalog` checks every kind of asset an organization keeps as Blitz reads it: agents, rules, skills (and their files), guardrails, commands, **workflows** (`workflows/<name>.flow.yaml`) and **evals** (`evals/<name>.md`), MCP servers and connectors.

### Packages (R6)

- **PK-10** A package's **address** (`agentpkg.ParseURI`), read as Castor reads its roots: `blitz://<host>/<kind>/<name>[@<range>]` (an organization's registry), `github://<owner>/<repo>[@<ref>][/<folder>]` (or `https://github.com/…` with `/tree/<ref>/`, `:<ref>`; `main` when none), `file://<path>` or a bare path, and Castor's own `castor://` (`cstr://`, `castors://`, `skm://`), `mod://` (`go://`) and `maven://` (`mvn://`), which Blitz reads but leaves to Castor's `cstr` to fetch.
- **PK-11** A package's **assets** are laid out as a workspace's `.agents` folder is (`agentpkg.Folders`): `agents/<name>.md`, `skills/<name>/` (`SKILL.md` and its other files), `workflows/<name>.flow.yaml`, `rules/<name>.md`, `guardrails/<name>.md`, `evals/<name>.md`; read as text (a file that isn't, or over 1 MB, or an asset over 5 MB or 200 files, isn't one; hidden files are left out, a linked skill folder isn't read), and written replacing what the asset had, only its own paths.
- **PK-12** **`blitz pkg add <address>`** writes a package's assets where Blitz reads them and records each in the **lockfile**, `.agents/blitz.lock` (`agentpkg.Lock`, a superset of Castor's `.manifest.lock`): its kind, name, address, version and digest (PK-05), when added. A folder may be a skill's, a folder of skills (as Castor's roots are), or laid out as `.agents`; a single Markdown file is an agent unless its folder or `--kind` says otherwise. From GitHub, the repository's archive at the ref (`GITHUB_TOKEN` sent). An asset of the workspace's own of the name, or from another address, is kept unless `--force`.
- **PK-13** **`blitz pkg compile`** writes `.agents/agents_manifest.json` (`agentpkg.Manifest`): every asset by `kind/name`, with its description, digest, version and source (the lockfile's, else its file), dependencies (an agent's `skills:`) and files; `--castor` writes Castor's `skills_manifest.json` beside it (each skill's name, description, instructions, path, source URI, digest, references and examples), for Castor's loaders.
- **PK-14** **`blitz pkg validate`** checks each asset as Blitz reads it (`pkg/catalog`, the engine's parsers, in the service: `WorkspaceService.ValidateAssets`, since the CLI never reaches the engine) and says what's worth fixing (Castor's audit: no description; a relative link to a file the asset hasn't); it exits 1 on a problem, `--json` for its findings.
- **PK-15** **`blitz pkg verify`** checks each locked asset against its digest: as added, changed since, or missing; it exits 1 unless every one is as added. `blitz pkg list` lists the workspace's assets, each package with its address and version.
- **PK-16** **The registry** (`enterprise.Registry`): `blitz pkg search [words] [--kind]` and `blitz pkg publish <kind>/<name> [--bump] [--note]` (through the library's evals: published when they pass), and the `blitz://` addresses `add` fetches, go to the organization's registry as the signed-in person, or, for CI and builders outside Blitz, with a registry token in `BLITZ_REGISTRY_TOKEN` and `--registry` (or the organization Blitz is connected to). A package of another host's registry is refused.

## 3. Not yet

- Fetching `castor://`, `mod://` and `maven://` packages in Blitz (Castor's `cstr` does), and a lock's dependencies resolved together.
