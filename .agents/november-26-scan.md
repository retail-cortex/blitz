# november-26-patches: the code scan

Branch `november-26-patches`, from main at 76ec07ca (v0.9.0). One PR, one
push at the end. Docs, specs and release notes in the same PR.

Done so far: 7c11daac, the limits guardrail. A turn it stops names the
guardrail and the file to change. A limits file with a setting outside
`limits:` reports that as its problem. A built-in whose file has a problem
runs with its defaults. That last one was a fail-open: a typo in
`limits.md` turned off the force-push and `rm -rf` refusals too. The docs
and the Studio tutorial now nest their examples under `limits:`
(spec_041 GT-32).

That bug is the model for the scan. It had all of these at once:

- **A hard-coded limit nobody could find.** The message didn't say where
  the 200 came from.
- **A docs example that didn't work.** Copying it was silently ignored.
- **Lenient parsing.** An unknown or misplaced key was read as nothing.
- **A safety feature that failed open.**
- **A spec that described each piece, but not how they combine.**

So the scan looks for patterns, not only files.

## What counts as a finding

Every finding goes in the ledger (below) with its file and line, the spec
ID or docs page it touches, and a proposed fix.

1. **Safety and fail-open (S).** A guardrail, permission, sandbox, policy,
   secret or sign-in path that ends up allowing more when something goes
   wrong:
   - a file with a problem;
   - a parse error;
   - a timeout;
   - a nil;
   - a policy switched off by a name that doesn't match.
2. **Behaviour against the spec (B).** The code does something other than
   what its spec item says, or the spec says nothing about a case the code
   decides, as the problem-file case was. A spec item that records the
   owner's own decision ("the owner: …") wins over the code. Otherwise
   it's a question for the review.
3. **Docs that don't work (D).** Every example people copy has to work:
   - the front matter of agents, skills, guardrails, rules, jobs, commands
     and flows;
   - TOML settings;
   - CLI flags;
   - the Settings and Studio labels a page mentions.

   The scan checks each against the real parser, flag set and locale
   strings.
4. **Silent ignores (I).** YAML and TOML read without rejecting unknown
   keys, `_ =` and `x, _ :=` on errors that matter, and `os.ReadDir` and
   `ReadFile` errors dropped. A setting that's misspelled or misplaced
   should say so, not do nothing.
5. **Hard-coded limits (L).** Every number a person can hit: turns, cost,
   time, sizes, counts, retries, page sizes (the Roles page's 200, for
   one). Each needs:
   - a setting, or a documented reason why not;
   - a message that names the limit and where to change it;
   - one definition only. For example, `tasks.go` falls back to `50` while
     `config` already defaults to 50.
6. **Dead code (X).** Unused functions, types, fields, constants, flags,
   RPCs, proto fields, config keys, locale keys, CSS classes, TS exports
   and test helpers.
   - Go is checked against blitz and `../blitz-enterprise`, read only,
     since it imports `pkg/` through `replace`.
   - Front ends are checked against Go, TS and the protos.
7. **Duplicate code (U).** The same logic in two places, for example:
   - limits in `api.Limits`, `gates.Limits` and the job limits;
   - "the lower limit wins" written out per field;
   - path display, relative to the workspace or `~`;
   - frontmatter split and edit;
   - desktop and CLI code that each re-derive what the service could
     return once;
   - locale keys that say the same thing.
8. **Smells (M).**
   - goroutines and tickers without a stop (the owner's soak runs find
     these as leaks);
   - `context.Background()` in a request path;
   - `time.Sleep` in code or tests;
   - long waits in tests;
   - English shown to people that isn't through i18n;
   - errors that don't say what to do;
   - functions over about 80 lines that hold more than one idea;
   - `sync.Map` used where a plain mutex is clearer;
   - comment and doc-comment drift: a comment saying what the code no
     longer does;
   - TODOs;
   - test fakes looser than the SDK they stand for.
9. **Parity (P).** An enterprise or workspace feature that's in the CLI
   but not the desktop, or the other way round, without the spec saying
   so.

Severity: **S** and **B** that people can hit come first, then **D**,
**I** and **L**, then **X**, **U** and **M**.

## How the scan runs

**Phase 0, tools (no code changes).** These are mechanical checks, run
through Bazel. Each one's raw output is kept beside the ledger.

- Go unused code: staticcheck's `unused`, plus `deadcode` from
  golang.org/x/tools, run with `bazel run @rules_go//go -- run …`, over
  blitz with blitz-enterprise's packages as extra roots.
- TS unused exports and files: knip, through the pnpm workspace, run by
  Bazel.
- Locale keys: every key in the en-US file against `i18n.T("…")` and `t("…")`
  in Go and TS, the reverse (keys used but missing), duplicate keys, and
  keys missing from es and fr-CA.
- Config keys: every key `pkg/config` reads against spec_002 and the
  settings reference, both ways.
- Proto: every RPC and field against what the server sets and what
  clients read.
- Docs examples: extract every fenced front-matter and TOML block under
  `docs/content` and parse it with the real parsers, as a test that stays.
  Today's bug would have failed it.
- A list of every numeric literal in a comparison or limit outside tests,
  for **L**.

**Phase 1, file by file (no code changes).** Read every non-test Go and
TS file, about 470 Go files (134k lines) and 230 TS files (51k lines),
next to its spec and docs pages. For each exported function and each
decision in it, note the spec item it implements, or that there's none.
Then work through the finding list above for that file, area by area, in
order of risk:

1. `pkg/engine/gates`, `pkg/guardrails`, `pkg/engine/tools` (permissions,
   approvals, sandbox, shell), `pkg/secrets`, `pkg/secretscan`,
   `pkg/redact`, `pkg/sandboxsetup`.
2. `pkg/enterprise`, `pkg/orgsync`, `pkg/signin`, `pkg/signedin`,
   `pkg/mcpauth`, `pkg/gitcred`, `pkg/selfupdate`.
3. `pkg/service` and `pkg/client`: the error mapping, streams, and
   whether the two sides match.
4. `pkg/config`: defaults, layering (project, org defaults, locked),
   strictness.
5. `pkg/engine`: the runtime, the turn, the workspace, workers, tasks,
   skills, agents, flows, evals, sessions, memory, search, LSP,
   connectors, databases, browser.
6. `pkg/api` and the smaller `pkg/*`.
7. `apps/cli`, then `apps/desktop/web`, `apps/electron`, `apps/tray` and
   `apps/vscode`, checked against each other for parity.
8. `tools/*` and `build/analyzers`.

Out of scope:
- `third_party/mcp-toolbox/internal`, upstream code kept as its authors
  wrote it (nogo excludes it too); only our `embed` glue is in scope;
- generated proto code;
- `bazel-*` output.

**Who reads.** The areas don't depend on each other, so they can be read
in parallel by subagents, one area each, with the same brief and ledger
format. That's a few million tokens over the whole codebase. I verify
every finding myself before it goes into the ledger as confirmed, because
an agent's mistakes come back sounding as sure as its findings. The other
way is that I read it all myself in one thread: slower, but cheaper. Your
call.

**Phase 2, review with you.** The ledger is grouped by area and sorted by
severity. You mark each finding: fix, leave, or ask. Where the spec and
the code disagree, you decide which one changes. The default-limit
question from today (should a turn have a 200-call ceiling at all?) goes
here too.

**Phase 3, fix in batches.** One batch per area, each with:
- tests, table-driven with Testify, and fakes as strict as the SDK;
- the spec items updated, or new dated ones like GT-32;
- the docs pages fixed;
- locale strings edited line by line;
- that area's tests run with `--config=race`;
- a local commit with a plain message (no skip-ci marker).

Nothing builds `//...` or `//apps/electron/...`. The desktop is checked
with `?fake` and Playwright WebKit screenshots, against the real service
with the model keys unset.

**Phase 4, before the push.**
- `tools/check_format.sh`, run alone, reading its exit code;
- `tools/third_party_notices.sh` if a dependency changed;
- `tools/coverage.sh --config=race` at or above the 94.0% floor (dead
  code removed should help);
- the release notes page for the patch.

Then you test locally and say when to push. One push, CI watched with a
time limit.

## The ledger

`.agents/november-26-findings.md`, one row per finding:

| ID | Sev | Kind | File:line | Spec / docs | What's wrong | Proposed fix | Status |
|----|-----|------|-----------|-------------|--------------|--------------|--------|
| N26-001 | S | fail-open | pkg/engine/gates/guardrails.go:347 | spec_041 GT-20 | A built-in's file with a problem turned the built-in off | Fall back to its defaults | fixed 7c11daac |

Status is one of proposed, confirmed, fix, leave, ask, or fixed with the
commit.

## Decisions (the owner, 2026-11-05)

1. **Subagents, with a join.** 19 readers, one area each (lists of the
   789 in-scope files, each in exactly one list). They share one brief
   and one findings format, and they don't run builds. I'm the join: I
   read every report, check every finding against the code before it's
   confirmed, remove duplicates across areas, and write the ledger.
   - Wave 1 (10): safety and enterprise, tools ×2, service,
     api/client/config, the engine's top level ×2, runtime,
     subpackages ×2.
   - Wave 2 (9): CLI ×2, desktop ×4, other apps, small packages ×2.
2. **blitz-enterprise.** It's read to check usage, never written to.
   Whether a shared library is needed is answered separately (no, but
   the contract between the repos should be explicit).
3. **No permanent CI checks yet.** The Phase 0 checks run once, for the
   ledger.
4. **The 200 stays.** It's a reasonable safeguard. The message now also
   says "Send “continue” to carry on where it stopped." in a conversation
   (not in a job's run), because long coding flows hit it (c8c32058).
