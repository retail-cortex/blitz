---
title: Blitz 1.0.0
weight: 1
---

Talk to Blitz. Click the microphone in the chat bar, say what you want done, and your agent gets to work as if you'd typed it; when it's finished, Blitz tells you what changed and what's waiting for you. And Blitz now has a personality of your choosing: a patient mentor, a straight talker, a calm hand at 2 a.m., or one you write yourself.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.0.0).
2. Pick a personality in **Settings › Personality**. **Try it** shows how it answers before you choose.
3. To talk, set up Gemini in **Settings › Providers** (an API key or Vertex AI), turn on **Settings › Voice**, then click the microphone in the chat bar or press ⌥⌘V.

### New

- **Voice, on your terms.** Talk to Blitz with Google's Gemini Live. The voice hands real work to your agent, so approvals, the sandbox and Changes work exactly as when you type. Ask "what's it doing?" while it works, steer it, or stop it. When the agent wants approval, the voice asks; a clear "yes" allows it once. When the work ends you hear a sentence or two: what changed, how the tests went, what's waiting. Voice is off until you turn it on, and completely gone when it's off. [More about voice](https://retail-cortex.github.io/blitz/guide/voice/).
- **Personalities.** Choose how Blitz sounds: friendly, direct, mentor, cheerful, calm or reviewer, or write your own in Settings. Set your default, choose one per workspace, or switch with `/personality`. A personality changes the tone only, never what the agent may do, and the agents it hands work to stay neutral. Each personality has a voice to match.
- **Hear your work finish.** With spoken summaries set to Always, Blitz says how a long task went even when you typed it.

### Good to know

- Voice is in the desktop app and uses Google's Gemini only, whatever model your agent runs on. While you talk, your microphone's audio goes to Google; Blitz keeps the words, never the audio. Audio is charged by Gemini like any model.
- The first time you talk, macOS asks whether Blitz may use the microphone.
