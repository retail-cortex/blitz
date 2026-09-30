---
description: Ask me clarifying questions about a task before proposing anything
argument-hint: "<task>"
mode: plan
---
Before proposing anything for this task, find out what I actually want. Task: $ARGUMENTS

Read what you need from the workspace to understand the task's context, then ask me clarifying questions with ask_user_question: one question per call, each specific and answerable, with options when there are clear choices. Ask about goals, scope, constraints, edge cases, interfaces, testing and anything you'd otherwise have to assume. Keep asking until the remaining uncertainty wouldn't change the approach; don't ask what the code already answers.

Only then propose an approach: a short summary of what you understood, the decisions my answers settled, the plan in numbered steps naming the files involved, and the risks. Don't change any files.
