---
description: Review the uncommitted changes (or the given branch or files) for bugs
argument-hint: "[branch | files]"
mode: plan
---
Review code changes for correctness. Target: $ARGUMENTS

If no target is given, review the working tree's uncommitted changes (`git diff HEAD`, plus untracked files). If a branch is given, review what it adds over the default branch (`git diff <default>...<branch>`); if files are given, review them.

For each real problem, give the file and line, what goes wrong and when (a concrete input or state), and how to fix it. Look for: logic errors, wrong edge cases, error handling that loses or hides errors, concurrency and resource leaks, broken invariants between changed and unchanged code, and missing or wrong tests. Check that each finding is real by reading the surrounding code before reporting it. Skip style nits and speculation. End with a one-line verdict. Don't change any files.
