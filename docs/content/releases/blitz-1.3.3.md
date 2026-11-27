---
title: Blitz 1.3.3
weight: 3
---

Your chat no longer stops when the network blinks. On Linux, starting a container, toggling a VPN or roaming between Wi-Fi access points used to end a running turn with "network error", even though nothing was wrong with Blitz or the model. Blitz 1.3.3 keeps the turn running and picks it up where it left off. It also gives your personalities a voice of their own: a locale, a speed and a pitch, on sliders.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.3.3).
2. Restart the Blitz service when the app offers to: the fix for dropped connections is in the service as well as the window.
3. To tune a voice, open **Settings › Personality**, edit one of your own personalities (duplicate a built-in to start from it), and set its **Locale**, **Speed** and **Pitch**. **Play** says a sample before you save. See [A voice of your own](../guide/voice.md#a-voice-of-your-own) for the one-time Google Cloud setup.

### What's better

- **Turns that survive a dropped connection.** When the connection between the window and the Blitz service drops, the turn carries on in the service, and the window quietly asks for it again from the last thing it received. Approval cards stay where they were. The terminal does the same. **Stop** still stops at once.
- **Pages on your own computer open in Blitz's browser.** When the agent starts an app and opens `http://localhost:3000` for you, and you say yes, the page now loads in the web tab. Until now it loaded only in your system browser, unless a setting most people never changed was on.
- **A voice of your own.** A personality can speak a locale (English as spoken in the United Kingdom, French as spoken in Canada, and 51 more), faster or slower, lower or higher. These are spoken by Google Cloud's Text-to-Speech, which Blitz now uses by itself for a personality that sets any of them, in conversations, spoken summaries and samples alike. Everything else is still spoken by Gemini Live.
- **Personalities as long as they need to be.** The 2,000-character limit is gone.
- **Agents that setup makes can be opened and fixed.** Setting up a project sometimes wrote an agent with an agency level that isn't one ("autonomous"), which then didn't load and couldn't be edited in Studio. Setup now names the levels it may use, and Studio opens such an agent with the value marked, so you can choose a real one and save.
- **Prices for all of Gemini 4.** Neon, Carbon and Hydrogen are priced alongside Argon, so their costs show and cost limits hold. Argon's and Carbon's published increase on 1 December 2026 is built in.

### Good to know

- The Google Cloud voice needs Google Cloud's own sign-in (`gcloud auth application-default login`) and a project with the Text-to-Speech and Speech-to-Text APIs turned on; a Gemini API key isn't enough for it. **Settings › Voice** says what's missing, and which project speech is made in. Without it, a tuned personality is spoken by Gemini Live in its own voice.
- With the Google Cloud voice, what you say reaches the model that writes the replies as text, as a typed message would. That is your workspace's small model unless you choose another in **Settings › Voice**, of whichever provider it is. Google Cloud charges for the minutes it listens and the characters it speaks. [Privacy](../guide/privacy.md) has the details.
- It answers a little more slowly than Gemini Live. Speaking over it stops it, as before.
- A turn left with no window or terminal attached for a minute is stopped, so one that lost its client for good doesn't keep running. A terminal that's killed outright leaves its turn running for up to that minute.
- A long personality is sent with every turn, and costs its tokens each time.
