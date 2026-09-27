---
description: Build, lint and test the project and report what fails
argument-hint: "[what to verify]"
---
Verify the project works. Focus: $ARGUMENTS

Find how this project is built, linted and tested (BLITZ.md, AGENTS.md, the Makefile, package.json, go.mod, pyproject.toml, CI workflows) and run those checks, starting with the ones covering the recent changes (`git diff HEAD --name-only`). Report each check with its result. For each failure, show the relevant output and say whether it looks caused by the recent changes. Don't fix anything unless asked; suggest the fix instead.
