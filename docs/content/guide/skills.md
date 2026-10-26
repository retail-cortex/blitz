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

## Kinds of skill, and Studio

A skill is one of four kinds, from what its folder holds:

| Kind | What it has |
|---|---|
| Simple | `SKILL.md` alone |
| With references | A `references/` folder: the agent reads its files when the skill calls for them (`read_skill_file`) |
| With scripts | Python (`.py`), TypeScript (`.ts`), JavaScript (`.js`, `.mjs`), shell (`.sh`, macOS and Linux), cmd (`.cmd`, `.bat`, Windows) or PowerShell (`.ps1`, where `pwsh` is installed) files in `scripts/`, or scripts declared in the front matter, run sandboxed under the skills policy. A `requirements.txt` or `pyproject.toml` (Python) or `package.json` (JavaScript, TypeScript) gives them an isolated environment with those packages, installed once you approve: in Studio, the Scripts tab's **Packages** section adds and edits them |
| Combines skills | `skills: [a, b]` in the front matter: activating it activates them too |

`agents: [reviewer]` in the front matter offers a skill only to those agents.

**Desktop:** **Studio › Skills** lists the workspace's skills (`.agents/skills`). **+** makes one; click one to edit it: **General** (what it's for, its name, instructions with **Redraft**, the skills it combines, the agents it's offered to, its tools), **References** (add, upload, edit or delete files; PDFs, images and Markdown preview) and **Scripts** (a new script from a template, **Redraft** to have the model write it, and **Test run** with arguments). Opening a file gives it the whole tab; the back arrow returns to the list. **Add from…** copies any skill you can see into the workspace to change it, or references it in the skill you're editing.

**In a prompt:** type `\` and a skill's name (the composer completes it) to use that skill for the request: its instructions go with your prompt, as `/skill-name` does.

## Trigger phrases and scenarios

**Trigger phrases** tell the agent what a skill is for, by example: `trigger_phrases: ["write the release notes", "what changed since the last release"]` in the front matter, or **Trigger phrases** under the description in Studio. They're examples, not commands: the agent matches a request to them by meaning, so "draft the notes for 2.4" brings in a skill whose phrase is "write the release notes". Skill search (`list_or_search_skills`) finds skills by them too.

**Scenarios** check that a skill works: that the agent picks it up when it should, leaves it alone when it shouldn't, and answers well. Each is a file in the skill's `scenarios/` folder, in [Castor](https://github.com/retail-cortex/castor)'s format, so a skill carries its tests wherever it's shared:

```markdown
---
description: Notes from the log
prompt: Write the release notes for v2.4
executes: true          # it should use the skill; false: it shouldn't
expected_skills: [release-notes]
threshold: 0.8          # how close the answer must be, 0 to 1
---
## What's new
- Wishlists you can share.
```

In Studio, a skill's **Scenarios** tab is the quickest way to make them:

1. **Try it:** type what a user might ask and choose **Try**. The agent can use any skill, as in a chat, so you see whether it used this one ("It used release-notes" or "It didn't use release-notes") and what it said. **Give it the skill** tries only the answer.
2. **Keep it:** when the result is what you want (or shows what it should have done), keep it as a scenario. It's filled in from the try: whether the skill should be used, the skills it used, and the answer as the one expected. Change anything before saving.
3. **Run all** runs the skill's scenarios in the background. A scenario passes when the right skills were used and the answer is close enough to the one expected.

From a terminal or CI, `blitz eval --skill release-notes` runs them, and fails when one does. Each scenario costs an agent turn and a judge's call. Trying a skill uses it as saved, so save your changes first.

## Reviewing a skill's scripts

A skill's scripts run without asking only once you've reviewed them: Blitz remembers the content you trusted, so a new or changed script (or a registry update) is asked about again. Until then the agent asks you before each run, and a job's run is refused.

- **Desktop:** installing a registry skill with scripts opens the review; Studio › Skills marks a skill **Scripts to review**, with **Review scripts** in its view. The review lists each script, its file and the packages it installs; **Trust these scripts** remembers them.
- Trusting a workspace's project settings trusts its skills' scripts as they are, so you aren't asked twice.

## Skills from a registry

A registry, such as your organization's Castor registry, offers skills to install by name. Each skill is a `.zip` whose SHA-256 the registry gives, and Blitz refuses one that doesn't match.

```sh
blitz skills registry add https://castor.example.com/skills/index.json --token-stdin   # the token is kept in the keychain
blitz skills search literature
blitz skills install castor/lit-review              # for you, in every workspace
blitz skills install castor/lit-review --project    # in this project's .agents/skills, for everyone
blitz skills update                                 # every skill whose registry has a new version
blitz skills uninstall lit-review
```

In a session, `/skills search` also lists what your registries offer, and `/skills install <registry>/<name>` installs one. In the desktop app it's **Settings › Library › Skills** (with the advanced settings shown): add a registry, find skills, install them for you or the open workspace, update and uninstall. Open conversations can use a new skill at once, with no restart.

| Command | Does |
|---|---|
| `blitz skills registry add <url>` | Adds a registry, after reading its index; `--name` names it, `--token-stdin` reads an access token |
| `blitz skills registry list`, `remove <name>` | Lists them, or forgets one and its token (its skills stay) |
| `blitz skills search [words]` | Skills whose name or description has every word |
| `blitz skills install <registry>/<name>` | Installs one (`--project`: in this folder's `.agents/skills`) |
| `blitz skills update [name…]`, `uninstall <name>`, `list` | Keeps them up to date, removes one, lists them |

A registry is `https://` (or `http://localhost`, or a local file), and the token is only ever sent to the registry's own host. An installed skill is an ordinary skill folder: the skills policy below applies to it as to any other, and Blitz never replaces a folder it didn't install.

**Serving a registry.** An index is a JSON file:

```json
{
  "version": 1,
  "name": "Castor",
  "skills": [
    {"name": "lit-review", "description": "Reviews the literature on a question", "version": "1.2.0",
     "archive_url": "lit-review-1.2.0.zip", "sha256": "9f2c…", "castor_id": "sk_lit"}
  ]
}
```

`archive_url` is absolute or relative to the index. The archive holds `SKILL.md` (naming the skill) at its root or in one folder, and the skill's other files.

## The skills policy

`[skills.policy]` caps what skills may ask for: the approval tier, script languages, network, which environment variables pass through, timeouts, trusted content hashes, denied tools and scopes, and packages (the index, wheels only, allow and deny lists, pinning). The stricter of the skill's request and the policy always applies:

- A missing tier gets `min_hitl_tier` (default 2).
- `TIER_0_BYPASS_ALL` needs both the skill's `allow_hitl_bypass` and the policy's; otherwise it becomes tier 3.
- Dependencies must be plain package requirements: URLs, paths and pip options are refused.

`/skills update` revisits the project's skills against its specification (`.agents/specs/`) and offers what to add, change or retire ([spec, then setup](../tutorials/spec-then-setup.md)). `/skills show <name>` lists every decision and the skill's content hash, for `trusted_hashes`. `blitz doctor` reports skills that don't parse and scripts the policy blocks.

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

- **Dependencies** go into an isolated environment in `~/.blitz/envs`, one per distinct set of requirements. It's built inside the sandbox, with the network on and writes allowed only to the environment and the package cache, using `uv` if it's installed (else `venv` and `pip`). You approve each install once, with the package list shown. The system Python is never touched. `/envs` lists environments; `/envs prune` removes the unused. A Python script can say which Python it needs, with `requires_python` in its definition or `requires-python` in its PEP 723 metadata. When the system's `python3` doesn't fit and [uv](https://docs.astral.sh/uv/) is installed, a uv-managed Python runs it (uv installs one after you approve); without uv the script is refused, saying why.

A skill whose scripts change your files declares `execution_hints.writes_workspace: true`: its scripts then work in a copy of the workspace, and when one succeeds you see what it changed as a diff and decide, every time, whether to keep it. Kept changes are part of the turn, so `/undo` reverts them.

TypeScript's npm packages go into `~/.blitz/node-envs` the same way, installed with `npm install --ignore-scripts` so no package's code runs at install; `[skills.policy.packages.npm]` sets the `registry`, `allow` and `deny` lists, and `require_exact` versions.
- **Scripts read the workspace but never write it.** At tier 2 or above a script writes to its own `.blitz/skill-output/<skill>/<run>/` (`$SKILL_OUTPUT`). The agent reads the results there and makes any changes with the file tools, so diffs, approvals, checkpoints and `/undo` work as usual.
