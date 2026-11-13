---
title: Voice
weight: 42
---

Specs: [voice](../about/specs/spec_063_voice.md), [personality](../about/specs/spec_064_personality.md).

Talk to Blitz in the desktop app. Say what you want done, and the voice hands it to your agent as if you'd typed it; when the work ends, it tells you in a sentence or two what changed, how the checks went and what's waiting for you. Voice uses Google's Gemini Live, is off until you turn it on, and is completely gone when it's off.

## Get started

1. Set up Gemini in **Settings › Providers**, with an API key or Vertex AI. Voice uses Gemini even when your agent runs on another provider.
2. Turn on **Settings › Voice**.
3. Click the microphone in the chat bar (or press ⌥⌘V) and talk. The first time, macOS asks whether Blitz may use the microphone.

## Talking

- **Ask for work.** "Run the tests and fix whatever fails." The voice writes your request out as a clear message and sends it to the agent, which works on it as usual. The message shows in the conversation, approvals and the sandbox apply, and changes show in **Changes**.
- **Steer or stop.** While the agent works, what you say adds to its task ("skip the slow ones"); "stop" stops it.
- **Ask where things stand.** "What's it doing?", "What did it change?" The voice reads Blitz's status: it can't see your files itself, and says so rather than guess.
- **Approvals.** When the agent asks to run a command or change a file, the voice says what it wants. A clear "yes" allows it **once**, and only that request, while its card is showing; "no" denies it. To allow something for the session or always, click.
- **Questions.** The voice reads the agent's question and its options; your answer goes back as if you'd picked it.
- **Interrupt.** Speak while the voice is talking and it stops to listen.
- **Finish.** Click the microphone again, press ⌥⌘V, or say "stop listening". A conversation also ends after a few quiet minutes with nothing running.

While the microphone is open, the bar above the chat box shows what's heard and said, with **Mute** and **Stop talking**, and the status bar shows a red **Listening** that ends it from any view. What you said and what the voice answered stay in the conversation, in writing.

## Settings › Voice

| Setting | What it does |
|---|---|
| **Voice** | On or off. Off, there's no microphone, no shortcut, nothing sent to Google, and the microphone is never used. |
| **Model** | The Gemini Live model. The default is the newest native-audio model your provider lists. |
| **Voice** | One of Gemini's voices, with **Play** to hear a sample. A [personality](agents.md#personality) can bring its own voice. |
| **Microphone** | Which microphone, with a level meter. This one is kept by the window, not the settings file. |
| **Listening** | **Tap to talk** (click to start, click to stop; Gemini hears when you start and stop speaking) or **Hold to talk** (Blitz hears you only while you hold the microphone or ⌥⌘V: for noisy rooms). |
| **Spoken summaries** | When work is summarised aloud: in voice conversations, always, or never. |
| **End after** | Minutes of quiet before a conversation ends by itself. |

They're the `[voice]` table of your settings file (`~/.blitz/.env.toml`), which a workspace's settings can't change:

```toml
[voice]
enabled = true
model = "gemini/gemini-2.5-flash-native-audio-preview-09-2025"  # optional
voice = "Puck"                                                   # optional
listen = "tap"        # or "hold"
summaries = "voice"   # or "always", "off"
idle_minutes = 5
```

## Privacy and cost

While a conversation is open, your microphone's audio goes to Google's Gemini Live API through the provider you set up (your API key, or your Vertex AI project), and nowhere else. Blitz never saves audio; it keeps the words, as the conversation's spoken lines. Gemini charges for the audio as for any model, so voice isn't free. See [Privacy](privacy.md).
