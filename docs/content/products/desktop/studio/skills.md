---
title: Skills
weight: 40
---

**Studio › Skills** is where you write the workspace's skills: recipes the agent follows for one kind of task, such as writing release notes or adding an API endpoint. A skill can be instructions alone, combine other skills, or carry scripts for steps that are better as code. The agent picks a skill up when a request matches what it's for.

## Open it

Choose **Studio › Skills**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Skills: the workspace's skills in .agents/skills](../../../../images/desktop/studio-skills.png)

The list shows the skills in `.agents/skills`, each with its kind (simple, combining skills, with scripts) and what it's for. A skill whose scripts you haven't reviewed says **Scripts to review**.

## Make a skill

1. Choose **+**. To start from a skill you can already see (a global one, or one from a registry), choose **Add from…** (the copy icon) instead.
2. On **General**, say **What it's for** and give it a **Name** (its folder's name: lowercase letters, digits and hyphens).
3. Add **Trigger phrases**: examples of requests it's for, such as *write the release notes*. The agent matches a request to them by meaning, so they needn't be exact.
4. Write its **Instructions**. **Redraft** has the model write or improve them.
5. Choose **Save**. It's saved as `.agents/skills/<name>/SKILL.md`.

![A skill's General tab: what it's for, its name, trigger phrases and instructions](../../../../images/desktop/studio-skills-edit.png)

## The other tabs

- **General** also has the skills it combines, the agents it's offered to, and its tools.
- **References**: files the skill reads, such as a style guide or an example. Add, upload, edit or delete them; PDFs, images and Markdown preview.
- **Scripts**: a new script from a template, **Redraft** to have the model write it, and **Test run** with arguments. The **Packages** section adds the packages a script needs.
- **Scenarios**: check that the skill works. Type what a user might ask and choose **Try** to see whether the agent used the skill and what it said; **Keep it** to save a good try as a scenario; **Run all** runs them in the background.

Opening a file gives it the whole tab; the back arrow returns to the list.

## Review its scripts

A skill's scripts run without asking only once you've reviewed them. Until then the agent asks before each run, and a job's run is refused. Choose **Review scripts** in the skill: it lists each script, its file and the packages it installs. **Trust these scripts** remembers them; a new or changed script is asked about again.

In a chat, `\` and a skill's name uses that skill for the request.

More: [Skills](../../../guide/skills.md).
