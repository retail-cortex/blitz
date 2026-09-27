---
description: Simplify the recent changes (or the given files) without changing behaviour
argument-hint: "[files]"
---
Simplify code without changing what it does. Target: $ARGUMENTS

If no target is given, work on the files changed in the working tree (`git diff HEAD --name-only`). Remove duplication, dead code and needless indirection; replace hand-written logic with existing helpers in this codebase or its standard library; make names and control flow clearer; keep the file's existing style. Don't change public behaviour, interfaces or tests' expectations. Make the changes, then run the relevant tests and report what you changed and why.
