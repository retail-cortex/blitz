---
name: python
description: Write and change Python code idiomatically (typing, errors, structure, pytest, ruff), following the project's own tools and conventions first; new projects use python-uv
tags: [python, development]
version: "1.0.0"
author: "Blitz"
category: development
agents: [blitz, coder]
trigger_phrases:
  - write this in python
  - change the python code
  - fix the python tests
  - add a python module
---
# Python

## 1. Look first

Read `pyproject.toml` (the Python version, the dependencies, `[tool.*]` settings), the lock file (`uv.lock`, `poetry.lock`, `requirements*.txt`) and the module you're changing. Use the project's tools: with `uv.lock`, run everything through `uv run`; with Poetry, `poetry run`; otherwise the virtual environment it documents. Never `pip install` into the system Python. Starting a new project: activate `python-uv` instead.

## 2. Write it

- **Types:** annotate public functions and anything non-obvious, in the syntax of the project's version (`list[str]`, `X | None` from 3.10). Prefer `dataclasses` (or the project's pydantic/attrs) to dicts that carry structure.
- **Structure:** modules by what they provide; no logic at import time; an entry point behind `if __name__ == "__main__":` or `[project.scripts]`. Keep functions small and pure where you can; pass dependencies in rather than reaching for globals.
- **Errors:** raise specific exceptions (built-in or the package's own, subclassing one base); catch only what you can handle, never a bare `except:`; chain with `raise … from err`. Use `with` for files, locks and connections.
- **Idiom:** comprehensions over `map`/`filter` with lambdas; `pathlib.Path` over `os.path`; f-strings; `enumerate` and `zip` over index arithmetic; `logging` (with the module's logger) over `print` in library code.
- **Async:** only where the project already is; never block the event loop with sync I/O inside `async def`.
- **Data work:** in notebooks and pandas code, keep cells re-runnable from the top, vectorise rather than loop over rows, and pin the random seed where results matter.

## 3. Test it

Tests in `tests/` (`test_<module>.py`) with pytest: plain `assert`, fixtures for setup, `@pytest.mark.parametrize` for cases, `tmp_path` and `monkeypatch` rather than touching real files or the environment.

## 4. Check it

Before saying it's done: the tests pass (`uv run pytest`, or the project's command), the linter and formatter are clean (`ruff check` and `ruff format --check`, or what the project uses), and the type checker passes if the project has one (mypy, pyright). Say what you ran.
