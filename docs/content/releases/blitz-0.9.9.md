---
title: Blitz 0.9.9
weight: 2
---

Gemini on Google Cloud answers again, and Blitz is easier to start with an organization.

### Get started

1. Update Blitz (it updates itself), run `sudo apt upgrade` if you installed the `.deb` from 0.9.8, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.9).
2. Ask Gemini something. That's it.

### Fixes

- **Gemini on Google Cloud works again.** If Blitz reaches Gemini with your Google Cloud sign-in rather than a Gemini key (that's Vertex AI), 0.9.7 and 0.9.8 stopped every conversation with "agent execution error: includeServerSideToolInvocations parameter is only supported in Gemini Developer API mode". Blitz now sends that setting only to the Gemini API, where it belongs. Gemini with a key wasn't affected.
- **Your organization's rules arrive as soon as they're there.** Blitz could keep saying "your organization's policy can't be read (not fetched yet)" until it was restarted. It now checks again on its own, every 30 seconds until it has the rules, then at your organization's refresh.
- **Sharing a workspace with your organization works when you sign your Git commits.** It failed with "gpg failed to sign the data … Inappropriate ioctl for device". Blitz's own commits for the organization aren't signed now; yours still are.

### Good to know

- For organizations: a new organization's people can use its models straight away, before anything is published to its catalog. That's the organization's service, already updated on Retail Cortex's side; nothing to install.
