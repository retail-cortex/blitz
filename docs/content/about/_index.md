---
title: About
weight: 60
---

Blitz is an AI agent built for speed and focus, for code and everything around it: terse output, plain status marks, and a short summary when it's done. Like a good working bird dog, it moves without waste, holds its position quietly, and stays on the task.

| | |
|---|---|
| [Performance](performance.md) | Startup, memory, size, and the design choices behind them |
| [Security log](security-log.md) | Security problems found and fixed, and the known gaps |
| [Roadmap](roadmap.md) | What was built, in order, and why |
| [Specifications](specs/_index.md) | Every requirement, numbered and traced to code and tests |
| [History](history.md) | The port from Python, and how the two compared |

## The specifications

Blitz is specified in a set of documents, written from the code and kept with it: each has numbered requirements (`CLI-01`, `FS-30`, …) that trace to the code and its tests, and its known gaps. They're numbered in build order, from project setup to release, so reading them in order rebuilds the system bottom-up. A check in CI (`//tools/specs`) keeps the index complete and every path they name real.

A few to start with:

- [Workspace](specs/spec_018_workspace.md): the UI-independent engine and the `api.Backend` contract every front end drives.
- [Service](specs/spec_021_service.md): `blitzd`, its socket and Connect API, and approvals across processes.
- [Approvals](specs/spec_005_approvals.md) and [shell](specs/spec_007_shell.md): permission modes and rules, the command policy, the OS sandbox.
- [Skills](specs/spec_013_skills.md): Agent Skills with Castor's definitions, the skills policy, and scripts sandboxed by gVisor.
- [Monorepo](specs/spec_028_monorepo.md): the layout, the dependency rules and the Bazel build.
- [Backlog](specs/spec_026_backlog.md) and [parity](specs/spec_027_parity.md): what's still missing, including against Claude Code and Antigravity.

## License

Blitz is licensed under the [Apache License 2.0](https://github.com/retail-cortex/blitz/blob/main/LICENSE). See [`NOTICE`](https://github.com/retail-cortex/blitz/blob/main/NOTICE), and [`THIRD_PARTY_NOTICES`](https://github.com/retail-cortex/blitz/blob/main/THIRD_PARTY_NOTICES) for the software Blitz includes. Every program shows them: `blitz license [full|third-party]` (and `/license`), `blitzd --license`, and the desktop app's **Settings › About**.

Blitz began as a Go port of [Code Puppy](https://github.com/mpfaffenberger/code_puppy) by Mike Pfaffenberger (MIT). The Python implementation was removed after the port and is kept at the `python-final` tag of the former repository, `rmcguinness/code_puppy`; this repository starts from a fresh history.

This site is built with [Hugo](https://gohugo.io) and the [Geekdoc](https://geekdocs.de) theme by Robert Kaussow, under the MIT License ([its license](../licenses/hugo-geekdoc.txt); the theme's icons are under [their own licenses](https://github.com/thegeeklab/hugo-geekdoc#license)).
