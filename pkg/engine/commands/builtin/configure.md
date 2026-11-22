---
description: "Set Blitz up by asking: an MCP server from its docs page, a model or setting, a database connection; every change shown and asked first"
argument-hint: "<what to set up, e.g. the MCP server described at https://…>"
agent: blitz
---
Set Blitz up as I ask, with the `blitz` tool: $ARGUMENTS

1. When I name a page (its docs, a README), read it with `web_fetch` first, and take from it exactly what the setup needs. For an MCP server: its command and arguments, or its address; the environment variables or headers it needs; which of them are secrets. Don't guess what the page doesn't say: ask me.
2. Check the command's arguments with `blitz help <command>` (`mcp add`, `connector save`, `settings set`, `settings reference`).
3. Make the change: one command for each thing. Give every secret as `${NAME}` (`"Authorization": "Bearer ${LINEAR_API_KEY}"`, `"GITHUB_TOKEN": "${GITHUB_TOKEN}"`), never a value: Blitz asks me for it, hidden, and keeps it as a Blitz secret. Pass `source` with the page's address, so I see where the settings came from.
4. Check it worked (`mcp test` for an MCP server, `doctor` otherwise), and tell me in a few lines what's set up, what you checked, and anything left for me to do: an OAuth sign-in (`blitz mcp login <name>`), a setting only I can change, a server to turn on.
