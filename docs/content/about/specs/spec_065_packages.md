---
title: "065 · Packages"
weight: 65
---

*Agentic assets as packages: agents, skills, workflows, rules, guardrails and evals, each versioned, depending on one another by version range, so one agent can run a skill's version 1 while another runs its version 2. Read alike by Blitz, by an organization's library (blitz-enterprise's spec_014_library), and, in time, by builders outside Blitz.*

| | |
|---|---|
| Status | **PK-01 to PK-03 built** (2026-11-15, the owner's plan R0: "versioned assets that can grow over time such that an agent may depend on skill version 1, while another agent depends on version 2"). Next (R6): the package format, URIs, a compiled manifest and lockfile, and `blitz pkg`, absorbing Castor's (cstr's) serialization for builders outside Blitz. |
| Source | `pkg/agentpkg` (`version.go` versions and ranges; `dep.go` a dependency); `pkg/engine/agents/frontmatter.go` (an agent's `skills:`, spec_014_agents AG-45); `pkg/catalog` (an organization's files checked as Blitz reads them, workflows and evals with them) |
| Tests | `pkg/agentpkg`: `TestVersion`, `TestRange`, `TestDep`; `pkg/engine/agents`: `TestAgentSkills`, `TestParseMarkdownSpecRejects`; `pkg/catalog`: `TestOf`, `TestCheck` |
| Depends on | [spec_014_agents](spec_014_agents.md), [spec_013_skills](spec_013_skills.md), [spec_053_enterprise](spec_053_enterprise.md) |

## 1. Purpose

A skill improves; an agent built on it shouldn't break when it does. So every asset has versions, and what uses it says which versions it takes. An organization's library keeps them and says what's out of date; Blitz runs each agent with the versions it was published with.

## 2. Requirements

- **PK-01** A **version** is `major.minor.patch` (a leading `v` allowed), compared part by part. The next is a **patch** (a fix), a **minor** (an addition) or a **major** (a change others must follow).
- **PK-02** A **range** is the versions a dependency takes: any (empty, `*` or `latest`), one (`1.4.2`), a major (`1`, `1.x`), a minor (`1.4`, `1.4.x`), a caret (`^1.4.2`: from it, before the next major; before 0.5.0 for a 0.4.x) or a tilde (`~1.4.2`: from it, before the next minor). Of the versions there are, a range resolves to the latest it takes.
- **PK-03** A **dependency** names an asset and its range: `name`, `name@range`, or `{name: …, version: …}`; written back as `name` or `name@range`. Its name is Blitz's (lowercase letters, digits, single hyphens or underscores, up to 64), and a range that doesn't read is refused.
- **PK-04** `pkg/catalog` checks every kind of asset an organization keeps as Blitz reads it: agents, rules, skills (and their files), guardrails, commands, **workflows** (`workflows/<name>.flow.yaml`) and **evals** (`evals/<name>.md`), MCP servers and connectors.

## 3. Not yet

- The package format (Castor's `SkillDefinition` generalized to every kind), URIs (`blitz://`, `castor://`, `github://`, `mod://`, `maven://`, `file://`), a compiled manifest, a lockfile, and `blitz pkg add|publish|verify|validate|compile|search|list`; `cstr` on the same package (R6).
- Each agent run with its own versions of its skills, side by side (R1).
