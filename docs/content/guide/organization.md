---
title: Your organization
weight: 6
---

Spec: [Blitz for organizations](../about/specs/spec_053_enterprise.md) (EN-10 to EN-22).

If your company runs Blitz Enterprise, connect Blitz to it: you sign in with your work account, and Blitz uses the models your organization offers you, under its limits, instead of keys of your own. Blitz itself stays the same, free and open source; the service is your organization's.

## Connect and sign in

**In the desktop app:** Settings › **Organization**. Enter the address your admins gave you, such as `https://blitz.acme.com`, and **Connect**. Then **Sign in**: your browser opens your company's sign-in page, and Blitz is signed in when you finish there.

**From the terminal:**

```sh
blitz enterprise connect https://blitz.acme.com
blitz enterprise login        # shows an address and a code to enter in any browser
blitz enterprise status       # who's signed in, and whether the service answers
```

`blitz enterprise login --browser` signs in in a browser on this computer instead of with a code.

The desktop app, the CLI and the tray share one sign-in: the Blitz service on your computer keeps it. Your sign-in lasts as long as your organization allows (often a working day); Blitz renews it on its own until then, and asks you to sign in again after.

## What changes while you're connected

- **The models are your organization's.** The model menu lists the ones it offers you, at its prices; a model your settings name that it doesn't offer gives way to its default. Your own keys and providers are set aside, not removed: disconnect and they're back.
- **Its limits apply.** Your organization may cap what you spend, a day or a month, and how fast you can send requests.
- **What you send goes to it.** Your requests to models go through your organization's service, which may keep a record of them under its policies. Ask your admins what they keep.
- **Signed out, the models wait.** A workspace tells you to sign in rather than falling back to your own keys.

A workspace's settings can't connect Blitz to an organization, or change which one: only you can, in your own settings (`[enterprise] url`).

## Disconnect

Settings › Organization › **Disconnect**, or `blitz enterprise disconnect`: you're signed out, and Blitz uses your own providers again. `blitz enterprise logout` signs out but stays connected.

## When something's wrong

`blitz doctor` says whether you're signed in to your organization; with `--online` it also says whether its service answers.

| What you see | What to do |
|---|---|
| "Sign in to your organization … to use its models" | Sign in: Settings › Organization, or `blitz enterprise login` |
| "… isn't answering" | Check your network or VPN; **Retry** |
| "… offers you no models" | Ask your admins for access to a model |
| "Accept the organization's terms" | Your organization changed its terms: accept them where it asks |
