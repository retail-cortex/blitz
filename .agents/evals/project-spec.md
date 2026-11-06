---
description: Checks that /spec (project-spec) writes stages in the exact Markdown skeleton parsed by Studio › Specs.
agents:
  - project-spec
---

| ID | Name | Prompt | Output | Output type | Threshold | Scope |
| --- | --- | --- | --- | --- | --- | --- |
| stage-format | Unattended two-stage spec | Unattended run (do not ask questions; put any assumptions under Open questions): we are building a Go service called "pulse" that polls 50 HTTP health endpoints every 30 seconds and serves a JSON status page in under 100ms (no auth, single binary). Write a two-stage specification (`001-overview.md` and `002-poller.md`) and include the full verbatim Markdown of both stage files in your reply. | The reply includes the full Markdown for `001-overview.md` and `002-poller.md`, and BOTH stages satisfy every one of these exact format rules required by the Studio Specs parser (score 0.3 or lower if any rule is missing):<br>1. Starts with `# 001. <Title>` and `# 002. <Title>`, followed by `Status: draft` (one of `draft`, `in-progress`, `done`) and `Depends on: none` (for 001, plus `Kind: software` on 001) and `Depends on: 001-overview` (for 002).<br>2. Uses `## Context`, `## Goals`, `## Non-goals`, `## Requirements`, `## Done when`, and `## Open questions` as level-2 `## ` Markdown headings (fails if written as bold inline labels like `**Context:**` or `**Done when:**`).<br>3. Every item under `## Requirements` is a Markdown checkbox with a bold ID prefix: `- [ ] **<PREFIX>-NN** <requirement>` (fails if `- [ ]` checkboxes are missing).<br>4. Every item under `## Done when` is a Markdown checkbox: `- [ ] <criterion>` (fails if `- [ ]` checkboxes are missing). | Near | 0.85 | Fail |
