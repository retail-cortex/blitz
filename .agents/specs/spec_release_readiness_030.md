# spec_release_readiness_030 — Licensing, comments, documentation

| | |
|---|---|
| Status | Planned (2026-09-27); blocks the next release |
| Depends on | every earlier spec; [spec_monorepo_028](spec_monorepo_028.md) (the build), [spec_release_025](spec_release_025.md) (packages) |

## 1. Purpose

The owner's review (2026-09-27) found the project not ready to release: the old name (Code Puppy) in `NOTICE`, no Apache 2.0 headers in the sources, the license shipped only with the CLI archives and shown nowhere, gaps in the code's comments, specs that describe the code before the monorepo move, a README that is a manual rather than an introduction, no `CONTRIBUTING.md` or owners file, and documentation spread over `.agents/`, `docs/` and the README. This spec closes those gaps, in the order below.

## 2. What was found (2026-09-27)

| Area | Finding |
|---|---|
| Name | `NOTICE` line 1 names the product "Code Puppy"; its history paragraph names the former repository. Other mentions are history (`docs/HISTORY.md`, `NEXT_STEPS`, the setup and release specs) or the MIT attribution to the upstream project. |
| Headers | 0 of ~450 source files carry the Apache header (339 Go, 55 TypeScript, 6 protos, 38 BUILD/MODULE files, 3 `.bzl`, 7 shell scripts, 2 CSS). |
| License in packages | The CLI archives carry `LICENSE` and `NOTICE`; `Blitz.app` and the `.deb` don't; no program can show it; no third-party notices anywhere, though every binary links MIT, BSD and Apache code (and the desktop app embeds React, CodeMirror and others). |
| Comments | 1,124 of 1,434 exported Go and TypeScript declarations have a doc comment; the 310 without are mostly the service's handlers (76 of 96), `pkg/client` (64 of 78) and the desktop app (73 of 212). 10 of 31 Go packages lack a package comment. The generated proto code has none (MR known gap). |
| Specs | 29 specs; many still name pre-monorepo paths (`internal/…`), Make, GoReleaser or `blitz serve`; statuses and "Source" rows are unchecked. |
| Docs | ~79,000 words across `README.md` (426 lines), `.agents/` (AGENTS, ROADMAP, NEXT_STEPS, MANUAL_VERIFICATION, specs) and `docs/` (HISTORY, TRANSLATING). |

## 3. Licensing (first: a release blocker)

- **RR-01** `NOTICE` starts "Blitz", Copyright 2026 Retail Cortex, Apache 2.0. The upstream attribution stays: Blitz began as a port of Code Puppy (MIT), and MIT requires its copyright and permission notice in copies of substantial portions (owner to confirm, §8). The former repository's paragraph moves to the history page.
- **RR-02** Every source file carries the Apache 2.0 header (`Copyright 2026 Retail Cortex` and the standard notice, as a comment in the file's syntax): Go, TypeScript, CSS, protos, Starlark (`BUILD.bazel`, `MODULE.bazel`, `.bzl`), shell, workflows, `index.html`. In Go it sits above the package comment with a blank line between, so it isn't taken as documentation. Not: JSON (no comments), Markdown, generated code, lock files, fixtures.
- **RR-03** `addlicense` (Google's, pinned in `go.mod` as a `tool` like staticcheck) through Bazel: `bazel run //tools:license_headers` adds missing headers; `tools/check_format.sh` (CI) fails on a file without one.
- **RR-04** Third-party notices: `THIRD_PARTY_NOTICES` generated at build time from the Go modules each binary links (their license files, from Bazel's `go_deps` repositories) and the npm packages the page bundles (from the lockfile's `node_modules`), grouped by license. A test fails when a dependency has no license file or one outside an allowed list (Apache-2.0, MIT, BSD-2/3-Clause, ISC, MPL-2.0 file-level, …).
- **RR-05** `pkg/legal` embeds `LICENSE`, `NOTICE` and the notices (from the root files via Bazel, never copies in the tree). Shown by: `blitz license` and `/license` in the REPL (with a pager); `blitzd --license`; in the desktop app, Settings › About › **License** and **Third-party notices** (a dialog with the text), and `/license` in the composer.
- **RR-06** Packages carry them: the CLI archives add `THIRD_PARTY_NOTICES`; `Blitz.app` has `Contents/Resources/{LICENSE,NOTICE,THIRD_PARTY_NOTICES}`; the `.deb` has `/usr/share/doc/blitz-desktop/copyright` (Debian's format, pointing at the Apache text in `/usr/share/common-licenses/Apache-2.0`) and `NOTICE`. A test lists each package's contents.

## 4. Comments

- **RR-10** A package comment on every Go package (the 10 missing: the CLI and its REPL, `pkg/config`, and the engine's agents, runtime, session, skills and tools packages and the two `builtin` ones).
- **RR-11** A doc comment on every exported Go declaration and every exported TypeScript function, component, type and constant, saying what it is for and anything a caller must know (not restating the name). Handlers say which RPC they serve and which errors they return. Unexported code is commented where the why isn't obvious (the existing standard).
- **RR-12** Kept that way: a `nogo` analyzer (`build/analyzers/doccomment`) fails the build on an exported Go declaration without a doc comment (generated code exempt, as for nogo already); the page's text check gains the same rule for exported TypeScript.
- **RR-13** The generated Go code keeps the protos' comments: descriptor sets built with source info (`--experimental_proto_descriptor_sets_include_source_info`), closing the known gap in [spec_monorepo_028](spec_monorepo_028.md) §5.

## 5. Specs brought up to date

- **RR-20** Every spec checked against the code: paths (`internal/…` → `apps/…`, `pkg/…`), commands (Make → Bazel, `blitz serve` → `blitzd`), the desktop layout (the drawer is gone), statuses (Implemented / Partial / Planned, with what's missing), and each "Source" and "Tests" row.
- **RR-21** Kept that way: a test (`//docs:spec_paths_test`) fails when a spec's Source or Tests row names a file or directory that doesn't exist, or when the specs index misses a spec.
- **RR-22** `ROADMAP`, `NEXT_STEPS` and `MANUAL_VERIFICATION` updated the same way; finished items leave `NEXT_STEPS`.

## 6. Documentation as a site

- **RR-30** All documentation lives in `docs/`, built by Hugo through Bazel with `rules_hugo` (the owner's fork, `github.com/retail-cortex/rules-hugo` v0.3.0: not in the Bazel Central Registry, so `bazel_dep` plus `archive_override` with its checksum) and the Geekdoc theme (a pinned release archive with its checksum).
- **RR-31** Layout of `docs/`: `BUILD.bazel` (`hugo_site` `//docs:site`, `hugo_serve` `//docs:serve`, `//docs:site_test` with a link check), `config/`, and `content/`:
  - `_index.md` — what Blitz is, in a page;
  - `getting-started/` — install (releases, verifying them), first run, API keys, the first workspace;
  - `guide/` — the CLI and REPL, the desktop app, the service, workers, safety and the sandbox, configuration, extending (agents, skills, MCP, hooks), web search, translations (from `TRANSLATING.md`);
  - `development/` — building from source, the monorepo and its rules (from `AGENTS.md`), testing, manual verification, releasing, contributing;
  - `specs/` — the specs, their index, and a page per spec;
  - `project/` — roadmap, next steps, history.
- **RR-32** Root keeps only what tools and packages need there: `README.md` (short), `LICENSE`, `NOTICE`, `OWNERS.txt`, and `AGENTS.md` as a short pointer for coding agents (§8). `CONTRIBUTING.md` lives at `docs/CONTRIBUTING.md` (GitHub finds it there) and the site mounts it. `.agents/` is emptied into the site.
- **RR-33** Published to GitHub Pages by a workflow on pushes to `main` that change `docs/` (and by hand): `bazel build //docs:site`, the link check, then `actions/upload-pages-artifact` and `actions/deploy-pages`.

## 7. README, contributing, owners

- **RR-40** `README.md`: what Blitz is and why, a screenshot of the desktop app, install (release downloads and how to verify them), a five-minute quick start, **Building from source** in detail (prerequisites per OS: Bazelisk; Xcode command-line tools on macOS; `libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config` and bubblewrap on Linux; then build, test, run the CLI, the service and the desktop app, the page's dev server, packages, and common problems), and links to the site for everything else.
- **RR-41** `docs/CONTRIBUTING.md`: how to set up, the Bazel-only rule (tidy, formatting, packages through Bazel), the checks CI runs and how to run them first, commit messages, specs first for features, translations, headers and comments, and the review process.
- **RR-42** `OWNERS.txt`: the maintainers (name and GitHub handle, no e-mail) and what each owns; `.github/CODEOWNERS` generated from it so reviews are requested automatically.

## 8. Decisions (the owner, 2026-09-27)

1. **Upstream attribution**: kept — `NOTICE` names Blitz, then carries Code Puppy's MIT notice.
2. **GitHub Pages**: build only at first; the owner enabled Pages for the repository the same day, so the site deploys from `main` (RR-33).
3. **Headers**: the full Apache boilerplate.
4. **`AGENTS.md`**: a short pointer at the root; the content lives in the site.
5. **Owners**: Ryan McGuinness (@rmcguinness) until others are named.

## 9. Order and size

| Step | What | Size |
|---|---|---|
| 1 | Licensing: `NOTICE`, headers and their check, `pkg/legal`, `/license` everywhere, third-party notices, packages | 1–2 sessions |
| 2 | Comments: package and doc comments, the analyzer, proto comments | 1–2 sessions |
| 3 | Specs and the other `.agents` documents brought up to date, with the path test | 1–2 sessions |
| 4 | The docs site: rules_hugo, theme, content moved and split, Pages workflow | 1–2 sessions |
| 5 | README, CONTRIBUTING, OWNERS | 1 session |

Each step ends with `bazel test --config=race //...`, the format and dependency checks, and a commit; steps 1 and 4 add their own tests. The release waits for all five.
