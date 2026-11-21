---
title: Reasoning
weight: 43
---

Spec: [reasoning](../about/specs/spec_066_reasoning.md).

See how the agent got to its answer. In the desktop app, the **Reasoning** view draws a turn as a graph: every model call, the reasoning the model shared, the tools it chose, what they returned and the sub-agents it asked. It works for past chats too, and it grows as a turn runs.

## Open it

Click the graph icon in the chat's bar, next to **History**. The view slides down under the top menu, which stays usable; close it with ✕ or Escape.

## Read it

- **Down the middle**, the model's calls, in order. Each says which model answered, its tokens in and out, how long it took and what it cost (estimated at today's prices).
- **On the left**, the steps of reasoning the call shared. Fold them with the call's **steps** button.
- **On the right**, the tools it called (a failed one is red), the sub-agents it asked (open one to see its own calls) and what it said.
- **A crossed-out eye** on a call means the model reasoned but its provider doesn't share the reasoning, as with OpenAI's reasoning models.

Click a card to read it in full beside the graph: the whole step, a tool's arguments and result, **Copy**, and **Show in chat** to jump to that turn in the conversation. Arrow keys move between cards; Enter opens or folds one.

## While a turn runs

The newest call appears as it's made, with a **Thinking…** card showing the reasoning as it streams. **Follow** (the target button) keeps it in view; move the graph and Follow turns off, click it to turn it back on.

## Choose what's shown

Use the arrows and the turn picker to move between turns. The lightbulb, wrench and speech-bubble buttons show or hide the reasoning, the tools and the words; the expand and collapse buttons open or fold every call and sub-agent at once.

## What it shows

The reasoning is the model's own summary, as its provider sends it. Blitz asks Gemini models that think for their thought summaries on every call (they cost nothing extra: the thinking is billed either way); turn thinking off with `thinking_budget = 0` and there's none to show. Claude shares its thinking when thinking is on (set a `thinking_budget` for the model, or use one that always thinks). OpenAI's reasoning models share at most a summary. Other models, local ones included, show their calls, tools and words; their reasoning appears only when the provider sends it apart from the answer. Secrets Blitz knows are masked, as they are in the chat. Very large turns are drawn up to their first 1,500 cards.
