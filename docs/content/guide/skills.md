---
title: Skills
weight: 60
---

Spec: [skills](../about/specs/spec_skills_013.md).

Skills are `SKILL.md` files in `~/.blitz/skills` and the project's `./skills` and `.agents/skills`; a project's skill runs its scripts only once you trust the project's settings ([Sharing settings with your team](../configuration/#sharing-settings-with-your-team)). Their frontmatter takes the Agent Skills fields (`name`, `description`, `license`, `compatibility`, `allowed-tools`, `metadata`) and the fields of Castor's skill definition (`castor.skills.v1.SkillDefinition`), under their proto names:

```yaml
---
name: gh-issues
description: Triage GitHub issues
tool_requirements:
  - {name: Bash, scopes: ["gh:*"], description: read issues}
execution_hints:
  hitl_tier: TIER_2_AUDITED_WRITE      # a tier name, never a number
  environment_variables: [GITHUB_TOKEN]
  custom_hints: {network: "true"}      # the scripts need the network
scripts:
  - name: list
    language: python
    relative_path: scripts/list.py
    dependencies: ["requests>=2.31"]
    timeout_seconds: 60
---
```

## The skills policy

`[skills.policy]` caps what skills may ask for: the approval tier, script languages, network, which environment variables pass through, timeouts, trusted content hashes, denied tools and scopes, and packages (the index, wheels only, allow and deny lists, pinning). The stricter of the skill's request and the policy always applies:

- A missing tier gets `min_hitl_tier` (default 2).
- `TIER_0_BYPASS_ALL` needs both the skill's `allow_hitl_bypass` and the policy's; otherwise it becomes tier 3.
- Dependencies must be plain package requirements: URLs, paths and pip options are refused.

`/skills show <name>` lists every decision and the skill's content hash, for `trusted_hashes`. `blitz doctor` reports skills that don't parse and scripts the policy blocks.

| Tier | Approval | Can write |
|---|---|---|
| 1 | None (audited) | Nothing but its private `/tmp` |
| 2 (default) | None (audited) | Its output directory |
| 3 | Asked every time; can't be remembered | Its output directory |
| 0 (bypass; both sides must allow it) | None | Its output directory |

## Where scripts run

`skills.policy.sandbox` chooses the sandbox, and `blitz doctor` shows which is in use:

- **`gvisor`** (Linux). Each script gets its own gVisor sandbox, whose user-space kernel keeps a kernel exploit in a script or package away from the host. Only the host's binaries and libraries, `/etc`, and the script's own paths are mounted; home directories don't exist inside, `/tmp` is private, and the network is off unless allowed. Install gVisor's release (`runsc`) on your `PATH`, in `~/.blitz/bin`, or at `RUNSC_PATH`.
- **`os`**. Seatbelt or bubblewrap, as for shell commands.
- **`auto`** (the default) uses gVisor when a test run works, otherwise the OS sandbox.

A script sees only the variables the policy passes, and it's stopped at its timeout or when you press Ctrl+C. Without a sandbox (on Windows, for example) scripts don't run.

## Running scripts

`activate_skill` lists a skill's scripts and whether the policy lets each run; the agent runs them with `run_skill_script`. Scripts are Python (the system `python3`) or TypeScript (the system `node`, 22.6 or later, which runs TypeScript by stripping its types).

- **Dependencies** go into an isolated environment in `~/.blitz/envs`, one per distinct set of requirements. It's built inside the sandbox, with the network on and writes allowed only to the environment and the package cache, using `uv` if it's installed (else `venv` and `pip`). You approve each install once, with the package list shown. The system Python is never touched. `/envs` lists environments; `/envs prune` removes the unused. TypeScript's npm packages go into `~/.blitz/node-envs` the same way, installed with `npm install --ignore-scripts` so no package's code runs at install; `[skills.policy.packages.npm]` sets the `registry`, `allow` and `deny` lists, and `require_exact` versions.
- **Scripts read the workspace but never write it.** At tier 2 or above a script writes to its own `.blitz/skill-output/<skill>/<run>/` (`$SKILL_OUTPUT`). The agent reads the results there and makes any changes with the file tools, so diffs, approvals, checkpoints and `/undo` work as usual.
