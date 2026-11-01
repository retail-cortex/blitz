---
title: "catalog"
weight: 10
---

An organization's catalog, checked as Blitz reads it. Source: [`pkg/catalog`](https://github.com/retail-cortex/blitz/tree/main/pkg/catalog).

An organization's catalog is a repository laid out as a workspace's `.agents` folder: the agents, rules, skills, guardrails, commands, MCP servers and connectors every person's Blitz works under ([spec_053_enterprise](../about/specs/spec_053_enterprise.md) EN-30, EN-31). `pkg/catalog` reads each file with the engine's own parsers, so a file Blitz would refuse can't be published; the engine's packages stay private.

| Path | Item |
|---|---|
| `agents/<name>.md` | an agent |
| `rules/<name>.md` | a rule |
| `skills/<name>/SKILL.md` | a skill, with its other files beside it |
| `guardrails/<name>.md` | a guardrail |
| `commands/<name>.md` | a slash command |
| `mcp/<name>.toml` | an MCP server: `[mcp.servers]`'s fields, each secret written as `{secrets:<name>}` |
| `connectors/…/<name>.yaml` | a connector's descriptor or connection |

Each item may say how it applies: `lock` (`locked`, or `default`, which a workspace or the person may override) and `groups` (who it's for; everyone when it names none).

## API

| Name | |
|---|---|
| `Of(path)` | What kind of item a path is, and its name |
| `Check(path, data)` | The file read as Blitz reads it: the item, and what's wrong |
| `Item`, `Item.For(groups)` | An item, and whether it applies to someone in those groups |

## Used by

The enterprise service's catalog builds.

Spec: [enterprise](../about/specs/spec_053_enterprise.md).
