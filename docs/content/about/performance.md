---
title: Performance
weight: 10
---

Blitz is compiled Go: the CLI and the service are static binaries with no runtime to install, start in tens of milliseconds, and idle in a few tens of megabytes. The time a turn takes is the model's; Blitz's aim is to add nothing noticeable to it and never to block on its own bookkeeping.

## Measured

Release builds (`--config=release`) of commit `ab35739`, measured 2026-09-27 on an Apple M2 Max, macOS 27.0, warm file cache, with an empty home directory and no API keys.

| | Median | Peak memory (RSS) |
|---|---|---|
| `blitz --version`, start to exit | 19 ms (20 runs) | 39–40 MB |
| `blitzd`, start until its socket accepts | 12 ms (8 runs) | 34 MB idle |
| `blitzd`, first request for a workspace (opens it: agents, skills, tools, sessions, model) | 44 ms | 39 MB with one workspace open |
| `blitzd`, later requests (`GetModel` over the socket, including starting `curl`) | 7 ms (20 runs) | |

## Size

The release archives, each holding `blitz`, `blitzd`, `blz` and the license files:

| Platform | Archive | `blitz` | `blitzd` |
|---|---|---|---|
| macOS, Apple silicon | 35.3 MB | 70.5 MB | 60.5 MB |
| macOS, Intel | 37.2 MB | 73.9 MB | 63.5 MB |
| Linux, x86-64 | 45.4 MB | 86.5 MB | 76.2 MB |
| Linux, ARM64 | 41.6 MB | 81.8 MB | 71.9 MB |
| Windows, x86-64 | 37.0 MB (zip) | | |

The binaries are already stripped. Most of their size is dependencies, the largest being the OpenAI and Anthropic SDKs (about 5 MB and 3 MB of code); Blitz's own code is about 1 MB.

The desktop app for macOS, `Blitz.app`, is universal (both architectures in each binary): about 280 MB, of which the bundled `blitz` and `blitzd` are 137 MB and 118 MB and the app itself 24 MB. Its page is 1.4 MB of JavaScript (441 kB gzipped), with the editor's language support in 117 smaller chunks loaded only when a file in that language opens.

## By design

What keeps Blitz's own work off the critical path:

- **Nothing waits on the disk or the network for bookkeeping.** The diagnostic log, OpenTelemetry export and observing hooks (`post_tool` and friends) all run on background goroutines. Observing hooks queue up to 256 events and get 5 seconds to finish at exit; telemetry gives up after 3 seconds if the collector is unreachable.
- **Tools run in parallel.** Up to `tools.max_parallel` (8) tool calls from one model response run at once, and each sub-agent is capped separately.
- **Text streams.** Model output is shown as it arrives, in the terminal and in the desktop app, whose API responses are streamed through its socket proxy unbuffered.
- **Failing dependencies are skipped fast.** A model or MCP server that keeps failing is put behind a circuit breaker (15 seconds, doubling to 5 minutes) instead of being retried on every call; a stalled model request fails after `llm.stall_timeout_seconds` instead of hanging the turn.
- **Conversations stay small.** Images are stored once, by hash, and referred to in history, so sessions don't carry megabytes of base64. History is compacted when a prompt passes `context.token_threshold`, and the Anthropic provider caches the system prompt.
- **Checkpoints are stored by content hash**, so a file saved many times costs one copy per distinct version, within `[checkpoints] max_bytes`.
- **The service opens each workspace once**, sharing the open between clients that ask at the same time, and keeps it for later requests.

## Compared with the Python original

Blitz began as a Go port of Code Puppy, a Python agent. Measured on 2026-09-24, before the Python code was retired (see [history](history.md)):

| | Python | Go |
|---|---|---|
| Warm `--version` | 0.960 s | 0.020 s |
| Peak memory, `--version` | 136 MB | 37 MB |
| First run after install | 25.3 s (environment set-up) | 0.76 s (macOS scanning the new binary) |
| Distribution | A Python 3.11+ environment and wheels | One static binary |
| Non-test source | 271 files, 71,962 lines | 91 files, 16,443 lines |

## Measuring it yourself

```bash
bazel build --config=release //apps/cli:blitz //apps/service:blitzd //release:archives
/usr/bin/time -l bazel-bin/apps/cli/blitz --version       # macOS; GNU time -v on Linux
```

For the service, start `blitzd --socket <path>` with a scratch `HOME`, then time a request with `curl --unix-socket <path> -H 'Content-Type: application/json' -d '{"workspace": "<dir>"}' http://localhost/blitz.v1.WorkspaceService/GetModel`. Unset your API keys first, so nothing reaches a model.
