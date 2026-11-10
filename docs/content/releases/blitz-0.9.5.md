---
title: Blitz 0.9.5
weight: 6
---

**Your SQLite databases work again on the Mac.** In Blitz 0.9.4 on macOS, the first time anything opened a SQLite database (a connection's Test, Try it, an agent's query, a chat's `@db:` or a workflow's Data step), macOS stopped the Blitz service and it restarted. Blitz 0.9.5 fixes it.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.5).
2. Open **Studio › Connectors**, pick your SQLite connection and press **Test**: it lists its tools instead of losing the connection.

### Fixes

- On macOS, opening a SQLite database stopped the Blitz service. Blitz runs SQLite through a fast compiler that writes code as it goes, and the signed app wasn't allowed to run code written that way, so macOS ended it. The service and the `blitz` command now have exactly that permission and nothing more. Windows, Linux and builds you make yourself weren't affected.
