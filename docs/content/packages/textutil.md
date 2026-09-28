---
title: "textutil"
weight: 13
---

Small string helpers. Source: [`pkg/textutil`](https://github.com/retail-cortex/blitz/tree/main/pkg/textutil).

`pkg/textutil` holds string helpers shared by the tools and the REPL: truncating without splitting a UTF-8 character, ellipsizing, turning globs into regular expressions, and removing terminal control sequences from text that will be printed.

## API

| Name | |
|---|---|
| `TruncateUTF8`, `TrimPartialRune`, `Ellipsize` | Cutting text safely |
| `GlobToRegex` | Globs for rules and matches |
| `SanitizeTerminal` | Strip escape sequences from untrusted output |

## Used by

apps/cli/internal/tui, pkg/engine, pkg/engine/memory, pkg/engine/tools.
