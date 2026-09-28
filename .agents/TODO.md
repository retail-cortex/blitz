1. Project Configuration & Workspace Trust Boundary (.blitz/settings.toml)
Problem: Blitz only loads user-global configuration (~/.blitz/) and instruction files (AGENTS.md, CLAUDE.md). Teams cannot commit workspace-specific MCP servers, custom command definitions, tool permission presets, or hooks to their repositories.
Value: Essential for team adoption. It standardizes tooling per repository while enforcing the cryptographic hash-pinned trust model (spec_parity_027 §2.4) to eliminate the RCE and credential exfiltration vulnerabilities seen in Claude Code (CVE-2025-59536, CVE-2026-21852).
Implementation:
- Add layered TOML parsing for .blitz/settings.toml and .blitz/settings.local.toml.
- Safe settings (scoped rules, prompt commands, stricter path/network denies) apply automatically.
- Execution settings (hooks, command-based MCP servers, tool allow rules) require explicit one-time confirmation recorded in ~/.blitz/trust.json keyed by workspace path and config SHA-256.

2. Cached Directory Masking for the Linux Shell Sandbox (pkg/engine/tools/bwrap.go)
Problem: On Linux, bwrap sandboxing (pkg/engine/tools) traverses all writable workspace roots to generate --ro-bind / --tmpfs masking rules before every shell execution. On workspaces with >10,000 files, this costs ~100 ms per invocation (3+ seconds under -race), forcing CI to disable the sandbox in parallel test runs (BL-SH-01).
Value: Immediate latency reduction for every shell command in Linux and CI environments, enabling full sandbox enforcement across all test suites without slowing down tool execution.
Implementation:
- Cache the scan tree and mask paths keyed by root directory modification times (mtime) and inotify watches.
- Bypass pattern scans in known build and cache directories (e.g., .git, bazel-out, node_modules, /tmp/go-build) when rule globbing permits.
- Re-enable the OS sandbox in CI's parallel execution integration tests.

3. Non-Blocking Background Sub-Agents and Task Orchestration (pkg/engine/agents/)
Problem: Currently invoke_agent runs synchronously inside the ADK tool dispatch loop (pkg/engine/agents/). If a sub-agent executes long test suites or searches, the parent turn and user interface are blocked.
Value: Enables true parallel development—e.g., launching an exploratory test runner or documentation retriever in the background while the primary agent continues refactoring or interacting with the user.
Implementation:
- Extend invoke_agent with a background: true parameter returning a task ID immediately (spec_parity_027 §8.1).
- Add list_tasks, task_output, and stop_task engine tools.
- Pipe background task completions back into the engine event loop as steering messages at the next tool cycle or prompt turn.

4. Token, Cost, and Context Accounting Persistence Across Restarts (pkg/engine/runtime/usage.go, pkg/engine/session)
Problem: UsageTracker stores session token counts, context sizes, and USD costs strictly in an in-memory map. When a session is resumed (--resume, /resume, --continue), when blitzd restarts, or when scheduled workers execute across service restarts, /cost and /context reset to zero (BL-ENG-10, BL-ENG-11).
Value: Enforces financial safety guardrails (--max-cost-usd) accurately across process boundaries, fixes inaccurate context compaction thresholds upon resume, and provides reliable cost auditing for multi-turn sessions and scheduled workers.
Implementation:
- Serialize cumulative api.Usage into session metadata via session.Store.UpdateMetadata after each model turn.
- Restore the session's usage baseline in UsageTracker when engine.Open or Workspace.ResumeSession opens a session.
- Expose cumulative usage in worker run records so total task cost survives service restarts.

5. Client–Daemon Parity for Background Processes and Shell Auditing (pkg/client/client.go, apps/service/internal/server)
Problem: When the CLI attaches to blitzd, pkg/client stubs out process management (Processes() returns nil) and shell escape auditing (AuditShell is a no-op) (BL-SVC-01, BL-SVC-02).
Value: Prevents orphaned background processes started by tools (e.g. servers, watchers) when running with the daemon, and ensures user shell escapes (!cmd) maintain an unbroken security audit trail in the workspace audit log.
Implementation:
- Add ListProcesses, GetProcessOutput, KillProcess, and AuditShell RPCs to WorkspaceService in proto/blitz/v1/workspace.proto.
- Implement Remote.Processes() in pkg/client to wrap these RPCs, scoped to the active session ID.
- Forward client-side !cmd executions to the daemon via AuditShell to record user_shell events in the central audit log.
