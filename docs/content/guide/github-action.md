---
title: GitHub Action
weight: 110
---

Spec: [parity](../about/specs/spec_parity_027.md) §11 (PAR-INT-01).

The Blitz action runs the agent in your repository's GitHub Actions: it answers `@blitz` in issues and pull requests, reviews pull requests, or turns an issue into a pull request. It installs a Blitz release (its signature checked), runs `blitz exec` with a cost, time and turn limit, and posts the answer as a comment, or the change as a pull request.

## Answer mentions

```yaml
# .github/workflows/blitz.yml
on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
permissions:
  contents: read
  issues: write
  pull-requests: write
jobs:
  blitz:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: retail-cortex/blitz/action@v0.2.0
        with:
          gemini-api-key: ${{ secrets.GEMINI_API_KEY }}   # or anthropic-api-key, openai-api-key
```

Only the repository's owners, members and collaborators can start a run: a mention from anyone else is ignored. The agent runs in `dont-ask` mode, so it reads and searches but anything that would need approval is refused. Give it more with `allow`, one rule per line:

```yaml
        with:
          allow: |
            shell(go test ./...)
            shell(npm test)
```

## Review pull requests

```yaml
on:
  pull_request:
    types: [opened, synchronize]
jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: retail-cortex/blitz/action@v0.2.0
        with:
          mode: review
          anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

## Turn an issue into a pull request

```yaml
on:
  issues:
    types: [labeled]
jobs:
  implement:
    if: github.event.label.name == 'blitz'
    runs-on: ubuntu-latest
    permissions:
      contents: write
      issues: write
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
      - uses: retail-cortex/blitz/action@v0.2.0
        with:
          mode: issue-to-pr
          gemini-api-key: ${{ secrets.GEMINI_API_KEY }}
          allow: shell(go test ./...)
          max-cost-usd: "5"
```

In `issue-to-pr` mode the agent may edit files (`accept-edits`); commands need `allow` rules. Blitz commits its changes to `blitz/issue-<n>`, pushes the branch and opens a pull request that resolves the issue.

## Inputs

| Input | Default | |
|---|---|---|
| `mode` | `auto` | `auto` (answer a mention), `review`, `issue-to-pr` |
| `trigger-phrase` | `@blitz` | what a comment must contain, in `auto` mode |
| `prompt` | | a prompt of your own instead of the one made from the event |
| `model` | the provider's | `provider/model` |
| `gemini-api-key`, `anthropic-api-key`, `openai-api-key` | | one of them, from a secret |
| `allow` | | permission rules to allow, one per line |
| `max-cost-usd`, `timeout`, `max-turns` | `2`, `20m`, `60` | the run stops at the first it reaches |
| `version` | the latest | the Blitz release to install |
| `sandbox` | `required` | the OS sandbox for the agent's commands (`sandbox.shell`): on Linux runners the action installs bubblewrap and lifts Ubuntu's restriction on user namespaces; `auto` runs commands unsandboxed where that can't work (Windows has no sandbox, so needs it) |
| `github-token` | `github.token` | to comment and open pull requests |

The answer is also the action's `result` output. Nothing of the session is kept (`--no-session-persistence`), and project settings that need trust (`.blitz/settings.toml` hooks and MCP servers) stay off.
