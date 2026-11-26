---
title: Voice
weight: 42
---

Specs: [voice](../about/specs/spec_063_voice.md), [personality](../about/specs/spec_064_personality.md).

Talk to Blitz in the desktop app. Say what you want done, and the voice hands it to your agent as if you'd typed it; when the work ends, it tells you in a sentence or two what changed, how the checks went and what's waiting for you. Voice uses Google's Gemini Live, is off until you turn it on, and is completely gone when it's off.

## Get started

1. Set up Gemini in **Settings › Providers**, with an API key or Vertex AI. Voice uses Gemini even when your agent runs on another provider.
2. Turn on **Settings › Personality › Voice**.
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

## Settings › Personality › Voice

| Setting | What it does |
|---|---|
| **Voice** | On or off. Off, there's no microphone, no shortcut, nothing sent to Google, and the microphone is never used. |
| **Model** | The Gemini Live model. Blitz lists the live models your provider offers, checks which ones your key or Google Cloud project can actually use, and offers only those; the default is the newest of them. If none answers, it says why here. |
| **Voice engine** | **Automatic** (Google Cloud when your personality sets a locale, speed or pitch and Google Cloud is set up, Gemini Live otherwise), **Gemini Live** (one model hears you and answers in its own voice: the quickest) or **Google Cloud** (Speech-to-Text hears you, a text model answers, and Text-to-Speech says it, in your personality's locale, at its speed and pitch). See [A voice of your own](#a-voice-of-your-own). |
| **Voice** | One of Gemini's voices, with **Play** to hear a sample. A [personality](agents.md#personality) can bring its own voice. |
| **Microphone** | Which microphone, with a level meter. This one is kept by the window, not the settings file. |
| **Listening** | **Tap to talk** (click to start, click to stop; Gemini hears when you start and stop speaking) or **Hold to talk** (Blitz hears you only while you hold the microphone or ⌥⌘V: for noisy rooms). |
| **Spoken summaries** | When work is summarised aloud: in voice conversations, always, or never. |
| **End after** | Minutes of quiet before a conversation ends by itself. |
| **The room** | **Quiet** or **Noisy**. In a noisy room, Blitz waits for clearer speech before taking a sound for you talking, so background chatter doesn't interrupt it. (Tap to talk.) |
| **Pause before Blitz answers** | How long a silence ends your turn, in milliseconds: longer lets you stop to think without an early answer. Empty is Gemini's own. (Tap to talk.) |
| **Speech that starts your turn** | How much speech counts as you starting to talk: longer ignores a cough or a click. Empty is Gemini's own. (Tap to talk.) |
| **Playback buffer** | How much of a reply is held before it plays: more smooths choppy sound on a slow or uneven connection, with a little more delay. |

They're the `[voice]` table of your settings file (`~/.blitz/.env.toml`), which a workspace's settings can't change:

```toml
[voice]
enabled = true
model = "gemini/<a live model>"   # optional: one Settings › Voice lists
voice = "Puck"                                                   # optional
listen = "tap"        # or "hold"
summaries = "voice"   # or "always", "off"
noise = "normal"      # or "noisy"
pause_ms = 0          # 200-5000; 0: Gemini's own
start_ms = 0          # 20-2000; 0: Gemini's own
buffer_ms = 20        # 20-2000
idle_minutes = 5
```

## A voice of your own

A [personality](agents.md#personality) can tune its voice, in Settings › Personality, when you edit one of your own (duplicate a built-in to start from it):

- **Locale**: the language and accent it speaks, such as English (United Kingdom) or French (Canada).
- **Speed**: a slider from 0.50× to 2.00×; 1.00× is the voice's own pace.
- **Pitch**: a slider from −6 to +6 semitones. A semitone or two goes a long way.
- **Play** says a short sample as it's tuned, before you save.

These are spoken by the **Google Cloud** voice engine, in conversations, spoken summaries and samples alike. With **Voice engine** left on **Automatic** (Settings › Voice), Blitz uses it by itself for a personality that sets any of them, once Google Cloud is set up. Gemini Live speaks only in its own voice: with the engine set to Gemini Live, or Google Cloud not set up, they're kept but not heard.

The Google Cloud engine needs Google Cloud's own sign-in, not a Gemini API key:

1. Sign in on the machine running Blitz: `gcloud auth application-default login`.
2. In that Google Cloud project, turn on the **Cloud Text-to-Speech API** and the **Cloud Speech-to-Text API**.
3. If Blitz can't tell which project to use, set `[llm.gemini] project_id` or `GOOGLE_CLOUD_PROJECT`.

Settings › Voice says what's missing, and which project speech is made in. There you also choose **The language you speak** (what Speech-to-Text listens for; your personality's locale unless you say otherwise) and the **Model** that writes what's said (your workspace's small model unless you name one).

It answers a little more slowly than Gemini Live, since three services take their turn. Speaking over it stops it, as with Gemini Live.


## Privacy and cost

While a conversation is open, your microphone's audio goes to Google's Gemini Live API through the provider you set up (your API key, or your Vertex AI project), and nowhere else. Blitz never saves audio; it keeps the words, as the conversation's spoken lines. Gemini charges for the audio as for any model, so voice isn't free. See [Privacy](privacy.md).

With the **Google Cloud** voice engine, the audio goes to Google Cloud's Speech-to-Text in your own project instead, the words it hears go, as text, to the model you chose for voice (your workspace's small model unless you name one, of whichever provider that is), and what that model writes goes to Google Cloud's Text-to-Speech. Google Cloud charges for the minutes it listens (while the microphone is open with tap to talk, silence included; hold to talk sends only what you hold for) and the characters it speaks, and the model for its tokens.
