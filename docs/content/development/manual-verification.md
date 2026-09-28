---
title: "Manual verification"
weight: 30
---

Everything here needs a person, a real terminal, real credentials, or GitHub. The automated suite (413 tests on macOS and Linux) covers the logic behind each item; this list checks the parts it can't. Each item has an **expected** result — if you see something else, note it next to the item.

Setup for most items: `bazel build //apps/cli:blitz //apps/service:blitzd`, then put `bazel-bin/apps/cli` and `bazel-bin/apps/service` on your `PATH` (or use a release archive). Use a scratch Git repository as the workspace so edits are safe.

**Cost note:** items marked 💲 call a paid API. A short session costs cents; long sessions and `/compact` cost more.

---

## 0. Setup

- [ ] `blitz config init`, then add your API key(s) to `~/.blitz/.env.toml`.
  **Expected:** file created with mode 600; re-running without `--force` refuses.
- [ ] `blitz doctor` 💲 then `blitz doctor --online`
  **Expected:** credentials ✓, model ✓, `model request` ✓ with `--online`, shell sandbox **on**, pricing ✓ for the default model.

## 1. Gemini end to end 💲

- [ ] Ask it to read a file and summarize it.
  **Expected:** answer streams in as it's generated; Markdown renders; a dim usage line (`↳ … in · … out · context … · $…`) follows the turn.
- [ ] Ask it to fix a small bug that needs `grep` + an edit + `go test` (or your language's tests).
  **Expected:** tool badges appear; the edit asks for approval with a colored diff; the test command asks for approval; the turn finishes.
- [ ] `/cost` and `/context`.
  **Expected:** non-zero tokens; cost shown; context size roughly matches the usage line.
- [ ] Set `[context] token_threshold = 4000` temporarily, then have a few long turns.
  **Expected:** the conversation keeps working past the threshold; `/context` stays near or below it.

## 2. Anthropic end to end 💲

Set `[llm] provider = "anthropic"` (key via `api_key`, `ANTHROPIC_API_KEY`, or `ant auth login`).

- [ ] Banner shows model `claude-opus-5`; `doctor --online` passes.
- [ ] A multi-step task with several tool calls (read → edit → run tests).
  **Expected:** completes without "invalid request" errors between tool calls (thinking blocks are carried across correctly).
- [ ] Run two turns in a row, then `/cost`.
  **Expected:** "read from cache" is non-zero from the second turn on.
- [ ] Quit, then `blitz --continue "what did we just do?"`.
  **Expected:** it remembers the previous turn's details.
- [ ] Optional: `fallbacks = "off"` in config, repeat one turn. **Expected:** works the same.

## 3. OpenAI-compatible and Ollama 💲

- [ ] `provider = "openai"` with a real key: a turn with at least one tool call.
  **Expected:** tool runs; no JSON printed as text.
- [ ] `provider = "ollama"` with a local model that emits tool calls as JSON text (e.g. `qwen2.5-coder`).
  **Expected:** the JSON becomes a real tool call (badge shown), not echoed text.

## 4. Terminal experience

- [ ] Up/Down recall history; history survives restart (`~/.blitz/history`, mode 600).
- [ ] Ctrl+R searches history.
- [ ] Tab completes `/com` → `/compact`, `/agent he` → `helios`, `/resume ` → this directory's session IDs, `look at @src/` → file names.
- [ ] Multi-line: end a line with `\` and continue; also a block between two `"""` lines.
  **Expected:** sent as one prompt with the line breaks kept.
- [ ] Resize the terminal mid-answer. **Expected:** no garbled output afterwards.
- [ ] Light terminal theme: `COLORFGBG="0;15" blitz`. **Expected:** Markdown readable on a light background.
- [ ] Spinner shows while waiting and disappears before output and approval prompts.

## 5. Ctrl+C behaviour

- [ ] At an idle prompt with no background processes. **Expected:** exits immediately with "Goodbye".
- [ ] During a long model answer. **Expected:** "⏹ Interrupted", back at the prompt, session still usable.
- [ ] At an approval prompt. **Expected:** the whole turn is cancelled (not just that one action).
- [ ] During a long shell command (`sleep 60` approved). **Expected:** the command is killed promptly.

## 6. Approvals

- [ ] Long edit: diff is truncated; `d` shows the full diff and asks again.
- [ ] `s` on a command, then the identical command again. **Expected:** no second prompt this session.
- [ ] `a` on a command; restart; same command in the **same** directory → no prompt; in **another** directory → prompts.
- [ ] `/approvals` lists both; `/approvals revoke 1` removes one.
- [ ] Add `deny = ["rm -rf *"]` under `[sandbox.commands]`, approve everything with `a`, then ask for `rm -rf build`. **Expected:** blocked by policy without a prompt.

## 7. Undo, diff, checkpoints

- [ ] After a turn that edits files: `/diff` shows the change; `/checkpoints` lists the turn; `/undo` restores the files.
- [ ] Make an edit via the agent, then change the same file yourself, then `/undo`. **Expected:** refuses with a conflict; `/undo --force` restores.
- [ ] `/diff git` shows the working tree diff.

## 8. Compaction 💲

- [ ] In a session with 5+ turns: `/compact keep the file names we touched`.
  **Expected:** "Replaced N earlier events…"; the next answer still knows the key facts and file names.
- [ ] Quit and `--continue`. **Expected:** the summary still applies (the model knows earlier context but `/context` is small).
- [ ] `/compact` twice with a turn in between. **Expected:** facts from before the first compaction are still known.

## 9. Sessions

- [ ] Run a prompt in directory A, then in directory B. In A: `blitz --continue "…"`. **Expected:** continues A's session, not B's.
- [ ] `/session list` in A shows only A's; `/session list --all` shows both with their directories.
- [ ] `blitz --resume <B's id>` from A. **Expected:** works, with a warning that it started elsewhere.

## 10. Sandbox on your real workflow

- [ ] Ask the agent to run your project's test command. **Expected:** works (build caches are writable).
- [ ] Ask it to `cat ~/.ssh/id_ed25519` (or any blocked file). **Expected:** denied / empty.
- [ ] Ask it to write outside the workspace (e.g. `echo x > ~/Desktop/x`). **Expected:** "Operation not permitted".
- [ ] `npm install` (or another tool writing to its own cache). **Expected:** works if the cache is in `shell_writable_paths`; otherwise fails until you add it.
- [ ] `allow_network = false`, then ask for `curl https://example.com`. **Expected:** fails.

## 11. Background processes

- [ ] Ask the agent to start a dev server in the background, then `/exit`. **Expected:** warning listing it; `k` kills it; `w` waits; `c` cancels the exit.
- [ ] Start one, then from another terminal `kill -9 <blitz pid>`. **Expected:** the server is gone within a second or two (`lsof -i :<port>` shows nothing).

## 12. MCP (real server)

Add to config:
```toml
[[mcp.servers]]
name = "fs"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]
prefix = "fs"
```
- [ ] `doctor --online` shows `mcp fs  N tools`. `/mcp` lists it.
- [ ] Ask the agent to list files using the fs server. **Expected:** approval prompt naming `fs__…` and the server; the call works inside the sandbox.
- [ ] Add `agents = ["qa"]`. **Expected:** the main agent no longer sees `fs__` tools; `invoke_agent` → qa can use them.
- [ ] `kill -9` the CLI. **Expected:** no leftover `npx` / server processes.
- [ ] During a session, `pkill -f server-filesystem` (kill the MCP server), then ask for another fs tool call. **Expected:** it works; `ps` shows a new server process.
- [ ] Point an MCP server at a command that exits immediately (e.g. `command = "false"`). **Expected:** one "unavailable" warning, then one "paused for 15s" warning; later turns aren't slowed and don't repeat the warning.

## 13. Hooks

- [ ] A `pre_tool` hook for `run_shell_command` that exits 2 with a message. **Expected:** shell calls are blocked with that message; other tools work.
- [ ] A `post_tool` hook appending the JSON event to a file. **Expected:** one line per tool call, including tool name and args.
- [ ] A `post_tool` hook `sleep 3; cat >> /tmp/post.jsonl`, then a turn with several tool calls. **Expected:** the turn is not slowed down; the events arrive in `/tmp/post.jsonl` a few seconds later, in tool-call order. Quit right after a turn: the pending events are still written (within about 5 s).
- [ ] A `prompt_submit` hook that blocks prompts containing `password`. **Expected:** "Prompt blocked by hook".

## 14. Web 💲 (search key)

- [ ] `web_fetch` of a docs page. **Expected:** approval per host; readable text returned.
- [ ] Ask it to fetch `http://localhost:…` or `http://169.254.169.254/`. **Expected:** refused ("not a public address").
- [ ] Configure `web.search_provider = "brave"` (or `tavily`) with a key; ask a question needing search. **Expected:** approval once per provider with `s`; results with titles/URLs; follow-up `web_fetch` works.

## 15. Forged tools (helios)

- [ ] `/agent helios`, ask it to forge a small bash tool and run it. **Expected:** approval shows the code as a diff; run needs separate approval.
- [ ] Restart, ask helios to list and run it. **Expected:** still there. Ask it to delete it. **Expected:** gone from `~/.blitz/uc_tools`.

## 16. Scripting

- [ ] `git diff | blitz review this --output-format json | jq .result` **Expected:** a single JSON object; `jq` works.
- [ ] `blitz --output-format stream-json "…" | jq -c .type` **Expected:** `session`, then events, ending with `result`.
- [ ] `blitz --max-turns 1 "do a multi-step task"; echo $?` **Expected:** exit code 3.
- [ ] Without credentials: `blitz "hi"; echo $?` **Expected:** clear error, exit code 1.

## 17. Other platforms

- [ ] Ubuntu 24.04 desktop: `sudo apt install bubblewrap`, `blitz doctor`. **Expected:** sandbox on, or a clear AppArmor reason (fix: an AppArmor profile for bwrap, or `sysctl kernel.apparmor_restrict_unprivileged_userns=0`).
- [ ] Windows: run the `.exe` with Git Bash on `PATH`; a simple prompt and a shell command. **Expected:** works without the OS sandbox; `doctor` shows sandbox off with a reason.

## 18. Repository and release

- [x] `git rm -r --cached go/bin` and commit (bin is now ignored). *Done: `go/bin` isn't tracked.*
- [x] Pin the actions in `.github/workflows/go-*.yml` to commit SHAs. *Done 2026-09-25, at the latest release of each action's current major: checkout v4.4.0, setup-go v5.6.0, cosign-installer v3.10.1, sbom-action v0.24.2, goreleaser-action v6.4.0. Newer majors exist (checkout v7, setup-go v7, cosign-installer v4, goreleaser-action v7); upgrade them separately and re-run a release.*
- [x] Push; both `go-ci` jobs pass; the Linux job's "Sandbox enforcement must not be skipped" step passes. *Done 2026-09-26 (`0c68c453`, `4e008b41`), after fixing the macOS gofmt check and the Linux parallel-cap test.*
- [x] Tag `v0.1.0` and push the tag. **Expected:** `go-release` creates a **draft** release with 5 archives, 5 SBOMs, `checksums.txt`, `checksums.txt.sigstore.json`. *Done 2026-09-26: unsigned tag on `4e008b41`, since GPG signing wasn't available; the draft had all 12 assets.*
- [ ] Download the assets and verify:
  ```bash
  cosign verify-blob --bundle checksums.txt.sigstore.json \
    --certificate-identity-regexp '^https://github.com/rmcguinness/code_puppy/\.github/workflows/release\.yml@refs/tags/v' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
  shasum -a 256 --ignore-missing -c checksums.txt
  ```
  **Expected:** `Verified OK` and every file `OK`. Then publish the draft.
  *Verified 2026-09-26 with cosign v3.1.3 (the bundle was made by cosign v2 in CI), using the exact identity `https://github.com/rmcguinness/code_puppy/.github/workflows/go-release.yml@refs/tags/v0.1.0`: `Verified OK`. A tampered `checksums.txt` and a wrong identity were both rejected. All 10 files `OK`. The SBOMs are SPDX 2.3 with 96 Go modules (95 in the binary's build info). The darwin/arm64 binary reports `0.1.0` and `vcs.revision=4e008b41`, and `doctor` runs. Draft assets need authentication: `gh api repos/<owner>/<repo>/releases/<id>/assets`, then download each asset with `Accept: application/octet-stream`. Not yet published.*
- [x] Publish the draft. *Published 2026-09-26 as Latest, after deleting a stray empty release on the same tag; the public `checksums.txt` is identical to the verified one.* Binaries aren't Apple-notarized: on macOS, a copy downloaded in a browser gets the quarantine flag, and Gatekeeper refuses to run it until it's cleared (`xattr -d com.apple.quarantine blitz`) or opened through Finder's context menu. Document this in the release notes, or notarize later.
- [ ] After the move to `retail-cortex/blitz` (fresh history, 2026-09-26): the first `v*` tag pushed here produces a draft release, and it verifies against the new identity:
  ```bash
  cosign verify-blob --bundle checksums.txt.sigstore.json \
    --certificate-identity-regexp '^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
  shasum -a 256 --ignore-missing -c checksums.txt
  ```
  **Expected:** `Verified OK` and every file `OK`; the former repository's identity is rejected. (The items above were run against `rmcguinness/code_puppy`.)

- [ ] Desktop release, before the Apple secrets are set: a tag's draft release has `Blitz_<v>_macos_universal.dmg`, `blitz-desktop_<v>_amd64.deb`, `blitz-desktop_<v>_arm64.deb` and a `.sigstore.json` for each, and the macOS job warns that it signed ad hoc. `cosign verify-blob --bundle <file>.sigstore.json` (identity flags as for `checksums.txt`) accepts each.
- [ ] Set the six secrets (spec_release_025 REL-22), tag again. **Expected:** the macOS job notarises (a few minutes) and `spctl` passes. Download the dmg in Safari, open it, drag Blitz to Applications, open it: no Gatekeeper warning, About shows the tag's version, and "Install and start the service" works (the bundled CLI is signed too). On an Intel Mac too, if one is at hand.
- [ ] On Ubuntu 24.04: `sudo apt install ./blitz-desktop_<v>_amd64.deb`; Blitz is in the app launcher with its icon, opens, installs the service (`systemctl --user status blitz`), and `sudo apt remove blitz-desktop` removes it.

## 19. Cost sanity 💲

- [ ] After a day of use, compare `/cost` totals with your provider's billing dashboard. **Expected:** same order of magnitude; if not, adjust `[pricing]`.

## 20. Language

- [ ] `/locale` **Expected:** "Interface language: English (US) (en-US)" and the list `en-US, es, fr-CA`.
- [ ] `/locale ES-sp` **Expected:** confirmation in Spanish; `~/.blitz/.env.toml` now has `locale = "es"` under `[ui]`, with your other settings and comments unchanged.
- [ ] `/help`, `/cost`, an approval prompt, and `/exit` with a background process running. **Expected:** all in Spanish; answer letters still `y/s/a/n` and `k/w/c`.
- [ ] 💲 Ask a question. **Expected:** the answer is in Spanish; code, paths, and command output are unchanged.
- [ ] Restart. **Expected:** still Spanish (banner, spinner "pensando").
- [ ] `/locale ja` 💲 **Expected:** a note that menus stay in English; replies come in Japanese.
- [ ] Put a `de.json` with a few keys in `~/.blitz/locales/` (see `docs/content/development/translating.md`), then `/locale de`. **Expected:** those keys in German, the rest in English.
- [ ] `/locale en-XA` **Expected:** accented ⟦…⟧ text everywhere; note any plain English you see.
- [ ] **Native-speaker review:** someone fluent reads `pkg/i18n/locales/es.json` and `fr-CA.json` (tone, terminology, Québec typography for fr-CA) and runs a short session in each.
- [ ] `/locale en-US` to switch back.

## 21. Images 💲

Use a real screenshot (e.g. a UI with visible text) saved in the workspace as `shot.png`.

- [ ] Gemini: `what does @shot.png show?` **Expected:** a dim `📎 shot.png W×H, … KB` line, then an answer that quotes text from the picture.
- [ ] Anthropic: same. Then ask it to "look at shot.png with view_image". **Expected:** a `view_image` badge and an answer about the picture (image delivered inside the tool result).
- [ ] OpenAI (`gpt-5` or another vision model): same two checks. **Expected:** no "unsupported content part" error.
- [ ] Gemini: the `view_image` check. **Expected:** works; if Gemini rejects an image next to a tool result, note the error text here.
- [ ] Ollama with a vision model (e.g. `qwen2.5vl`) and with a text-only model. **Expected:** vision works; text-only gives a clear API error, not a crash.
- [ ] Take a screenshot to the clipboard (macOS: Cmd+Ctrl+Shift+4), `/paste`, then ask about it. **Expected:** `📎 clipboard-HHMMSS.png … will be sent with your next message`. With text on the clipboard: "The clipboard has no image".
- [ ] Linux desktop: `/paste` with `wl-paste` (Wayland) or `xclip` (X11) installed.
- [ ] A 4000×3000 photo: **Expected:** attaches as 1568×1176; the model still describes it.
- [ ] `@~/Desktop/x.png` (outside the workspace) and `@id_rsa_screenshot.png`. **Expected:** refused; "Nothing was sent".
- [ ] Quit and `--continue "what was in that image?"`. **Expected:** the model can still see it. Check `ls -la ~/.blitz/images` (files mode 600) and that the session file under `~/.blitz/sessions` is small (no base64).
- [ ] `blitz --image shot.png "describe" --output-format json | jq .result` **Expected:** a description.

## 22. Diagnostic log and telemetry

- [ ] After any session: `ls -la ~/.blitz/logs`. **Expected:** directory mode 700, `blitz-YYYY-MM-DD.jsonl` mode 600 with a `start` line; a failed turn adds an `ERROR` line.
- [ ] `BLITZ_LOG_LEVEL=off blitz "hi"`. **Expected:** nothing new in the log.
- [ ] Run a local collector, e.g. Jaeger: `docker run --rm -p 16686:16686 -p 4318:4318 jaegertracing/jaeger:latest`. Then `BLITZ_TELEMETRY=1 blitz` 💲 and do a turn that reads and edits a file.
  **Expected:** in Jaeger (http://localhost:16686, service `blitz`), one trace per prompt: `turn` → `invoke_agent` → `generate_content …` and `execute_tool …`, with `approval` under the edit's tool span. No file content, prompt text or tool arguments in any attribute.
- [ ] Two prompts, quit, then `--continue` with a third. In Jaeger search by tag `gen_ai.conversation.id=<session id>`. **Expected:** three `turn` traces with `turn.index` 1–3; turns 2 and 3 each show a link ("References") to the previous turn, including across the restart.
- [ ] Same with `capture_content = true`. **Expected:** prompts and tool arguments appear; your API key does not.
- [ ] Stop the collector, then run and quit a session. **Expected:** exit is not delayed by more than about 3 s; no telemetry errors in the terminal (they go to the log at debug level).
- [ ] `blitz doctor`. **Expected:** `log` and `telemetry` lines matching your settings.

## 23. Resilience 💲

- [ ] Turn Wi-Fi off, send a prompt, turn it back on within about 5 s. **Expected:** the turn completes (retried); the log shows the failed attempts.
- [ ] `max_retries = 0`, Wi-Fi off, send a prompt. **Expected:** a clear error; the session keeps working afterwards.
- [ ] `stall_timeout_seconds = 5`, then ask for a long answer with `provider = "openai"` (not streamed). **Expected:** it fails after about 5 s with "model API stopped responding". Restore the default afterwards.
- [ ] Ask for a task that makes many tool calls at once (e.g. "read these 12 files"), with `max_parallel = 2`. **Expected:** it completes; with telemetry on, no more than two `execute_tool` spans overlap.

## 24. Steering 💲

- [ ] Start the REPL. **Expected:** the hint line mentions typing (or Ctrl+T) to message the agent.
- [ ] Ask for a multi-step task (read several files, then edit). While it works, type `use tabs for indentation`. **Expected:** output pauses and a `↪ message for the agent` prompt appears with your text. Enter shows "Sent", output resumes, and the agent's next step reflects the message.
- [ ] Press Ctrl+T during a turn. **Expected:** the same prompt, empty. Enter on an empty line shows "Nothing sent".
- [ ] Type a message while an approval prompt is showing. **Expected:** your keys go to the approval prompt, not a steer prompt.
- [ ] Send a message just as the agent writes its final answer (no more tool calls). **Expected:** "The agent finished before reading your message; sending it now", then a new turn with it.
- [ ] During a steer prompt press Ctrl+C. **Expected:** the turn is cancelled ("Interrupted"), as Ctrl+C always does.
- [ ] After a few steered turns, quit and `--continue "what did I ask you mid-way?"`. **Expected:** it knows.
- [ ] After quitting, type in the shell. **Expected:** echo and line editing work normally (the terminal mode was restored).
- [ ] Linux desktop: the same basic check.

## 25. `!`, `/plan`, `/tools`

- [ ] `!git status` and `!ls`. **Expected:** output as in your terminal, then `✅ Done (…)`; the agent's next answer doesn't know about it.
- [ ] `!vim README.md` (or `!less README.md`), then quit it. **Expected:** the program works normally; the REPL prompt comes back intact.
- [ ] `!sleep 30`, then Ctrl+C. **Expected:** `⚡ Interrupted`; the session continues (no exit prompt).
- [ ] `!exit 3`. **Expected:** `❌ Exit code 3`. The audit log has a `user_shell` entry.
- [ ] 💲 `/plan add input validation to the signup handler`. **Expected:** a "Plan mode" note; the agent reads files; any attempt to edit or run a command comes back as "plan mode: … disabled"; the answer is a numbered plan and no files change (`git status` clean).
- [ ] 💲 `blitz --plan "…" --output-format json | jq .result`. **Expected:** a plan; no changes.
- [ ] `/tools`. **Expected:** the active agent's tools with ● on read-only ones; configured MCP servers listed for the primary agent only (unless `agents` says otherwise).

## 26. Model fallback 💲

Set `fallback_models = ["anthropic/claude-sonnet-5"]` with a working Anthropic key, and the primary on Gemini.
- [ ] `blitz doctor --online`. **Expected:** `model` and `fallback 1` each initialised and responding.
- [ ] Break the primary: an invalid `GEMINI_API_KEY`. Ask something. **Expected:** one notice "gemini-… is unavailable; answering with fallback claude-sonnet-5", then the answer. Ask again: no second notice and no delay from the primary.
- [ ] `/cost`. **Expected:** priced at Claude's rates.
- [ ] Fix the key, wait 15 s or more, and ask. **Expected:** "gemini-… is answering again".
- [ ] `fallback_models = ["anthropic/no-such-model"]` with a broken primary. **Expected:** a clear "every model failed" error listing both reasons.
- [ ] `provider = "ollama"` with no `base_url` and Ollama running locally. **Expected:** it works (it used to call api.openai.com).

## 27. Per-agent models 💲

- [ ] `/pin_model qa anthropic/claude-haiku-4-5`. **Expected:** "qa now runs on claude-haiku-4-5", "Saved in …/.env.toml"; the file has `[agent_models]` with that line and your comments intact.
- [ ] `/agents`. **Expected:** 📌 claude-haiku-4-5 next to qa.
- [ ] Ask the main agent to have qa review a file. **Expected:** it works; `/cost` includes Haiku-priced tokens. With telemetry on, qa's `generate_content` span names claude-haiku-4-5.
- [ ] Restart. **Expected:** the pin is still there (`/pin_model` lists it). `blitz doctor` shows `pin qa`.
- [ ] `/model anthropic/claude-sonnet-5`. **Expected:** the main agent switches provider.
- [ ] `/unpin qa`. **Expected:** it runs on the configured model again; the line is gone from the config file.

## 28. Per-model settings 💲

- [ ] `/model_settings`. **Expected:** "No model has settings of its own…".
- [ ] `/model_settings gemini-3.8-flash temperature=0.1 seed=7`. **Expected:** "gemini-3.8-flash now uses temperature=0.1 seed=7", "Saved in …/.env.toml"; the file has `[model_settings."gemini-3.8-flash"]` with both lines and your comments intact.
- [ ] Ask the same short question twice. **Expected:** it works; the answers are close to identical. With `BLITZ_LOG_LEVEL=debug` nothing is logged about dropped settings.
- [ ] `/model_settings gemini-3.8-flash`. **Expected:** temperature 0.1, seed 7, max_tokens "(global: 8192)", top_p "(provider default)".
- [ ] `/model_settings anthropic/claude-sonnet-5 temperature=0.5`. **Expected:** a warning that anthropic doesn't accept temperature for claude-sonnet-5. `/model anthropic/claude-sonnet-5` and ask something: it answers (no 400 error).
- [ ] With an OpenAI key: `/model_settings openai/gpt-5 seed=1`, `/model openai/gpt-5`, ask something. **Expected:** a warning when setting; the answer works (the seed isn't sent).
- [ ] `/model_settings gemini-3.8-flash temperature=5`. **Expected:** "Nothing changed: temperature must be a number in [0, 2]".
- [ ] Restart. **Expected:** `/model_settings` still lists the settings. `/model_settings gemini-3.8-flash reset` removes them and the table from the file.

## 29. Session snapshots 💲

- [ ] In a session, ask the agent to remember a word ("pineapple"). `/session save fruit`. **Expected:** "Saved snapshot fruit (2 messages)…"; `/session list` shows 📸 fruit.
- [ ] Tell it "actually, remember mango". `/session load fruit`, then ask "which word?". **Expected:** "Started session … from snapshot fruit"; it answers pineapple and doesn't know mango.
- [ ] `/resume <the original session's id>` and ask again. **Expected:** mango.
- [ ] `/session save fruit`. **Expected:** refused with a hint about `--force`; with `--force` it's replaced and `/session list` shows one 📸 fruit.
- [ ] Exit. `blitz --continue "which word?"`. **Expected:** continues your last ordinary session, not the snapshot. `blitz --resume=fruit "which word?"`: pineapple, in a new session.
- [ ] In a session that used tools (e.g. edited a file), save, load, and ask what it just did. **Expected:** it knows about the tool calls, not only the chat text.

## 30. `/search` and Google search 💲

- [ ] `[web] search_provider = "google"` with a Gemini key. `blitz doctor --online`. **Expected:** `web search  google: N results`.
- [ ] `/search web golang errors.Is vs errors.As`. **Expected:** up to five numbered links with real site URLs (no `vertexaisearch.cloud.google.com` links), "Handing these to the agent to read", then an answer that cites some of them. No approval prompt for those pages.
- [ ] In the same answer, if the agent tries a page that wasn't listed. **Expected:** an approval prompt.
- [ ] Ask it to save its findings to a file in that turn (e.g. `/search web … and write notes.md`). **Expected:** `create_file` is refused as read-only; no file.
- [ ] `/search session <something said earlier>` after a `/compact`. **Expected:** "Found N messages…" and an answer that recalls the compacted detail.
- [ ] `/search session nonsense-word`. **Expected:** "Nothing in this session's transcript mentions it…", and the agent says it never came up.
- [ ] SearXNG instead: run `docker run -p 8888:8080 searxng/searxng` with `json` added to `search.formats`, set `search_provider = "searxng"` and `search_url = "http://localhost:8888"`, then `/search web …`. **Expected:** it works, with no API key.
- [ ] Remove `search_provider`. `/search web x`. **Expected:** "Web search isn't set up…".

## 31. Side questions (`/btw`) 💲

- [ ] Ask the agent to remember a word. Then `/btw what was the word?`. **Expected:** "Side question: read-only…" and the word.
- [ ] Ask "what did I just ask you on the side?". **Expected:** it doesn't know.
- [ ] `/btw create a file notes.txt`. **Expected:** it says it can't (read-only); no file.
- [ ] `/cost` before and after a `/btw`. **Expected:** the side question's tokens are included.
- [ ] Exit and `--continue`. **Expected:** the recap and history have no trace of the side question.

## 32. Session names and resume hint

- [ ] Start the REPL. **Expected:** the terminal tab/window title is "🐶 (untitled)". Send a prompt; at the next prompt the title is its first line.
- [ ] `/rename Login work`. **Expected:** "Session renamed to Login work."; the window title follows; `/session list` shows it.
- [ ] `/exit`. **Expected:** "Resume with: blitz --resume=session-…"; the terminal's own title comes back. Running that command resumes the session.
- [ ] `ui.terminal_title = false`. **Expected:** the window title isn't touched.
- [ ] In tmux, with `set -g set-titles on`. **Expected:** the tmux title follows the session name.

## 33. Skill definitions and `[skills.policy]`

- [ ] Put a Castor-style `SKILL.md` (with `scripts`, `tool_requirements`, `execution_hints`) in `~/.blitz/skills/<name>/`. `/skills list`. **Expected:** a line such as "1 script · TIER_2_AUDITED_WRITE · allowed by skills.policy".
- [ ] `/skills show <name>`. **Expected:** the content hash, required tools, tier, network, passed and withheld variables, and each script with ✓/✗ and its reasons.
- [ ] Add `custom_hints: {network: "true"}`. **Expected:** the scripts are blocked ("needs the network…"). Set `network = "allowlist"` and `network_allow = ["<name>"]`: allowed.
- [ ] Put the hash from `/skills show` in `trusted_hashes`, then edit the script. **Expected:** blocked ("isn't in skills.policy.trusted_hashes").
- [ ] Break the frontmatter (`hitl_tier: TIER_9`). **Expected:** a startup warning naming the file; `doctor` shows it too.
- [ ] `[skills.policy] sandbox = "docker"`. **Expected:** `doctor` warns: use auto, gvisor or os.

## 34. Script sandbox (gVisor)

- [ ] macOS: `blitz doctor`. **Expected:** `script sandbox   os (gVisor unavailable: gVisor runs only on Linux)`.
- [ ] Linux without gVisor: `doctor`. **Expected:** `script sandbox   os (gVisor unavailable: runsc not found …)`.
- [ ] Linux: unpack gVisor's release tarball into `~/.blitz/bin` (keep `gvisor-bin/` beside `runsc`). `doctor`. **Expected:** `script sandbox   gvisor`.
- [ ] `skills.policy.sandbox = "gvisor"` on macOS. **Expected:** `doctor` warns that skill scripts won't run.
- [ ] Linux with gVisor: kill Blitz with `kill -9` while a script runs, then start it again. **Expected:** `runsc --root ~/.blitz/sandboxes/state list` is empty after the restart.

## 35. Skill scripts and environments 💲

- [ ] Put a skill with a Python script that has a dependency (e.g. `requests>=2.31`) in `~/.blitz/skills/<name>/`. Ask the agent to use the skill. **Expected:** `activate_skill` lists the script as allowed; running it asks once to install ("Install packages for skill … --only-binary :all: -- requests>=2.31"), then runs.
- [ ] Ask again. **Expected:** no install prompt; `/envs` shows one environment with the package, "used by <name>".
- [ ] A script that writes `$SKILL_OUTPUT/report.md` and tries to write a workspace file. **Expected:** the workspace write fails, `report.md` appears under `.blitz/skill-output/…`, and the agent can read it and apply changes with the file tools (diff and approval as usual).
- [ ] `hitl_tier: TIER_3_MANDATORY_APPROVAL`. **Expected:** asked before every run, with no "always" option.
- [ ] A script that needs the network, without `network_allow`. **Expected:** refused with the reason; allowed after `network = "allowlist"` plus `network_allow`.
- [ ] Linux with gVisor: a script that runs `ls ~` and `cat .env` in the workspace. **Expected:** home doesn't exist and `.env` reads as empty.
- [ ] Change the requirements and run again. **Expected:** a new environment and a new install prompt. `/envs prune` removes the old one.

## 36. The service, attaching and workers

- [ ] `blitzd` in one terminal, `blitz` in a workspace in another. **Expected:** the REPL says it attached; a turn streams as usual; an approval prompt appears in the REPL and its answer is honoured; `/pin_model`, `/session save`, `/undo` work.
- [ ] While attached, `blitz --local` in the same workspace. **Expected:** refused, naming the other owner (exit code 2).
- [ ] Stop the service with Ctrl+C during a turn. **Expected:** the turn ends within about 10 s, the socket file is gone, and a new `blitz` in the workspace runs locally.
- [ ] `blitz service install` on macOS, then log out and in. **Expected:** `blitz service status` says installed and answering; `~/.blitz/logs/service.log` shows it started. With a key only in the shell, install warns about it. `blitz service uninstall` removes it.
- [ ] Same on Linux with systemd (`systemctl --user status blitz`).
- [ ] 💲 A worker `workers/check/WORKER.md` with `schedule: every 5 minutes` and `permissions: ["write:reports/"]` whose workflow writes `reports/check.md` and also tries to edit `main.go`. `blitz workers enable check`, wait. **Expected:** it runs on schedule; `reports/check.md` exists, `main.go` is unchanged; `blitz workers runs check` lists the run with one refusal; `/resume <session>` shows the conversation. Edit `WORKER.md`: the listing says "changed" and it stops running until re-enabled.
- [ ] 💲 A worker with `limits: {max_cost_usd: 0.001}`. **Expected:** stopped as "limited" with the cost limit as the reason.
- [ ] 💲 A worker with `agent: qa` and `model: anthropic/claude-haiku-4-5` (the workspace on another model). `blitz workers enable` shows both; `blitz workers run` it. **Expected:** the run's session (`/resume` it) is the `qa` agent's, the run is priced as Haiku in `workers runs`, and the workspace's `/agents` and `/model` are unchanged afterwards. With `agent: nobody`, `blitz workers` lists the problem and a run fails naming it.

## 37. Desktop app

- [ ] `bazel build //apps/desktop/packaging:Blitz.app`, copy it out of Bazel's output (`ditto "$(bazel cquery --output=files //apps/desktop/packaging:Blitz.app)" /tmp/Blitz.app`) and open it with no service running. **Expected:** it offers to install the service; accepting runs `blitz service install` and the app continues.
- [ ] With the service running: open a workspace with "+". **Expected:** a tab named after the directory, showing the active agent and model (or why the model is unavailable).
- [ ] 💲 A turn in the app: its text appears as it streams, not only at the end. **This checks that WebKit streams responses through Wails's asset server**, which the tests can't: they exercise the proxy over plain HTTP.
- [ ] 💲 A turn that edits a file with approvals on. **Expected:** the approval shows the diff; "Allow once" edits the file; "Deny" doesn't, and the agent says so.
- [ ] 💲 While a turn runs, type a message and press Steer. **Expected:** "Queued for the agent", and the agent takes it into account at its next tool call; Stop ends the turn with "Interrupted."
- [ ] 💲 An `ask_user_question`. **Expected:** the question with its options; the answer reaches the agent.
- [ ] Two workspace tabs, a turn in each at once. **Expected:** both stream independently; switching between them doesn't interrupt either, and the workspace dropdown shows a pulsing dot on each (a red `!` when one waits for an approval).
- [ ] First start: `blitz service uninstall`, move `~/.blitz/desktop.json` aside, open the app, click "Install and start the service". **Expected:** within a few seconds the welcome page ("Open a workspace") replaces the install screen.
- [ ] The window on macOS: the title bar area is the page's (drag it by the top bar; the window buttons don't overlap the workspace dropdown).
- [ ] Settings › Appearance: System, Light, Dark. **Expected:** System follows System Settings › Appearance while the app is open; the choice survives a restart. Compact density tightens the lists and chat.
- [ ] Edit a workspace's name, description and colour (dropdown pencil, Settings › Workspaces). **Expected:** the dropdown and welcome cards change; after a restart the same workspaces are open, in order, with the same one shown.
- [ ] Close a workspace (dropdown ✕): it moves to Recent, "Undo" reopens it. 💲 Close one while a turn runs: the dialog stops the turn first. With the REPL attached and running a turn in the same workspace, closing it in the app leaves the REPL's turn running.
- [ ] Stop the service (`blitz service stop` or kill it) with the app open, then click something. **Expected:** "isn't answering. Reconnecting…"; start it again: "Reconnected" and the workspaces reload.
- [ ] 💲 A model answer with a link, a table and a code block: the table and code render; Copy copies the code; clicking the link opens the default browser, not the window. An image in Markdown shows as a link and isn't loaded.
- [ ] 💲 Hover a prompt › Edit: the files and conversation go back, and the prompt is in the composer. "Code only" restores files and keeps the chat. After editing a file by hand, a rewind asks to overwrite.
- [ ] 💲 "Plan first" with a small task: the plan shows as a card; "Yes, carry it out" makes the change; typed feedback makes the agent revise. The mode chip changes the permission mode (the REPL attached to the same workspace shows it too).
- [ ] Run settings: change the model, effort, temperature (saved to `[model_settings]`), mode, agency; add and remove a permission rule; revoke an approval; compact the context.
- [ ] Changes view after a turn that edited files: the files, their diffs, the agent's summary, "Undo last turn"; the Git toggle shows `git diff`.
- [ ] 💲 Notifications: start a turn that needs an approval, switch to another app. **Expected:** macOS asks once to allow notifications; then "<workspace>: approval needed"; clicking it brings Blitz back on that workspace. A long turn finishing while another workspace is shown notifies "done"; a quick one doesn't; nothing notifies while you watch the conversation. Settings › Notifications off stops them.
- [ ] 💲 Images: paste a screenshot (Cmd+Shift+Ctrl+4, then Cmd+V) into the composer, drop a PNG from Finder onto the chat, and attach one with the button: each shows a thumbnail with its size; ask about them and the model describes them. Dropping a file on the top bar does nothing (the window doesn't open it).
- [ ] Commands: type `/` and pick with the arrows; `/cost`, `/context`, `/checkpoints`, `/help` show results; `/nope` is refused; `/usr/bin is odd` goes to the agent as text. 💲 `/btw what is this repo?` answers without adding to History's message count; `/search web connect-go interceptors` lists links then reads them; a workspace command (`.claude/commands/x.md`) runs. Cmd+K: "changes" + Enter shows Changes; a workspace name switches to it; "theme dark" switches the theme.
- [ ] Settings › Appearance › Interface language: Español, then Français (Canada). **Expected:** the whole window changes at once — top bar, dropdown, composer, menus, settings, the "Open a workspace" folder dialog's title — and stays after a restart; System follows System Settings › Language & Region. Set `"language": "en-XA"` in `~/.blitz/desktop.json`: no plain English left except what the service or model wrote.
- [ ] A narrower window (about 1000 px): the run settings float over the chat, the Files shelf floats over the editor and minimizes when a file opens, the view switcher shows icons.

## 38. Parity features (ROADMAP item 25)

- [ ] 💲 Ask "which test files are there?" in this repository. **Expected:** the agent calls `glob` with a pattern like `**/*_test.go` and lists them without shelling out to `find`.
- [ ] 💲 `blitz exec --max-cost-usd 0.01 "refactor the whole repository"`. **Expected:** it stops soon with "the turn reached its cost limit ($0.01)" and exit code 3 (`echo $?`); with `--output-format json` the result has `is_error: true` and `exit_code: 3`. The same with the service running (attached) gives the same exit code.
- [ ] 💲 `blitz exec --timeout 20s "run the full test suite and fix failures"`. **Expected:** stops after about 20 s with "time limit (20s)", exit 3, and no test process left running.
- [ ] `blitz --max-cost-usd 1` with no prompt. **Expected:** usage error (exit 2). With a model that has no price, a one-shot run warns that the cost limit can't be enforced.
- [ ] A repository with only a `CLAUDE.md` (and one with a `GEMINI.md`). `/memory`. **Expected:** listed and followed; a `CLAUDE.md` that copies `AGENTS.md` is listed once.
- [ ] `AGENTS.md` containing `See @docs/style.md` and `@.env`. **Expected:** `/memory` lists `docs/style.md` (imported by AGENTS.md) and not `.env`.
- [ ] `.blitz/rules/go.md` with `paths: ["**/*.go"]` and a distinctive instruction. 💲 Ask the agent to edit a Go file. **Expected:** the first tool result on a `.go` file carries `project_rules` and the agent follows it; editing a Markdown file doesn't trigger it.
- [ ] 💲 `/init` in this repository. **Expected:** the agent proposes `BLITZ.md` with the real `make` targets (approval with diff); after approving, `/memory` lists it. Again: it updates rather than replaces. `blitz init -d <dir>` does the same from the shell.
- [ ] `/mode` lists the five modes. `/mode accept-edits`, 💲 then ask for a file edit and a command. **Expected:** the prompt shows `[accept-edits]`; the edit happens without asking; the command still asks.
- [ ] 💲 `/mode plan`, then "add a README section". **Expected:** a plan, no file changed, and the transcript (`/session list`, resume) shows the prompt as typed.
- [ ] `blitz --permission-mode dont-ask exec "delete tmp.txt"` (💲). **Expected:** the deletion is refused without a prompt; the agent reports it.
- [ ] macOS or Linux with the sandbox: `blitz --permission-mode bypass`. **Expected:** the prompt shows a red `[bypass]`; commands run without asking. With `[sandbox] shell = "off"`: `--permission-mode bypass` is a usage error, `/mode bypass` is refused, and `auto_approve = true` starts in default with a warning (`doctor` shows it under permissions).
- [ ] `[permissions] ask = ["shell(git push *)"]`, `/mode bypass` (with the sandbox), 💲 then ask the agent to push. **Expected:** it still asks; "always" doesn't stop the next push from asking. `deny = ["shell(rm -rf *)"]`: `rm -rf build` is refused, also as `bash -c 'rm -rf build'`.
- [ ] `/permissions allow write(docs/**)` then 💲 an edit in `docs/`: no prompt; an edit elsewhere still asks. `/permissions remove write(docs/**) --save` edits `~/.blitz/.env.toml` without touching its comments.
- [ ] `deny = ["read(secrets/**)"]`, restart: `read_file secrets/x` is blocked, and a shell `cat secrets/x` fails inside the sandbox.
- [ ] A worker with `permissions: ["shell:git push *"]` and a user `ask` rule for `shell(git push *)`: the run's push is refused and recorded.
- [ ] `.claude/commands/greet.md` with `Say hello to $1.`; `/help` lists `/greet` under Custom commands; 💲 `/greet Ada` sends "Say hello to Ada." and the transcript shows `/greet Ada`. Tab completes `/gr`.
- [ ] 💲 `/review` on uncommitted changes: findings with file and line, and no file edited (plan mode). `/verify` runs this repository's Bazel tests (`bazel test //...`) and reports.
- [ ] A command with `allowed-tools: Read, Grep`: 💲 ask it to edit a file; the edit is refused with the allowed list.
- [ ] 💲 `/code-review focus on the new code` runs the built-in skill by name.
- [ ] 💲 `[[hooks.stop]] command = "grep -q stop_hook_active || echo '{\"continue\": true, \"reason\": \"Now run the tests.\"}'"`: after a turn the agent runs the tests once, and the transcript shows `(stop hook) Now run the tests.`.
- [ ] `[[hooks.session_start]] command = "echo 'Use tabs.'"` and 💲 ask for a small edit: the agent follows it. `[[hooks.notification]] command = "osascript -e 'display notification \"Blitz needs you\"'"`: a notification appears when an approval waits.
- [ ] `[[hooks.permission_request]] match = "run_shell_command" command = "grep -q '\"go test' && echo '{\"decision\":\"allow\"}'"`: 💲 `go test` runs without asking; other commands still ask.
- [ ] 💲 `/effort max` with `claude-opus-5-5`, then a hard question: the answer arrives (no 400) and `/set` shows `Effort: max`; `/effort auto` clears it. `--effort low` on a one-shot with `gpt-5` works too.
- [ ] 💲 `/model_settings claude-haiku-4-5 thinking_budget=4000` with `temperature` set globally: the call succeeds (temperature left out) and shows thinking in the transcript.
- [ ] In a real terminal (Terminal.app, iTerm2, and over ssh): 💲 Esc while the agent works stops the turn ("interrupted"); arrow keys and Alt+arrows still edit the line; Esc at an approval cancels the turn; Esc in the steer prompt drops the message and the turn goes on.
- [ ] Shift+Tab at the prompt shows `[accept-edits]`, then `[plan]`, then no tag, with the prompt redrawn in place (no leftover characters). Started with `--permission-mode bypass` (sandbox on), the cycle includes `[bypass]`.
- [ ] `EDITOR=vim`: type a draft, Ctrl+G, add lines, `:wq` — the whole text is sent and shown after the prompt; `:cq` or an empty file returns to the draft. `VISUAL="code --wait"` works. Esc Esc clears a typed line.
- [ ] Pickers in a real terminal (and a narrow one, 40 columns): `/agent`, `/model`, `/resume` — arrows, typing to filter, Esc clears then cancels; the menu redraws in place and leaves one line. 💲 An edit approval: `y`/`s`/`a`/`n` answer at once, "Show the whole diff" shows it and asks again, Esc stops the turn. 💲 A question with options: arrows and Enter, "Another answer…" reads a line.
- [ ] 💲 Have the agent edit a file, `/exit`, then `blitz --resume=<id>`: `/diff` shows the change and `/undo` reverts it. `ls -l ~/.blitz/checkpoints/*/` shows owner-only files.
- [ ] 💲 Three prompts, the second and third editing files. Esc Esc on an empty line: pick the second prompt → "Code and conversation": the files are as before it, the model doesn't remember prompts 2–3, and the second prompt is in the input line to edit. `/rewind` → "Conversation only" keeps files; "Summarize up to here" shrinks `/context`. Edit a file by hand, then rewind its prompt: the conflict picker offers to overwrite. `/exit`, `--resume`, `/rewind` still works.
- [ ] 💲 `/plan add a --version flag`: the plan shows in a picker; "Yes, carry it out" makes the change in the same turn and `.blitz/plans/` has the plan. Again with feedback typed ("also update the README"): the agent revises and asks again. `/mode plan`, a prompt, "Yes, and accept its file edits": the prompt tag becomes `[accept-edits]`.
- [ ] 💲 A three-step task: the checklist appears and ticks off (☐ → ☒) as the agent works. `plan_review = "always"` in the config: a simple prompt is planned first.

## 39. Monorepo and Bazel (spec_monorepo_028)

- [ ] A fresh clone on a Mac with only Bazelisk (`brew install bazelisk`): `bazel test //...` passes, downloading Go, Node and the tools itself.
- [ ] The same on Ubuntu 24.04 with `libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config` (and bubblewrap): `bazel test //...` passes; `bazel build //apps/desktop/packaging:deb`, `sudo apt install ./…deb`, and Blitz opens from the app launcher.
- [ ] The Bazel-built `Blitz.app` (section 37) opens, shows its version under Settings › About (`dev`, or the tag in a `--config=release` build), installs the service (its `blitzd` beside `blitz`), and works as before. On an Intel Mac too, if one is at hand.
- [ ] `bazel run //apps/desktop/web:dev`, then http://localhost:5173/?fake: the page works without a service.
- [ ] Your editor with `GOPACKAGESDRIVER=<repo>/bazel/gopackagesdriver.sh`: go to definition from `pkg/client` into `proto/blitz/v1` (generated) works; no false errors.
- [ ] The first push of the branch: every CI step passes on both runners, and the reproducible job finds the archives identical.
- [ ] The first tag after the move: the draft release has five archives with `blitz`, `blitzd` and `blz`, their SBOMs, `checksums.txt` and its bundle, the dmg and two .debs with bundles; `blitz --version` and `blitzd --version` show the tag.
- [ ] Existing installs: after upgrading, a login item installed as `blitz serve` still starts the service (the hidden command runs `blitzd`); `blitz service install` rewrites it to `blitzd`.
- [ ] With an older service running (or one whose program was deleted), `bazel run //apps/desktop:blitz-desktop`. **Expected:** a banner says the service is older (or its program is gone) with **Restart the service**; clicking it stops that service (its login item, else its process) and starts this build's `blitzd` at login, and the banner goes, with "The service restarted". Settings › Service shows both versions, the service program, and **Restart** and **Stop**: Stop stops it (the window then offers to start it), also for a `blitzd` started by hand in a terminal.
- [ ] Open workspace from `bazel run //apps/desktop:blitz-desktop`: the folder dialog stays open until you choose; the terminal logs `open workspace dialog: "<dir>" …` (a dialog that closes on its own logs an empty answer after a moment).

## 40. API keys and per-workspace settings (spec_config_002 §6)

These touch your real keychain and settings: use a test key, or a throwaway `--config DIR` for the CLI steps.

- [ ] `blitz config set-key gemini`, paste a key (not echoed). **Expected:** Keychain Access shows an item "Blitz: global/llm.gemini.api_key" (service `dev.blitz`); `~/.blitz/.env.toml` has `api_key = "keychain:global/llm.gemini.api_key"`; `blitz config keys` says `gemini keychain`; `blitz doctor` and a turn work with no key in the environment. The first read may ask to allow `security` access: **Always Allow**.
- [ ] A plain key in the file (`api_key = "sk-…"`): `blitz config keys` suggests `secure-key`; `blitz config secure-key anthropic` leaves only the reference in the file.
- [ ] In a project: `blitz config set-key -w openai` and `llm.provider = "openai"` set for it (desktop, below). **Expected:** that project uses OpenAI with its key; another project still uses the global provider; nothing is written inside either project; the file is under `~/.blitz/workspaces/`.
- [ ] On Linux with GNOME Keyring: the same, with `secret-tool search service dev.blitz` listing the item. Without a Secret Service (a server): the key goes to `~/.blitz/secrets.toml`, mode 600.
- [ ] Desktop, Settings › Providers & keys: each provider's status chip matches `blitz config keys`; **Set key** stores it, and a workspace whose model was unavailable ("The model isn't available…") clears the note without reopening; **Move to keychain** on a plain key; **Remove**. Base URLs and the default model save on leaving the field.
- [ ] Desktop, run settings › API keys: "Global" for inherited keys; **Set key** gives the workspace its own; a provider chosen there applies to that workspace only.
- [ ] Desktop, Settings › Settings file: the global file and a workspace's; a typo'd setting saves with a warning; `[llm` is refused with the parse error and nothing is written; ⌘S saves; **Revert** restores.

## 41. Files: the shelf and the editor (spec_files_029, phase 1)

- [ ] In a git project, the top bar's **Files** button: the tree lists folders first, modified files in orange with **M**, new ones in green with **U**/**A**, folders with changes marked. Dotfiles, ignored files (`node_modules`, `bin/`) and `.env` aren't listed; **Show hidden files** lists them dimmed, `.env` with a lock ("Hidden from Blitz"); the setting is remembered after a restart.
- [ ] Open a Go, a TypeScript, a Markdown and a YAML file: each is highlighted in the window's colours (light and dark); typing offers the file's words and the language's keywords; ⌘F searches and replaces; ⌃G goes to a line.
- [ ] Change a file, ⌘S: the tab's dot goes, the tree shows it modified, `git diff` shows the change. Ask the agent something next: it's told you edited the file (and your prompt in the history doesn't show that note).
- [ ] Edit a file in another editor while it's open and unchanged here: within 5 seconds it reloads. Change it here too, then save: "This file changed since you opened it" — **Overwrite** writes yours, **Reload** takes theirs.
- [ ] Let the agent edit a file you have open (unchanged here): it reloads after the tool runs. With your own unsaved changes: "Changed on disk…".
- [ ] Right-click in the tree: **New file** (opens it), **New folder**, **Rename** (F2), **Delete** (asks), **Copy path**, **Copy relative path**. Arrow keys move and open folders; Enter opens.
- [ ] ⌘P: type part of a name (`dsc` finds `discount.go`); `name:12` opens at line 12.
- [ ] A path in an answer (`internal/cart/discount.go`) and a tool call's path open the file.
- [ ] Unsaved changes: closing the tab, closing the workspace (dropdown) and quitting the app (⌘Q, the window's close button) each ask first.
- [ ] With no file open, the chat is center stage (the middle, full width of its column); open a file during a turn: the chat moves to the right and keeps streaming; close the last file: it returns to the middle. In Changes or Workers it stays on the right.
- [ ] The IDE layout: files left, editor middle, chat right. Minimize the shelf (≪): a rail with Show files and Go to file remains, and stays after a restart. Drag the chat's left edge: its width is kept after a restart.
- [ ] The workspace dropdown: switch workspaces; while a turn in another workspace waits for approval, the dropdown shows a `!`; reopen a recent workspace; Open workspace…; Settings is the top bar's far-right button.

## 42. Licensing (spec_release_readiness_030 §3)

- [ ] `blitz license`: Blitz's NOTICE (Copyright 2026 Retail Cortex, then Code Puppy's MIT notice) and where the rest is; `blitz license full` the Apache License; `blitz license third-party` the notices, in `less` (or `$PAGER`); `/license` in the REPL the same.
- [ ] `blitzd --license`, `--license=full`, `--license=third-party`: the texts, and the service doesn't start.
- [ ] The desktop app: Settings › About › **Licenses** and **Third-party notices**, and `/license` in the composer, open the Licenses dialog; its three tabs show the texts.
- [ ] A release: each archive has `LICENSE`, `NOTICE` and `THIRD_PARTY_NOTICES`; `Blitz.app/Contents/Resources` has them; after installing the `.deb`, `/usr/share/doc/blitz-desktop/` has `copyright`, `NOTICE` and `THIRD_PARTY_NOTICES`.
- [ ] Add a Go or npm dependency without updating the notices: CI's "third_party_notices --check" fails until `tools/third_party_notices.sh` is run.

## 43. The docs site (spec_release_readiness_030 §6)

- [ ] `bazel run //docs:serve`, then http://localhost:1313: the menu has Getting started, Products, Guide, Architecture, Shared packages, Development and About; search finds a word from a spec; the dark-mode toggle works.
- [ ] After a push to `main` that changes `docs/`, the **docs** workflow deploys, and https://retail-cortex.github.io/blitz/ shows the change; the architecture page's diagrams render; **Edit page** opens the file on GitHub.
- [ ] A link to a missing page (`[x](nope.md)` in any page) fails `bazel build //docs:site` with the page and the link named.
- [ ] On a phone-width window, the menu opens from the ☰ button and no page scrolls sideways.
- [ ] Architecture › API reference: a page per proto; every diagram draws (the workspace page has 100 and takes a few seconds), in light and dark mode; a comment with a placeholder (`turn.proto`'s `plan` field: "/plan <text>") shows the placeholder.

## 44. README, owners and coverage (spec_release_readiness_030 §7)

- [ ] On GitHub: the README shows the desktop screenshot and its links work; **Contributing** in the repository's sidebar opens `docs/CONTRIBUTING.md`.
- [ ] Open a pull request: GitHub requests a review from the owner in `.github/CODEOWNERS`.
- [ ] CI's Linux job summary has the coverage table; lower `tools/coverage/floor.txt`'s number above the total on a branch and the job fails.
- [ ] After CI on `main`, the site's **About › Coverage** shows the same total, the commit it measured, and each package's files when expanded.
