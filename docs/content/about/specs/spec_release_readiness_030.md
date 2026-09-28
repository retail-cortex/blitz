---
title: "030 · Release readiness"
weight: 30
---

*Licensing, comments, documentation* (`spec_release_readiness_030`)

| | |
|---|---|
| Status | Implemented: steps 1 (licensing), 2 (comments), 3 (specs), 4 (the docs site) and 5 (README, contributing, owners, and test coverage) done 2026-09-27. |
| Depends on | every earlier spec; [spec_monorepo_028](spec_monorepo_028.md) (the build), [spec_release_025](spec_release_025.md) (packages) |

## 1. Purpose

The owner's review (2026-09-27) found the project not ready to release: the old name (Code Puppy) in `NOTICE`, no Apache 2.0 headers in the sources, the license shipped only with the CLI archives and shown nowhere, gaps in the code's comments, specs that describe the code before the monorepo move, a README that is a manual rather than an introduction, no `CONTRIBUTING.md` or owners file, and documentation spread over `.agents/`, `docs/` and the README. This spec closes those gaps, in the order below.

## 2. What was found (2026-09-27)

| Area | Finding |
|---|---|
| Name | `NOTICE` line 1 names the product "Code Puppy"; its history paragraph names the former repository. Other mentions are history (`docs/content/about/history.md`, `NEXT_STEPS`, the setup and release specs) or the MIT attribution to the upstream project. |
| Headers | 0 of ~450 source files carry the Apache header (339 Go, 55 TypeScript, 6 protos, 38 BUILD/MODULE files, 3 `.bzl`, 7 shell scripts, 2 CSS). |
| License in packages | The CLI archives carry `LICENSE` and `NOTICE`; `Blitz.app` and the `.deb` don't; no program can show it; no third-party notices anywhere, though every binary links MIT, BSD and Apache code (and the desktop app embeds React, CodeMirror and others). |
| Comments | 1,124 of 1,434 exported Go and TypeScript declarations have a doc comment; the 310 without are mostly the service's handlers (76 of 96), `pkg/client` (64 of 78) and the desktop app (73 of 212). 10 of 31 Go packages lack a package comment. The generated proto code has none (MR known gap). |
| Specs | 29 specs; many still name pre-monorepo paths (`internal/…`), Make, GoReleaser or `blitz serve`; statuses and "Source" rows are unchecked. |
| Docs | ~79,000 words across `README.md` (426 lines), `.agents/` (AGENTS, ROADMAP, NEXT_STEPS, MANUAL_VERIFICATION, specs) and `docs/` (HISTORY, TRANSLATING). |

## 3. Licensing (first: a release blocker)

- **RR-01** `NOTICE` starts "Blitz", Copyright 2026 Retail Cortex, Apache 2.0. The upstream attribution stays: Blitz began as a port of Code Puppy (MIT), and MIT requires its copyright and permission notice in copies of substantial portions (owner to confirm, §8). The former repository's paragraph moves to the history page.
- **RR-02** Every source file carries the Apache 2.0 header (`Copyright 2026 Retail Cortex` and the standard notice, as a comment in the file's syntax): Go, TypeScript, CSS, protos, Starlark (`BUILD.bazel`, `MODULE.bazel`, `.bzl`), shell, workflows, `index.html`. In Go it sits above the package comment with a blank line between, so it isn't taken as documentation. Not: JSON (no comments), Markdown, generated code, lock files, fixtures.
- **RR-03** `addlicense` (Google's, pinned in `go.mod` as a `tool` like staticcheck) through Bazel: `bazel run //tools:license_headers` adds missing headers; `tools/check_format.sh` (CI) fails on a file without one.
- **RR-04** Third-party notices: `THIRD_PARTY_NOTICES`, at the root and committed so a license change shows in review, written by `tools/third_party_notices.sh` (the Go program `//tools/notices`). It asks Bazel which Go modules the three programs link (`bazel query --notool_deps`: build tools left out) and which npm packages the page bundles (its runtime dependencies, walked through the pnpm lockfile), and reads each one's license files from Bazel's copies: 121 Go components with the standard library, 163 npm packages. The Apache License is printed once, at the end; other licenses with their component. It fails on a component without a license, or with one outside the accepted list (Apache-2.0, MIT, BSD-2-Clause, BSD-3-Clause, ISC, MPL-2.0, and the Pictogrammers Free License of the Material Design icons, whose icons are Apache-2.0). An npm package that ships no license file is described by its `package.json` license, that license's standard terms and its author. CI runs it with `--check` on both runners (the file must be current).
- **RR-05** `pkg/legal` embeds `LICENSE`, `NOTICE` and the notices (Bazel copies the root files in; they stay the only copies in the tree). Shown by: `blitz license [full|third-party]` and `/license` in the REPL (paged through `$PAGER`, else `less`, on a terminal); `blitzd --license[=full|third-party]`; in the desktop app, Settings › About › **Licenses** and **Third-party notices** (a dialog with the three texts), and `/license` in the composer.
- **RR-06** Packages carry them: the CLI archives add `THIRD_PARTY_NOTICES`; `Blitz.app` has `Contents/Resources/{LICENSE,NOTICE,THIRD_PARTY_NOTICES}`; the `.deb` has `/usr/share/doc/blitz-desktop/{copyright,NOTICE,THIRD_PARTY_NOTICES}` (`copyright` in Debian's machine-readable format, pointing at `/usr/share/common-licenses/Apache-2.0`). `//release:contents_test`, `//apps/desktop/packaging:app_contents_test` (macOS) and `:deb_contents_test` (Linux) check each package.

## 4. Comments

- **RR-10** A package comment on every Go package (the 10 missing: the CLI and its REPL, `pkg/config`, and the engine's agents, runtime, session, skills and tools packages and the two `builtin` ones).
- **RR-11** A doc comment on every exported Go declaration and every exported TypeScript function, component, type and constant, saying what it is for and anything a caller must know (not restating the name). Handlers say which RPC they serve and which errors they return. Unexported code is commented where the why isn't obvious (the existing standard).
- **RR-12** Kept that way: a `nogo` analyzer (`//build/analyzers/doccomment`, with its own tests) fails the build on a package or an exported Go declaration without a doc comment; a comment of only directives (`//go:embed`) doesn't count, one on a const, var or type group covers its names, and tests and generated code are exempt. The page's `doc.lint.test.ts` does the same for exported TypeScript (the license header doesn't count). buf's comment rules require a comment on every service, RPC, message and enum (request and response messages say which RPC they belong to).
- **RR-13** The generated Go code keeps the protos' comments: descriptor sets built with source info (`--experimental_proto_descriptor_sets_include_source_info` in `.bazelrc`), closing the known gap in [spec_monorepo_028](spec_monorepo_028.md) §5.
- **RR-14** Done 2026-09-27: 122 Go declarations and 10 package comments, 69 TypeScript exports, and 160 proto elements documented; the comments that describe behaviour were checked against the code.

## 5. Specs brought up to date

- **RR-20** Every spec checked against the code: paths (`internal/…` → `apps/…`, `pkg/…`), commands (Make → Bazel, `blitz serve` → `blitzd`), the desktop layout (the drawer is gone), statuses (Implemented / Partial / Planned, with what's missing), and each "Source" and "Tests" row.
- **RR-21** Kept that way: `bazel run //tools/specs` (CI) fails when a spec isn't in the specs index, or names a repository path that doesn't exist (anywhere in its text, old-layout paths included; examples and paths marked "(planned)" aside). It moved with the specs to the site (`docs/content/about/specs`, index `_index.md`).
- **RR-22** `ROADMAP`, `NEXT_STEPS` and `MANUAL_VERIFICATION` updated the same way; finished items leave `NEXT_STEPS`.
- **RR-23** Done 2026-09-27: the check found 9 stale references (the desktop app's former separate module, pre-monorepo paths, the service still called `blitz serve`), all fixed; every spec's status says what's true (the parity spec 29 of 99 requirements done and 2 in part, the backlog 13 of 45); `ROADMAP` keeps its dated entries under a note mapping the old layout, and `NEXT_STEPS` marks the superseded layout decision.

## 6. Documentation as a site

- **RR-30** All documentation lives in `docs/`, built by Hugo through Bazel with `rules_hugo` (the owner's fork, `github.com/retail-cortex/rules-hugo` v0.3.0: not in the Bazel Central Registry, so `bazel_dep` plus `archive_override` with its checksum) and the Geekdoc theme (a pinned release archive with its checksum).
- **RR-31** Layout of `docs/`: `BUILD.bazel` (`hugo_site` `//docs:site`, `hugo_serve` `//docs:serve`), `hugo.yaml`, `layouts/` (a link render hook: a relative link to a Markdown file resolves to its page, so the files read the same on GitHub and on the site, and a link to a missing page fails the build, which is the link check), `static/` (the Blitz mark), and `content/`. Diagrams are Mermaid code blocks, rendered by Geekdoc's code-block hook:
  - `_index.md`: what Blitz is, in a page;
  - `getting-started/`: install, verifying a download, configuring, the first run;
  - `products/`: a page per program: the CLI and its REPL, the service and its workers, the desktop app;
  - `guide/`: what applies to every program: configuration, safety, models, agents and tools, extending, skills, search, images, language, logs and telemetry;
  - `architecture/`: the layers and their dependency rules, the engine, the service API, sandboxing and trust, the build, and the API reference (`architecture/api/`, generated from `proto/blitz/v1` by proto-gen-md-diagrams at build time: `//docs:api`, a page per proto with Mermaid class diagrams);
  - `packages/`: a page per shared package under `pkg/`;
  - `development/`: working on Blitz (from `AGENTS.md`), building from source on each OS, where to pick up, manual verification, translating;
  - `about/`: performance (measured, and by design), the roadmap, history, and the specs (`about/specs/`, the index as `_index.md`).
- **RR-32** Root keeps only what tools and packages need there: `README.md` (short), `LICENSE`, `NOTICE`, `OWNERS.txt`, and `AGENTS.md` as a short pointer for coding agents (§8). `CONTRIBUTING.md` lives at `docs/CONTRIBUTING.md` (GitHub finds it there) and the site mounts it (`//docs:contributing`, under Development). `.agents/` is emptied into the site.
- **RR-33** Published to GitHub Pages by a workflow on pushes to `main` that change `docs/` (and by hand): `bazel build //docs:site`, the link check, then `actions/upload-pages-artifact` and `actions/deploy-pages`.

## 7. README, contributing, owners

- **RR-40** `README.md` (the desktop screenshot is `docs/static/images/desktop.png`, taken from the page with its fake service): what Blitz is and why, a screenshot of the desktop app, install (release downloads and how to verify them), a five-minute quick start, **Building from source** in detail (prerequisites per OS: Bazelisk; Xcode command-line tools on macOS; `libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config` and bubblewrap on Linux; then build, test, run the CLI, the service and the desktop app, the page's dev server, packages, and common problems), and links to the site for everything else.
- **RR-41** `docs/CONTRIBUTING.md`: how to set up, the Bazel-only rule (tidy, formatting, packages through Bazel), the checks CI runs and how to run them first, commit messages, specs first for features, translations, headers and comments, and the review process.
- **RR-42** `OWNERS.txt`: the maintainers (name and GitHub handle, no e-mail) and what each owns; `.github/CODEOWNERS` generated from it by `bazel run //tools/codeowners` so reviews are requested automatically (CI: `-- --check`).
- **RR-43** Test coverage, measured and kept from falling (the owner's request, 2026-09-27): `tools/coverage.sh` runs `bazel coverage` over every test with the instrumentation limited to `apps/` and `pkg/`, and `//tools/coverage` summarizes Bazel's combined LCOV report per package and file. CI's Linux job runs the tests this way (with the race detector), puts the table in the job summary, uploads the summary, and fails below the floor in `tools/coverage/floor.txt`, which only ever goes up. The docs workflow publishes the latest summary from CI on `main` as **About › Coverage**. First measured 2026-09-27: 78.6% of 20,075 lines in CI on Linux (78.7% of 19,936 on macOS; each compiles its own sandbox code), so the floor starts at 78.0%; lowest `apps/desktop` 47.3%, `pkg/client` 47.4%, `pkg/secrets` 50.3%, `pkg/loginitem` 55.4%.

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
