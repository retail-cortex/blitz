---
title: "063 · Voice"
weight: 63
---

*Talk to Blitz. A microphone in the chat bar opens a live voice conversation with Gemini: you say what you want, the voice hands the work to the workspace's agent, and when the work is done it tells you, in a sentence or two, what changed and what's waiting for you. Desktop only, Google only, off until you turn it on, and completely gone when it's off.*

| | |
|---|---|
| Status | **Built** (2026-11-13, for v1.0.0; not yet: the tray's **Stop talking** (VOX-23), an organization switching voice off (VOX-52, VOX-90), voice's tokens in the session's cost (VOX-18), the 1007 "audio content type not supported" seen once in the owner's first test, not reproduced. The owner: "a voice interface using the bidi voice access with ADK. A microphone icon in the chat bar, and voice summaries of the work", then "Don't forget to add the voice settings in the UI, not just the file, and it should be able to be switched completely off. This is a desktop only feature and I'm happy to make it a Google only feature.") |
| Source | `pkg/engine/voice.go` (the conversation, the voice agent and its tools, the turns' facts, summaries); `pkg/engine/runtime/voice.go` (the Live model, its settings); `pkg/engine/runtime/wirefake/live.go` (the Live API's fake); `pkg/config` (`VoiceConfig`, `[voice]`); `VoiceService` (`proto/blitz/v1/voice.proto`, `pkg/service/server/voice.go`); the page's `apps/desktop/web/src/voice/` (`voice.ts`, `audio.ts`, `useVoice.ts`, `VoiceControls.tsx`, `VoiceSettings.tsx`), the composer and conversation (`Conversation.tsx`), the status bar, the command palette, Settings (`SettingsDrawer.tsx`, `simpleMode.ts`); Electron's microphone permission (`apps/electron/src/logic.ts` `pagePermission`, `main.ts`), `packaging/entitlements.plist`, the app's `Info.plist` (`packaging/mac.bzl`) |
| Depends on | [spec_015_models](spec_015_models.md) (the Gemini provider, its API key or Vertex AI), [spec_021_service](spec_021_service.md) (the API, SVC-30's reasons), [spec_024_desktop](spec_024_desktop.md) (the composer, Settings), [spec_061_models](spec_061_models.md) (the models each provider lists), [spec_064_personality](spec_064_personality.md) (how the voice sounds) |

## 1. Purpose

Some work is easier said than typed: "run the tests and fix whatever broke", "what's left on the payments spec?", "yes, go ahead". And a long run is easier to follow by ear than by watching the window: Blitz can say "Done: three files changed, all 212 tests pass, nothing waiting on you" while you look at something else.

Voice is a way of driving the agent you already have, not a second agent. The voice model listens, answers what it can from what Blitz tells it, and passes anything that needs work to the session's agent as an ordinary turn. So the sandbox, approvals, permission rules, guardrails, limits and Changes all apply exactly as they do for a typed message, and the conversation keeps a written record of everything said.

**Scope.** The desktop app only; the CLI and the VS Code extension have no voice. Google's Gemini Live models only (through the Gemini API with a key, or Vertex AI), whatever model the session's agent uses.

## 2. Switched on, switched off

- **VOX-01** **Off by default.** `[voice] enabled` is `false` until the person turns it on, in Settings › Voice (VOX-40) or the settings file.
- **VOX-02** **Off means gone.** With voice off:
  - the chat bar has no microphone, the status bar no voice indicator, the command palette and menus no voice commands, and Settings › Voice shows only its switch and what voice does;
  - the page never asks for the microphone, so the operating system never asks the person either (VOX-50);
  - the service refuses the voice socket (`VOICE_OFF`, VOX-31) and opens no live connection, speaks no summary and sends Google nothing for voice;
  - nothing of voice runs in the service: no voice agent is built and no voice tools are registered.

  Turning it off while a voice conversation is open ends it at once (VOX-14).
- **VOX-03** **On, but not set up.** Voice needs a Google provider (VOX-05). With voice on and none set up, the microphone shows, disabled, and its tooltip and Settings › Voice say what's missing, with a button to **Set up Google** (Settings › Providers, on Gemini).
- **VOX-04** **The settings.** `[voice]` in `~/.blitz/.env.toml` (the person's; a workspace's settings can't turn voice on, only the person can, so a repository can't switch on someone's microphone):

  | Key | Default | Meaning |
  |---|---|---|
  | `enabled` | `false` | Voice on or off (VOX-01, VOX-02). |
  | `model` | `""` | The Gemini Live model, as `gemini/<model>`. Empty is VOX-06's default. |
  | `voice` | `"Kore"` | One of Gemini's prebuilt voices (Puck, Charon, Kore, Fenrir, Aoede, Leda, Orus, Zephyr…, as the model offers them). The session's personality's voice wins when it has one (spec_064 PER-40). |
  | `listen` | `"tap"` | `tap`: click the microphone to start, click again to end; Gemini hears when you start and stop speaking. `hold`: speak only while holding the microphone or its shortcut (VOX-21), for noisy rooms. |
  | `summaries` | `"voice"` | When work is summarised aloud (VOX-17): `voice`, only in a voice conversation; `always`, also when a turn the person typed ends; `off`, never. |
  | `idle_minutes` | `5` | A voice conversation with nothing said and nothing running for this long ends by itself (VOX-14). 1–60. |

  The microphone device is the window's own (`~/.blitz/desktop.json`), not a setting in the file: device IDs belong to one computer. Each key can be set with `SetConfigValue` (`voice.enabled`, `voice.model`, `voice.voice`, `voice.listen`, `voice.summaries`, `voice.idle_minutes`); bad values are refused with what's allowed. The CLI's `blitz config` template lists `[voice]`, commented out, saying it's for the desktop app.

## 3. The model

- **VOX-05** **Google only.** The voice is a Gemini Live model reached through Blitz's `gemini` provider (spec_015), with an API key or Vertex AI (application default credentials or OAuth, with the provider's project and location). No other provider, gateway or local model is offered for voice. The session's agent, the one doing the work, can be on any provider.
- **VOX-06** **The default model.** With `model` empty, the newest model the provider lists for live, two-way audio (`bidiGenerateContent` among its supported actions; spec_061's list, kept 10 minutes) whose name says native audio, else any live one. When the provider lists none (Vertex AI lists no actions), the default is the one Blitz's model facts name for live voice. A `model` that isn't live-capable is refused when it's set (`VOICE_MODEL`).
- **VOX-07** **Through ADK's live runner.** The service runs the voice agent with ADK's `Runner.RunLive` (`google.golang.org/adk/v2`, bidirectional streaming), which connects through genai's Live API. Audio is 16-bit mono PCM: 16 kHz in, 24 kHz out. The connection asks for:
  - audio responses in the configured voice (`speechConfig`);
  - transcripts of both sides (`inputAudioTranscription`, `outputAudioTranscription`), for the conversation's record (VOX-16);
  - Gemini's own activity detection with `listen = "tap"`, and explicit activity start/end messages with `listen = "hold"`;
  - context window compression and session resumption, so a conversation outlasts Gemini's session limit and survives a dropped connection: Blitz keeps the latest resumption handle and reconnects with it, quietly, before the server's `goAway` deadline.
- **VOX-08** **The reply language** is the language the person speaks in (the voice's instructions say so). The session's reply language (spec_024's run settings) isn't passed to it yet.
## 4. The voice agent

- **VOX-10** **Who it is.** A built-in agent, `voice`, with its own short instructions: it's the person's spoken link to the workspace's agent; it keeps replies short (two sentences unless asked for more); it never claims to have done something it didn't; it hands anything that needs reading files, running commands or changing things to the agent with `run_task`. It knows the workspace's name, the session's agent and model, and what's running. It speaks with the session's personality (spec_064 PER-40), in its frame (PER-03). It's not listed among the agents the person can pick and can't be edited in Studio; its instructions live in `pkg/engine/voice.go`.
  *2026-11-13 (the owner: "all communication from the agent should be first person, like \"I'm looking into the details …\"")* It speaks as Blitz, always in the first person ("I'm looking into that now", "I found…", "I need your OK to…"), and never mentions an agent, a subagent, a tool or a hand-off: the work is its own. Blitz's notes to it ([Blitz] …) are written the same way.
- **VOX-11** **Its tools, and only these.** It has no file, shell, web or MCP tools of its own:
  - `run_task(request)`: starts a turn in the session with `request` as the person's message (spoken requests are rephrased into a clear written prompt, and the written prompt is what's shown and kept), through the same turn path as a typed message: hooks, the agent, approvals, limits, the record. Refused while a turn runs: then it's `steer`.
  - `steer(request)`: adds to the running turn, as the composer's steer does (spec_024).
  - `stop()`: stops the running turn, as the Stop button does.
  - `status()`: what's running (the turn, its task list, its last tool calls by name, how long it has run), what's waiting on the person (an approval, a question), and the last turn's result (its answer's start, the files it changed, its tests, its cost).
  - `answer(choice)`: answers the question the agent asked, by one of its offered options or as free text (VOX-13 for approvals).
  - `recent(n)`: the last `n` (at most 10) messages of the conversation, as text, so "what did it say about the cart?" can be answered.
  - `end_conversation()`: ends the conversation once its goodbye has been said (VOX-14).
- **VOX-12** **Nothing secret reaches the voice.** What `status` and `recent` give the voice model goes through the same redaction as the agent's tool output (keys, tokens, hidden files never in it: spec_006 FS-15), and they never include file contents beyond what's already in the conversation.
- **VOX-13** **Approvals by voice, carefully.** When Blitz needs permission to run a command or change a file, in the conversation's own turn or a background task's or sub-agent's, the voice asks in the first person, the action in plain words ("I need permission to run `make deploy`, may I?"). A spoken yes allows it **once**, and only when:
  - that approval card is showing in the window, and
  - the voice model's `answer` names that approval (its ID, which only `status` gives it), and
  - the transcript of what the person said is a yes (Blitz checks the words, not only the model's reading of them).

  "For the session" and "always" can't be given by voice; denying can. Anything else (a mishearing, a second approval arriving) leaves the card for a click.
- **VOX-14** **When it ends.** A voice conversation ends when the person clicks the microphone again, says to stop ("stop listening", the voice ends it with a goodbye), closes the workspace or the window, turns voice off, or after `idle_minutes` with nothing said and nothing running. A turn the voice started keeps running when the voice conversation ends; its summary is then spoken only with `summaries = "always"`.
- **VOX-15** **One at a time.** One voice conversation per workspace in the service (a second is refused, `VOICE_BUSY`), and one per window: while one is open, the other workspaces' microphones are hidden. It's about the session showing when it starts.
- **VOX-16** **The record.** The conversation keeps what was said, in writing: the person's words (from the input transcript) and the voice's replies (from the output transcript), each marked as spoken, in the session's messages beside the typed ones, so they're searched, summarised and resumed like any other. A spoken exchange that only answered (no `run_task`) is kept the same way. The voice model's own audio isn't kept; nor is the person's (VOX-51).
- **VOX-17** **Spoken summaries.** When a turn ends (with `summaries` allowing it), the voice says what happened, in one or two sentences, from facts Blitz collects, not from a guess:
  - whether it finished, stopped, failed or hit a limit;
  - the files changed (their count, the main ones by name);
  - tests or builds it ran and how they went (from the shell tool's exit codes);
  - what's waiting: an approval, a question, a failed check;
  - the cost, only when over a dollar;
  - *2026-11-13 (the owner: "the voice agent is calling the sub agent, but then unaware of the subagents response")* the agent's answer itself, its final text (secrets masked, at most 6,000 characters), so the voice tells the person what the agent found and answers their question from it. A task the voice handed over always has its answer said, whatever `summaries` is; `status` has the last answer too.

  While a turn runs the voice stays quiet, except to say an approval or a question is waiting (VOX-13) or that the turn failed. With `summaries = "always"` and no voice conversation open, the summary is spoken through a short live connection opened for it (text in, audio out), and only while the window is in front and the session showing; otherwise a turn ending is the usual notification.
- **VOX-18** **Usage.** The live model's tokens are logged by the `voice` logger at debug (VOX-33). Adding them to the session's usage and cost, as the session's other model calls are, is still to do; Settings › Voice says audio isn't free.
## 5. The window

- **VOX-20** **The microphone in the chat bar.** With voice on (VOX-02), the composer has a microphone button beside Send. Its states:
  - *ready*: the outline icon, "Talk to Blitz (⌥⌘V)";
  - *connecting*: a spinner;
  - *listening*: filled and pulsing with the input level;
  - *speaking*: a wave while the voice talks; speaking over it interrupts it (Gemini's barge-in; the page drops what it hasn't played);
  - *muted*: crossed out, the conversation open but the microphone closed;
  - *not set up* (VOX-03) and *no microphone* (VOX-50): disabled, the tooltip saying why and what to do.

  It uses the composer's existing icon buttons and the page's icon set (`@mdi/js`), with no new styles.
- **VOX-21** **The shortcut.** ⌥⌘V starts and ends a conversation (with `listen = "hold"`, holding it talks); listed in the command palette as **Talk to Blitz** and **Stop talking**, and in the menu bar's View menu.
- **VOX-22** **The voice bar.** While a conversation is open, a slim bar above the composer shows:
  - the live transcript, the person's words and the voice's, a line or two, the latest at the end;
  - mute, and end;
  - the model and voice, small.

  It never covers the conversation's last message. The written messages land in the conversation as they finish (VOX-16), marked with a small speaker icon.
- **VOX-23** **The status bar** shows a red **Listening** while a conversation is open, in any view (the conversation needn't be showing); clicking it ends the conversation. The tray's **Stop talking** is still to do.
- **VOX-24** **When something goes wrong** (the connection drops and can't resume, the model refuses, the quota is spent), the bar says so in a sentence with what to do, the conversation ends, and nothing typed is lost.

## 6. Settings › Voice

- **VOX-40** **A section of its own.** Settings has **Voice** in its *Setup* group beside Agents, Connectors and Browser, and it's shown in simple mode too (the switch is a choice people make, not an advanced one). It reads and saves the settings with `GetConfig` and `SetConfigValue`, like Browser's section; every change applies to the next voice conversation, and turning voice off applies at once.
- **VOX-41** **What it holds**, top to bottom:
  1. **Voice**: the on/off switch (VOX-01), with a line on what it does and where the audio goes: "Talk to Blitz with Google's Gemini Live. While you talk, your microphone's audio goes to Google; Blitz keeps the words, not the audio."
  2. **Model**: a menu of the Google provider's live models (VOX-06), the default first ("Default: *name*"), with the provider used (Gemini API or Vertex AI). Without a Google provider: what's missing and **Set up Google** (VOX-03).
  3. **Voice**: a menu of the prebuilt voices (*From the personality* first when the personality has one, spec_064 PER-40), with **Play** beside it to hear a short sample (one short live call; the first one says it costs a little).
  4. **Microphone**: the input devices the system has (the window's choice, VOX-04), with a level meter that moves while the section is open and the microphone allowed, and **Allow the microphone** when it isn't (VOX-50).
  5. **Listening**: *Tap to talk* or *Hold to talk* (`listen`).
  6. **Spoken summaries**: *In voice conversations*, *Always*, *Never* (`summaries`).
  7. **End after**: minutes of silence (`idle_minutes`).

  With the switch off, only item 1 shows (VOX-02). Labels and text are in the page's catalogs (en-US, es, fr-CA).
- **VOX-42** **The settings file** shows `[voice]` like any other table, and Settings › Voice reflects a change made there when it's saved.

## 7. Audio and the service

- **VOX-30** **The path.** The page server speaks HTTP/2 only, and a browser can't stream to the service, so voice works as the terminal does (spec_062): `VoiceService` (`voice.proto`) streams the conversation down and takes the person's side by unary calls, through the page server's API proxy like every other call.
  - `Converse(workspace, session_id)` opens a conversation and streams it until it ends: first `started` (its ID, the model, the voice, tap or hold), then the voice's audio (24 kHz PCM), transcripts (`heard`, `said`, pieces with `final` ending a line), `state` (listening, speaking), `interrupted`, the window's work (`run_task`, `steer`, `stop`, `answer`, `approve`), `error` and finally `ended` (closed, idle, goodbye, off, summary, error). The stream ending ends the conversation. `summary_only` says the session's last turn's summary and ends (VOX-17); `sample_voice` says a short sample in that voice and ends (VOX-41).
  - `SendVoice(workspace, id, …)` sends one of: the person's audio (16 kHz PCM; the page sends 100 ms at a time), `speaking` (hold to talk's start and end), `waiting` (what the window shows waiting, whole each time it changes, VOX-13) or `end`.
  - `DescribeVoice(workspace, list_models)` is Settings › Voice's: the settings, why voice isn't set up, the model in use and its API, Gemini's voices, the live models, the personality's voice.

  The page captures with `getUserMedia` (audio only, echo cancellation and noise suppression on) and an AudioWorklet, in 20 ms frames resampled to 16 kHz; it plays the voice's audio through one `AudioContext`, dropping its queue on an interruption.
- **VOX-31** **Refusals**, as SVC-30's reasons:
  - `VOICE_OFF`: voice is off (VOX-02);
  - `VOICE_NOT_SET_UP`: no Google provider, or it can't sign in (VOX-03);
  - `VOICE_MODEL`: the model isn't a Gemini Live model;
  - `VOICE_BUSY`: a conversation is already open in this workspace (VOX-15);
  - `UNKNOWN_VOICE`: `SendVoice` to a conversation that has ended, or another workspace's;
  - `INVALID_ARGUMENT`: a sample voice that isn't one of Gemini's.
- **VOX-32** **Only the window.** VoiceService is served as every other service is: on the service's socket, reached by the page through the page server's proxy (its origin and token checks), never on a network port of its own.
- **VOX-33** **Logs.** The `voice` logger (spec_003 LOG-01b) records each conversation's start and end, its model, reconnects and errors at info, and the events (not audio, not transcripts) at debug.

## 8. Microphone and privacy

- **VOX-50** **The microphone permission.** The app asks for the microphone only when a voice conversation starts or Settings › Voice's meter is shown, and only with voice on:
  - Electron's permission handler (`pagePermission`) allows `media` with audio only, never video, for the window's own page; every other page is refused, as before. The page asks only with voice on, so with voice off nothing asks;
  - on macOS the app's `Info.plist` has `NSMicrophoneUsageDescription` ("Blitz uses the microphone when you talk to it.") and its entitlements `com.apple.security.device.audio-input`, as notarization requires; macOS asks the person the first time, never while voice is off;
  - when the system says no, the button is disabled with "Allow Blitz to use the microphone in System Settings › Privacy & Security › Microphone" (Windows' and Linux's equivalents), and Settings › Voice has a button that opens that page.
- **VOX-51** **What goes where.** While a conversation is open, the microphone's audio goes to Google's Gemini Live API through the provider the person set up (their key, or their Vertex AI project) and nowhere else. Blitz writes no audio to disk. The transcripts are kept as the conversation's messages (VOX-16). The privacy guide says this, and the active-installs ping (spec_054) says nothing about voice.
- **VOX-52** **Organizations.** *Not built (VOX-90).* An organization's policy would switch voice off for its members, the switch showing off and disabled. The voice uses the person's Google provider, not the organization's gateway (VOX-05).
## 9. Testing

- **VOX-60** **A strict fake of the Live API** in `pkg/engine/runtime/wirefake` (`NewLive`): a WebSocket server speaking `BidiGenerateContent` as Gemini does (setup first, then realtime input; transcripts, interruptions, tool calls, `goAway`, resumption handles; refusing out-of-order messages as the real one does), so the engine's tests are as strict as the API.
- **VOX-61** **Engine tests**: each tool (VOX-11), approvals by voice and every refusal of VOX-13, summaries from facts (VOX-17), idle and goodbye endings, resumption across `goAway` and a dropped connection, redaction (VOX-12), usage (VOX-18), and that with voice off nothing of voice is built (VOX-02).
- **VOX-62** **Service tests**: the socket's refusals (VOX-31), origin and token (VOX-32), the page server forwarding the upgrade, a whole conversation against the fake.
- **VOX-63** **Page tests** (Vitest): the button's states, what's waiting, the PCM conversions, the shared conversation, and the fake service's conversation as the window drives it (refused while off; heard, handed over, said; an approval read out and allowed once; one a workspace; off ends it; the sample). Checked on the fake page in Chromium with a stubbed microphone: Settings › Voice off and on, a conversation, ending it from the status bar, in both themes.
- **VOX-64** **The paid check** (`TestVoiceLive` in `pkg/engine`), with `BLITZ_LIVE=1`, a Gemini key and recorded speech (`BLITZ_VOICE_WAV`, 16 kHz; macOS's `say` and `afconvert` make one): the request heard, handed over with `run_task`, the tool's reply survived, a turn's end summarised. It costs a few cents and isn't run in CI. *2026-11-13: it found the voice calling `run_task` with `request` while the tool's schema said `prompt`, so every hand-off was refused (and the model said it had passed the work on when it hadn't); the argument is `request` now, the instructions say to call the tool before saying so, and each tool call is logged at debug.*
## 10. Done when

- With voice off, nothing of voice shows, runs or asks for the microphone, in the window, the service or the OS.
- With it on and Gemini set up, the person clicks the microphone, says a task, and the agent does it as if typed, approvals and all; the voice tells them when it's done, what changed and what's waiting.
- Every setting is in Settings › Voice as well as the file, and applies as VOX-40 says.
- The guide has a **Voice** page (`docs/content/guide/voice.md`), the privacy and configuration pages list `[voice]` and the microphone, spec_002's settings table has `[voice]`, and the release page says how to get started.
- Tests as section 9, `tools/coverage.sh --config=race` at or above the floor, and the macOS build notarized with the new entitlement.

## 11. Open questions

- **VOX-90** VOX-52 assumes the organization's policy can lock a setting; the policy today switches off built-ins by name (spec_053 EN-31). If it can't lock `[voice]`, building VOX-52 adds that.
- **VOX-91** Wake words ("Hey Blitz") are left out: they need the microphone open all the time. Not planned.
