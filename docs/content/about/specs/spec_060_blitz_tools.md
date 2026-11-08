---
title: "060 · Blitz as a tool"
weight: 60
---

*Ask for anything Blitz does in words and get it: "create a SQLite database of my recipes and add a connector for it" ends with the connection on and `@db:recipes` working; "make a poster for Friday" opens Pictures with the prompt in it; "run the evals on my reviewer agent" shows the cost and runs them. The agent gets one tool, **blitz**, that runs Blitz's own commands, the same commands the `blitz` CLI has, each one that changes something approved through Blitz's forms. A built-in **blitz** skill says which command does what. Making Blitz an MCP server for agents outside it is a later, separate feature (§10).*

| | |
|---|---|
| Status | **Draft** (2026-11-07, the owner's request; decisions in §11). Phase 1 (the tool, connections, the skill) is first. |
| Source | Planned: the command catalog in `pkg/api/blitzcommands.go` (names, arguments, what each does) and its commands in `pkg/engine/blitzcmd/`; the tool in `pkg/engine/tools/blitz.go`; the skill in `pkg/engine/skills/builtin/blitz/`; the CLI's commands in `apps/cli/`; the window's actions in `apps/desktop/web/src/` (the approval's secret fields, opening Pictures or the browser); the terminal's in `apps/cli/internal/tui/` |
| Depends on | [spec_047_connectors](spec_047_connectors.md) (CON-06 files, CON-09 trust, CON-17 on in a workspace, CON-18 Studio stays in its workspace), [spec_046_databases](spec_046_databases.md) (DB-17 paths), [spec_039_secrets](spec_039_secrets.md), [spec_005_approvals](spec_005_approvals.md) (APR-14b auto mode reads answers), [spec_043_workflows](spec_043_workflows.md), [spec_058_pictures](spec_058_pictures.md), [spec_057_spec_driven](spec_057_spec_driven.md), the jobs tools (`create_job`, `list_jobs`) |

## 1. Purpose

Blitz's features are set up in Studio, Settings and the CLI. A person who asks the agent instead gets an agent that doesn't know Blitz: it can't tell where a connection lives, what its file looks like, or how to turn it on.

The owner tried it in the released app (2026-11-08, the recipe-book workspace): create a SQLite database in the workspace's folder for recipes like those in `@recipes/`, fill it with them, then add the data connector. After 20 minutes it still hadn't written the connection. The audit shows why:

- It ran `blitz --help`, `blitz config --help` and `blitz config show`. No command adds a connection, and `config show` failed in the sandbox (it couldn't read the workspace's `.env.toml`).
- It then spent 16 minutes on 20 commands reading the `blitz` binary (`strings`, `nm`, Python reading byte ranges) for the folder name and the YAML format.
- Each of those commands waited for auto mode's reviewer, from 7 seconds to nearly 2 minutes (`blitz config show`: 112 seconds), so most of the time went on approvals of guesses.

Everything it needed is a fact Blitz already has. This spec makes Blitz's functions something the agent calls, as a person does from the CLI or the window.

## 2. Principles

- **DRV-01** **One tool, Blitz's commands.** The agent has one tool, `blitz`, whose arguments are a **command** (`connector save`, `picture`, `eval run`) and its **arguments** (an object). The commands are a catalog (`pkg/api`'s list: each command's name, a line on what it does, its arguments' schema, and whether it reads, changes or costs), implemented in the engine on the functions Studio's calls use, so the files, checks, trust and serving are Studio's and nothing in the agent's instructions can drift from the code. One tool, not a tool a command, keeps what every turn carries small as the catalog grows (§11, decision 2).
- **DRV-02** **The CLI has the same commands.** Each command in the catalog is a `blitz` CLI command of the same name and arguments (`blitz connector save`, `blitz picture`, `blitz eval run`), so what the agent does a person can script, and the reverse. A test fails when a catalog command has no CLI command. The CLI's existing commands (`eval`, `spec`, `jobs`, `agents`, `skills`, `mcp`) come into the catalog as they are; the new ones (`connector`, `workflow`, `open`) are added to both.
- **DRV-03** **Help on demand.** The tool's description names the command groups, a line each. `blitz help` (and `help <command>`) answers with a group's commands and a command's arguments, read from the catalog, so the details are there when the agent needs them and not in every turn.
- **DRV-04** **One form, showing the result.** A command that changes something asks through Blitz's approval (as `create_job` does), showing what will exist (a connection's file, a workflow's steps), not the call's arguments. One that costs shows the cost first (a picture, an eval run, a spec review), as its window does. One that only reads doesn't ask. In auto mode the reviewer judges a change like any other, with the user's request in view (APR-14b). A rule can allow or deny a command (`blitz:connector save`) like any tool.
- **DRV-05** **Secrets never reach the model.** A command takes a secret's **name** (`DB_SHOP_PASSWORD`), never its value. When a named secret isn't set, the approval form asks for its value in a password field, and Blitz saves it as the workspace's secret (spec_039_secrets). A secret's value is never in a command's arguments, its result, the transcript or the audit.
- **DRV-06** **The workspace only.** As in Studio (CON-18), the commands change this workspace's items. They list global ones, and turn one off or on here (CON-17), but never change, delete or make one global: that stays the person's, in Settings.
- **DRV-07** **The window acts, the terminal answers.** A command that opens something (Pictures, the browser, a Studio section) opens it in the desktop app's window, with what the agent gave it filled in, and the person carries on there. In the terminal the same command does what the terminal can: `picture` makes the picture after its cost is shown and saves it; `open` opens the system browser after asking.
- **DRV-08** **Not through the shell.** The tool runs the command in the engine, so it never meets the sandbox's limits (`config show`'s) and never waits for a shell command's review. The skill tells the agent not to run `blitz` in the shell for what the tool does.

## 3. The commands

By phase. Each group's commands are listed by `blitz help`.

| Group | Commands | Reads, changes or costs | Phase |
|---|---|---|---|
| `connector` | `list`, `save`, `enable`, `disable` | `list` reads; the others change | 1 |
| `help` | `help`, `help <command>` | reads | 1 |
| `picture` | `picture` (prompt, model, references, shape) | costs; opens Pictures | 2 |
| `open` | `open url`, `open file`, `open studio <section>` | opens; `open url` asks | 2 |
| `eval` | `eval run <agent>` / `--skill`, `eval results` | `run` costs | 2 |
| `spec` | `spec list`, `review`, `verify`, `implement` | `review` and `verify` cost | 2 |
| `job` | `job list`, `job create` (today's `create_job`, `list_jobs`) | `create` changes | 2 |
| `workflow` | `workflow list`, `save`, `run` | `save` changes; `run` asks as its steps do | 3 |
| `agent`, `skill` | `list`, `save` | `save` changes | 3 |

## 4. Connections (phase 1)

- **DRV-10** `connector list` answers:
  - the **categories** and their **kinds**, each kind with its fields (name, label, help, whether required, a path or a secret), whether it can be read-only, and its ready-made tools;
  - the **connections**: name, kind, scope (workspace or global), on here or not, trusted or not, read-only or not, its queries' names, and its problem if it has one.

  It's what the agent reads first, so it never guesses a kind's fields.
- **DRV-11** `connector save` takes `kind`, `name`, `description`, `settings` (the kind's fields; a secret field as `${NAME}`), `read_only`, and `queries` (name, description, statement, parameters, as Studio's **Your queries**). Before asking, it checks the connection as Studio's form does (`Workspace.SaveConnector`'s checks: the kind, the fields, a path that names no file, a name already taken by another connection): a problem comes back to the agent with nothing asked or written. Then the approval shows:
  - the file as it will be written, its folder (`.agents/connectors/<category>/`) and whether it replaces one;
  - read-only or not, and its queries, each marked if it may write;
  - a password field for each `${NAME}` that isn't set.

  Approved, it's saved as Studio saves it, **trusted** (the person approved exactly this file, as **Save and trust**; CON-09; §11, decision 1), on here, and then tested (CON-04 test). The result says it was saved, its tools, and the test's problem if there is one. Declined, nothing is written.
- **DRV-12** Changing a connection is `connector save` on its name. The approval shows the change as a diff of the file. A global connection's name is refused (`it's a global connection: Settings changes it`).
- **DRV-13** `connector enable` and `connector disable` turn a connection (a global one too) on or off in this workspace (CON-17), asked as a change.
- **DRV-14** No delete in phase 1 (§11, decision 3). The agent says where to do it (Studio › Connectors).
- **DRV-15** With a connection saved, `@db:<name>` works in the next message and the agents' `db__` tools are there from the next turn, as after a save in Studio. The result says so, so the agent can tell the person.

## 5. The blitz skill

- **DRV-20** A built-in skill, **blitz** (`pkg/engine/skills/builtin/blitz/SKILL.md`; a workspace's own skill of that name is used instead), whose description is what makes the agent load it: using or setting up Blitz's own features. Loaded only then, so other turns don't carry it.
- **DRV-21** It says, briefly:
  - what each feature is, in a line, and its command group;
  - that `blitz help <command>` gives the arguments, and that the commands ask the person, so the agent shouldn't ask first as well;
  - that secrets go as names and the form asks for values;
  - that Blitz's own files, the shell's `blitz` and the binary aren't the way (DRV-08);
  - for a new SQLite database: create the file in the workspace, then `connector save` with a relative path (DB-17), so it works wherever the workspace is cloned;
  - what to tell the person when done (`@db:<name>`, Studio › Connectors).
- **DRV-22** A test reads the skill and fails when a command it names isn't in the catalog.

## 6. Pictures, the browser and Studio (phase 2)

- **DRV-30** `picture` takes a prompt and, optionally, a model, references (workspace files) and a shape. In the window it opens **Pictures** with them filled in (spec_058_pictures), and the person makes it there: the cost shows before they press make, as always. The result tells the agent it's open, not a picture. In the terminal it's `blitz picture`, its cost asked first.
- **DRV-31** `open url` opens the window's browser at the address, asked first as a fetch of that host is (web.allow_domains, the rules); `open file` opens a workspace file in its editor; `open studio <section>` opens Studio there (Connectors, Workflows, Specs). In the terminal, `open url` opens the system browser after asking, and the others say where it is.
- **DRV-32** A window action goes to the conversation's window as an event of the turn's stream; with no window open (the terminal, a job), the command says so and does the terminal's part.

## 7. Evals, specs and jobs (phase 2)

- **DRV-40** `eval run` runs an agent's or a skill's evals as `blitz eval` does, after its estimate is approved, as a process the person sees (spec_052_processes), never inside the turn's reply. The result says it started and where to see it; `eval results` reads the last run's.
- **DRV-41** `spec` runs `blitz spec`'s commands (spec_057_spec_driven): `review` and `verify` show their cost first, `implement` answers with the prompt it would give.
- **DRV-42** `job create` and `job list` are today's `create_job` and `list_jobs`, which stay as names for the commands so existing agents keep working.

## 8. Workflows, agents and skills (phase 3)

- **DRV-50** `workflow save`: the workflow as its file (spec_043_workflows), checked with the workflow's checks (WF-02) before asking. The approval shows the steps in order, each with what it does. A Data step (spec_059_data) shows Allow writes and the BigQuery cost limit when set. `workflow run` runs it as Studio's Run does, its steps asking as they do.
- **DRV-51** `agent save` and `skill save`: the workspace's agent or skill as Studio writes it, the approval showing the file (the instructions rendered). An agent created or changed this way has its evals run as Studio runs them (offline, after the change, never in the turn).

## 9. Tests

- **DRV-60** The catalog: every command has an engine command, a CLI command of the same name and arguments (DRV-02), and a `help` entry; the tool's description names every group.
- **DRV-61** Connections: `connector list` against the built-in descriptors and workspace and global files; `connector save`'s checks (a missing field, a path to nothing, a taken name, a global name) with nothing asked; approved (written, trusted, on, tested, `db__` tools there), declined (nothing written), a missing secret asked and saved, and the secret's value absent from the result, the transcript and the audit.
- **DRV-62** A journey test (the scripted model, as in the other journeys): the owner's recipe prompt makes the database, calls `connector list` then `connector save`, one approval, and the next message's `@db:recipes` reads it, with no shell command reading Blitz's files or binary. Phase 2 adds `picture` opening Pictures in the fake window and `open url` asking.
- **DRV-63** A live check, after the owner agrees its cost: the recipe prompt in the desktop app with Gemini, which should finish in under two minutes with one approval for the connection.

## 10. Not in this spec

- **Blitz as an MCP server** for agents outside it (Claude Code, Cursor): the catalog's commands as MCP tools, signed in, with Blitz's approvals. The catalog makes it a small step later; it belongs beside the hosted service (standard protocols). For Blitz's own agent it would only add a connection back into the same process, and MCP's approval rules in place of Blitz's forms.
- Auto mode's reviewer taking up to two minutes per command (§1). The tool avoids it for Blitz's commands, but the wait is its own problem.

## 11. Decisions

The owner's answers (2026-11-07):

1. A connection approved in the form is **trusted**, as **Save and trust**: **yes**.
2. Always offered, and at what cost: asked. The answer, with Gemini 4 Argon's prices ($2 a million input tokens, $0.10 cached):
   - Phase 1's connection tools as separate tools would be about 900 tokens a model call: $0.0018 the first call, $0.00009 a call once cached, under half a cent for a 20-call turn.
   - The whole catalog as separate tools (about 30) would be about 9,000 tokens a call, about 4 cents for that turn, beside the 93 tools the agent can have today, which also makes picking the right one harder.
   - One `blitz` tool (DRV-01) is about 300 tokens a call however many commands it has, with `help` for the rest: **recommended**, always offered.
3. Deleting a connection by tool: **not in phase 1**.
4. Blitz's other functions callable by the agent too (evals, a picture opening Pictures, a browser opened at an address), most of them through the CLI as well: **yes** (the owner, 2026-11-07), as §3's catalog and DRV-02.
