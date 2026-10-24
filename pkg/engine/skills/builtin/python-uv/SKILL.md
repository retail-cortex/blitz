---
name: python-uv
description: Start or change a Python project with uv, asking first which layout it needs (app, packaged app, library, single script or workspace) instead of assuming a flat project
tags: [python, uv, packaging, project-setup]
version: "1.0.0"
author: "Blitz"
category: development
agents: [blitz]
trigger_phrases:
  - new python project
  - start a uv project
  - uv init
  - set up a python package
  - create a python library
  - python cli with uv
  - python monorepo with uv workspaces
---
# Python projects with uv

uv makes very different projects from the same command, and the layout is hard to change once code and imports depend on it. **Never assume a simple project: ask first.**

## 1. Ask before running anything

Look first: is there a `pyproject.toml`, `uv.lock`, `.python-version`, `src/`, or a `[tool.uv.workspace]` table already? Is the folder in a git repository? If the project exists, follow its layout and skip to step 3.

For a new project, ask with **one** `ask_user_question`, the layout the request suggests first, marked recommended:

| Option | When | Command |
|---|---|---|
| **App** | scripts, a service, a notebook-style project that's run, not installed | `uv init --app` |
| **Packaged app** | a command-line tool or service to install or publish, with an entry point | `uv init --package` |
| **Library** | code other projects import, published to PyPI or shared | `uv init --lib` |
| **Single script** | one file with its own dependencies, no project | `uv init --script <name>.py` |
| **Workspace** | several packages developed together (an app and its libraries, a monorepo) | `uv init` at the root, then `uv init packages/<name>` per member |

In the same question (or a second one, never more), ask what the request leaves open:

- **Python version**: offer the `.python-version` already there, else the newest stable one `uv python list` shows, and the one the user's other projects use if you know it.
- **Tools**: pytest for tests, ruff for linting and formatting (both offered by default), mypy or pyright for types.
- **Name**: when the folder's name isn't a valid package name (lowercase, `_` not `-`).

Unattended (no one to ask), choose **App** for "a script"/"a project", **Packaged app** for "a CLI"/"a tool", **Library** for "a library"/"a package", and say which you chose and why in your summary.

## 2. Make it

Run in the workspace (`uv init` makes the folder's project; `uv init <dir>` a new folder):

```sh
uv init --package --python 3.13          # the layout chosen above
uv add --dev pytest ruff                  # the tools chosen
uv sync                                   # creates .venv and uv.lock
```

What each layout gives you:

- **App:** `pyproject.toml` without a build system, `main.py`, `.python-version`, `README.md`. Run with `uv run main.py`.
- **Packaged app:** `src/<package>/__init__.py` with a `main()`, `[project.scripts] <name> = "<package>:main"`, and a build system (`uv_build`). Run with `uv run <name>`.
- **Library:** `src/<package>/__init__.py` and `py.typed`, a build system, no entry point. Test with `uv run pytest`.
- **Single script:** the dependencies inline at the top of the file (`# /// script`), added with `uv add --script <file> <dep>`. Run with `uv run <file>`.
- **Workspace:** the root's `pyproject.toml` lists members under `[tool.uv.workspace] members = ["packages/*"]` (`uv init packages/<name>` inside the root adds them); one `uv.lock` for all; members depend on each other with `[tool.uv.sources] <name> = { workspace = true }`. Run a member's command with `uv run --package <name> …`.

Put tests in `tests/` (`tests/test_<module>.py`), and settings for the tools in `pyproject.toml` (`[tool.ruff]`, `[tool.pytest.ini_options]`), not in separate files.

## 3. Work in it

- Add dependencies with `uv add <dep>` (`--dev` for tools, `--optional <group>` for extras), never `pip install`: uv keeps `pyproject.toml` and `uv.lock` in step.
- Run everything through `uv run` (`uv run pytest`, `uv run ruff check`, `uv run python -m <module>`), so it uses the project's environment without activating it.
- Commit `pyproject.toml`, `uv.lock` and `.python-version`; never `.venv/` (uv's `.gitignore` already leaves it out).
- Change the Python version with `uv python pin <version>`, then `uv sync`.

## 4. Check it

Before saying it's done: `uv sync` succeeds, `uv run pytest` passes (a first test that imports the package is enough), and `uv run ruff check` is clean. Say which layout you made, its commands (how to run it, test it, add a dependency), and what's left.
