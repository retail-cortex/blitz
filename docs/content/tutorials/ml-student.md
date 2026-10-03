---
title: Machine learning student
weight: 20
---

This tutorial follows a real study workspace: **blitz-research**, a student's folder for a machine learning course (CSCE 478/878: decision trees, neural networks, Bayesian classifiers, SVMs, ensembles, clustering, reinforcement learning; four exams, auto-graded labs in R, readings and a project). Over the term it collects the course's sources and turns them into notes, quizzes, flashcards, spoken overviews and runnable experiments.

It's the idea behind NotebookLM, kept to what helps you learn. Your files stay in a folder you own, every output is a plain file you can edit and commit, and nothing happens that you didn't ask for.

1. [Choose a folder](#1-choose-a-folder)
2. [Run the wizard](#2-run-the-wizard)
3. [Add your sources](#3-add-your-sources)
4. [Write notes you can trust](#4-write-notes-you-can-trust)
5. [Practise recall](#5-practise-recall)
6. [Listen to an overview](#6-listen-to-an-overview)
7. [Run experiments](#7-run-experiments)
8. [Export notes as PDFs](#8-export-notes-as-pdfs)
9. [Digest new sources overnight](#9-digest-new-sources-overnight)

You need Blitz installed and a model ([Getting started](../getting-started/_index.md)). **Use Gemini or Claude**: they read a PDF as it is, figures, tables and equations included. Other models get only a PDF's text, which loses the maths and diagrams that matter most here ([PDFs in](../guide/images.md#pdfs-in)).

## 1. Choose a folder

One folder per course keeps its sources, notes, experiments and conversations together, and in git, so every change can be reviewed and undone.

```sh
mkdir -p ~/Projects/blitz-research && cd ~/Projects/blitz-research && git init
```

- **Desktop:** **Workspaces › Open workspace…**, then choose the folder.
- **Terminal:** `blitz` in the folder.

## 2. Run the wizard

The setup wizard writes the instructions every agent in this folder reads, and proposes skills and agents. It asks what the project is, so it works for a course as well as for code.

- **Desktop:** the chat's first tile, **Set up this workspace for agents**, or **Help › Set up the agent harness**.
- **Terminal:** `/setup`.

Tell it about the course, how you work and what's off limits. For this workspace:

> This is my research and study workspace for CSCE 478/878 Machine Learning: decision trees, ANNs, Bayesian classifiers, genetic algorithms, k-NN, SVMs, ensembles, deep learning, clustering and RL. Four exams (45%), auto-graded labs in R (21%), readings (19%) and a project. I write notes in Markdown with LaTeX and Mermaid, labs in R, experiments in R or Python. Help me with active recall and exam prep. Follow the course's academic integrity and AI policies.

From that, it wrote `.agents/AGENTS.md`, with every coding agent's file pointing at it (here `BLITZ.md` holds `@.agents/AGENTS.md`). The parts that matter:

```markdown
## Commands
- Run an R script: `./bin/Rscript <path_to_script>.R`
- Run R tests: `./bin/Rscript -e "testthat::test_dir('tests')"`
- Generate experiments catalog: `python3 scripts/generate_experiments_index.py` (verify: `--check`)

## Directory Layout
- `experiments/<name>/`: one experiment each: `Tutorial.md`, its scripts, `data/` (large files git-ignored)
- `notes/`: course notes, fact sheets, derivations, exam guides
- `tests/`: quizzes, mock exams, and the scripts' tests

## Conventions
- Notes: Markdown with LaTeX `$...$` and Mermaid diagrams.
- Accuracy: verify algorithms, formulas and loss functions against the textbook and the literature.
- R: always `set.seed(...)`, for reproducible results and the auto-graded labs.
- Study: active recall, spaced repetition, concept checks for the four exams.

## Boundaries
- Adhere strictly to the course's academic integrity and AI policies.
```

Read it and correct what's wrong: it's yours, and short, accurate instructions beat long ones. Add a `sources/` line now (step 3): "`sources/`: the course's readings, slides and lab handouts; read-only to agents".

The wizard also proposed a kit, and this student kept all of it:

| Skills (`.agents/skills/`) | |
|---|---|
| `add-ml-topic` | A topic note in `notes/`: overview, equations with every symbol defined, a Mermaid diagram, practical considerations, five facts for recall |
| `verify-ml-facts` | Checks a note's claims and formulas against the standard references and fixes them |
| `create-quiz` | A quiz in `tests/quiz-<topic>.md`, each question tagged by difficulty, its solution folded |
| `run-experiment` | A small script that checks the maths numerically; the finding goes back into the note |
| `r-language` | How labs are written and tested in R: the course's packages, seeds, exact function contracts, `testthat` |

| Agents (`.agents/agents/`) | |
|---|---|
| `curator` | Writes and organises `notes/`; can edit files, but has no shell |
| `ml-tutor` | Teaches by asking: Socratic questions, derivations before answers; reads, never writes |
| `evaluator` | Grades your answers in plan mode, so it can't change a file |

Each skill is also a command (`/add-ml-topic decision trees`), and `/agent ml-tutor` switches the agent working on the conversation (in the desktop app, also from the status bar). Make the first commit.

## 3. Add your sources

Make a `sources/` folder and drop files in as the term goes on, from the Files shelf or your file manager:

- **Readings and papers as PDFs**, up to 32 MB each: the zyBook chapters you export, the graduate readings, the papers your project builds on.
- **Slides and lab handouts** as PDFs, or as images (PNG or JPEG).
- **Notebooks** (`.ipynb`): the agent reads them as cells, with their outputs.

Then ask about them. An `@` mention attaches a source to your question:

```text
Using @sources/ch3-decision-trees.pdf, explain information gain versus
Gini impurity, and work the example in Table 3.2 by hand.
```

In the desktop app you can also drop a PDF straight into the chat. The agent also reads sources on its own: `view_document` for a PDF it finds, `read_file` for a notebook. Ask "which of my sources cover pruning?" and it looks through `sources/`.

## 4. Write notes you can trust

Notes are where the course comes together. With the curator:

```text
/agent curator
/add-ml-topic decision trees, from sources/ch3-decision-trees.pdf
```

The note follows the skill: an overview, the equations (entropy, information gain, Gini, with every symbol defined), a Mermaid diagram of recursive partitioning, the practical side (overfitting, cost-complexity pruning, `rpart`'s `cp`), and five facts for recall. Then check it:

```text
/verify-ml-facts notes/decision-trees.md
```

It cross-checks formulas and claims against the references and corrects what's off. Ask it to cite the source and page for each claim it keeps, so you can check the ones that matter.

Open the note in the desktop app to read it as a document: equations as written, diagrams drawn. The course's own note in this workspace (`notes/CourseInfo.md`, the syllabus and grading as tables and Mermaid charts) was written this way too.

## 5. Practise recall

Reading notes isn't learning them. Three ways to be tested:

- **A quiz you can take later:** `/create-quiz decision trees` writes `tests/quiz-decision-trees.md`, each question tagged `[Easy]`, `[Medium]` or `[Hard]`, its solution folded so you can try first.
- **A tutor:** `/agent ml-tutor`, then "quiz me on SVM kernels". It asks before it tells: what you think a margin is, then the derivation, then an edge case. It can ask with buttons to choose from (`ask_user_question`).
- **A grader:** answer a quiz or a past exam question in your own words, then `/agent evaluator` and "grade my answer to question 4". It runs in plan mode, so it critiques without touching your files.

Flashcards are a fourth way, for spaced repetition. Add a skill, `.agents/skills/flashcards/SKILL.md`:

```markdown
---
name: flashcards
description: Flashcards from a topic's note, to read and to import into Anki
---
# Flashcards

1. Read notes/<topic>.md, and its "Key Facts for Active Recall".
2. Write 15–25 cards: one idea per card, answerable in under ten seconds;
   definitions, "why" questions, equations, and the edge cases exams like.
3. Write tests/flashcards-<topic>.md: each card as "**Q:** …" then "**A:** …".
4. Write tests/flashcards-<topic>.tsv for Anki. Its first two lines are
   "#separator:tab" and "#html:false", then one card per line: question,
   a tab, answer, with no tabs or line breaks inside either.
```

`/flashcards decision trees`, then in Anki **File › Import** the `.tsv`: its header lines tell Anki the format.

## 6. Listen to an overview

An audio overview is two hosts talking a topic through: a script the model writes from your note, read aloud by a speech model. Good for the bus before an exam.

First, **set a speech model**:

- **Desktop:** **Settings › Workspaces**, this workspace's settings, **Agent and model › Speech model**: `gemini/gemini-2.5-flash-preview-tts` (with a Gemini key), or `openai/gpt-4o-mini-tts`.
- **Settings file:**

  ```toml
  [audio]
  model = "gemini/gemini-2.5-flash-preview-tts"
  speakers = [{ name = "Ana", voice = "Kore" }, { name = "Ben", voice = "Puck" }]
  ```

Then add `.agents/skills/audio-overview/SKILL.md`:

```markdown
---
name: audio-overview
description: An audio overview of a topic, as two hosts talking it through
---
# Audio overview

1. Read notes/<topic>.md.
2. Write notes/audio/<topic>-script.md: a conversation between Ana, who
   explains, and Ben, a sharp student who asks what a listener would. About
   1,200 words, under 9,000 characters; every line starts "Ana: " or "Ben: ".
   Say what equations mean rather than reading symbols. End with the three
   things to remember for the exam.
3. Call generate_audio with text_path notes/audio/<topic>-script.md, path
   notes/audio/<topic>.wav, and speakers Ana and Ben.
```

`/audio-overview decision trees`. `generate_audio` asks before it sends the script to the provider, and its cost shows in `/cost`. Open the `.wav` in the desktop app to play it, or in any player; Gemini makes `.wav` files and OpenAI `.mp3`. Keep `notes/audio/*.wav` out of git (`.gitignore`) if they're large. More in [audio](../guide/images.md#audio).

## 7. Run experiments

Some ideas only click when you run them. Each experiment in this workspace is a folder under `experiments/`, with a `Tutorial.md` that explains it step by step, its scripts, its data (git-ignored) and its results. The first one, `experiments/linear-regression/`, models 70 years of Pacific Ocean temperatures (the CalCOFI database) with ordinary least squares in R, with a hyperparameter deep-dive and diagnostic plots.

To make one, ask in plain words, and the skills supply the how:

```text
Make an experiment comparing rpart's cp values on a public dataset: show
how pruning trades training accuracy for test accuracy. Follow the
r-language skill, with a testthat test.
```

The agent writes the scripts and the tutorial, runs them with `./bin/Rscript` (which keeps R's packages in the project, `renv.lock` recording their versions), and runs the tests. Commands ask before they run, unless you've allowed them.

Two pieces of the workspace keep this tidy without anyone remembering: `scripts/generate_experiments_index.py` rebuilds `experiments/Index.md`, the catalogue of experiments, from each `Tutorial.md`'s title and summary, and a pre-commit hook (`.githooks/pre-commit`, enabled with `git config core.hooksPath .githooks`) runs it on every commit. Ask for that kind of automation when you notice a chore: the agent writes it, with a test.

For quick checks there's `run-experiment`: "check numerically that Gini impurity is maximal at p = 0.5" writes a small script, runs it, and adds the finding to the note.

## 8. Export notes as PDFs

For printing, a cheat sheet, or sharing with a study group:

- **Desktop:** open a note, then **Export as PDF** in its bar. It prints the preview as you see it, equations as written and Mermaid diagrams drawn, to `notes/<topic>.pdf` beside it.
- **Agent:** "export notes/decision-trees.md as a PDF" uses `export_pdf`, which works in the terminal and in workers too. It typesets the Markdown itself: headings become the PDF's outline, and tables, code and images are laid out, but Mermaid diagrams print as their code.

The paper is A4 or Letter, from your locale; `[pdf] page_size` sets it.

## 9. Digest new sources overnight

A worker is a workflow the Blitz service runs on a schedule, unattended, with only the permissions you give it ([the service](../products/service.md#workers)). This one writes notes and flashcards for whatever you dropped in `inbox/` during the day. Create `.agents/workers/digest/WORKER.md` (or, with advanced settings on, **Workers › Add worker** in the desktop app):

```markdown
---
description: Notes and flashcards for new sources in inbox/
schedule: Daily at 02:00
permissions: ["write:notes/", "write:tests/"]
limits: { max_turns: 40, max_cost_usd: 1.00, timeout: 30m }
---
For each file in inbox/ not yet listed in notes/digested.md:
1. Follow .agents/skills/add-ml-topic/SKILL.md for its topic, then
   .agents/skills/verify-ml-facts/SKILL.md on the note.
2. Follow .agents/skills/flashcards/SKILL.md for the same topic.
3. Add the file's name and the topic to notes/digested.md.
```

Enable it with `blitz workers enable digest`, which shows exactly what you're approving. In the morning, `blitz workers runs digest` lists what it did, and each run is a conversation you can open. Move the digested files into `sources/` when you're done with them.

## What it doesn't do

- **It doesn't do your coursework.** The labs and exams are yours; the workspace's own instructions hold it to the course's academic integrity and AI policies, and the tutor and evaluator teach and grade rather than answer for you.
- **It's only as right as its sources.** `verify-ml-facts` and page references help; check what matters, and edit the notes: they're yours.
- **It's not a podcast studio.** An overview is a script read aloud: clear, not produced radio. Keep each under ten minutes (`[audio] max_chars`); split long topics.
- **Scanned PDFs** have no text, so only Gemini and Claude can read them. Very large PDFs (over 14 MB for Gemini, 22 MB or 100 pages for Claude) go as text; split them if you need the figures.
- **It doesn't fetch sources by itself.** Ask it to ("find the original random forests paper and save it to sources/"), and with web access it can, asking first.

Your files stay in your folder. What leaves it is what you send to your model's provider: the sources you attach or the agent reads, and the scripts it reads aloud.
