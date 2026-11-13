---
title: Blitz 0.9.9
weight: 5
---

If you use Gemini through your Google Cloud account, update now: it works again. Teams also get their organization's rules and shared workspaces without the restarts and errors.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` if you installed it with apt, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.9).
2. Open a conversation with Gemini and carry on.

### Fixed

- **Gemini through Google Cloud answers again.** Since 0.9.7, every conversation with Gemini on Vertex AI (signed in with Google Cloud rather than an API key) stopped with an error. It works now. Gemini with an API key was never affected.
- **Your organization's rules arrive on their own.** You no longer have to restart Blitz when it says it can't read your organization's policy yet. It keeps checking until the rules arrive.
- **Share a workspace even if you sign your commits.** Sharing with your organization failed for anyone with Git commit signing turned on. It works now, and your own commits are still signed.

### For organization admins

- A new organization's people can use its models right away, before you publish anything to the catalog. This is already live on the service, with nothing to install.
