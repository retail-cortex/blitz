# Contributing to Blitz

Thanks for helping. This page is how to get a change into Blitz: setting up, the rules the code follows, the checks CI runs, and how review works. The [development guide](content/development/_index.md) has the layout and the commands in full.

## Before you start

- **Bugs**: open an issue with what you ran, what happened and what you expected, and the output of `blitz doctor`. `doctor`, `--help` and CLI errors stay in English so they can be shared.
- **Features**: open an issue first. Blitz closes the gaps it already knows about before adding features (the [backlog](content/about/specs/spec_backlog_026.md) and the [parity gaps](content/about/specs/spec_parity_027.md)), and every feature is specified before it's built: a new requirement in the spec it extends, or a new spec.
- **Security problems**: don't open a public issue. Report them privately through the repository's **Security** tab (**Report a vulnerability**). Fixed problems, and known gaps, are recorded in the [security log](https://retail-cortex.github.io/blitz/about/security-log/) (`docs/content/about/security-log.md`); a fix adds its entry.

## Set up

You need git and [Bazelisk](https://github.com/bazelbuild/bazelisk) (as `bazel`), plus Xcode on macOS, or GTK and WebKitGTK on Linux for the desktop app. [Building from source](content/development/building.md) has the steps for each OS. Then:

```bash
bazel build //...
bazel test //...
```

## Everything goes through Bazel

Bazel brings its own Go, Node, pnpm, buf and Hugo, so everyone builds with the same versions. Use it for the upkeep too:

| Instead of | Run |
|---|---|
| `go …` | `bazel run @rules_go//go -- …` |
| `go mod tidy` | `bazel run //tools:tidy` (also updates `MODULE.bazel` and the BUILD files) |
| `pnpm add …` in the page | `bazel run -- @pnpm//:pnpm --dir "$PWD/apps/desktop/web" add …` |
| writing BUILD files by hand | `bazel run //:gazelle` |

Plain `go build` and `go test` don't work anyway: the API's generated Go code exists only in the build. Point your editor's gopls at Bazel with `GOPACKAGESDRIVER=$PWD/tools/gopackagesdriver.sh`.

## The rules the code follows

- **Dependencies between parts.** Apps depend on `pkg/`, never on each other; front ends never on the engine. `bazel run //tools:check_deps` checks it. See [architecture](content/architecture/_index.md#the-dependency-rules).
- **License headers.** Every source file starts with the Apache License header. `bazel run //tools:license_headers` adds it to new files.
- **Comments.** Every package and exported name has a doc comment that says what it is and why, not how. The build fails without one (the `doccomment` analyzer; `doc.lint.test.ts` for the page; buf's rules for the protos).
- **Tests.** A bug fix comes with a test that fails without the fix. Go tests use Testify: `require` for what the rest of the test depends on, `assert` for the checks, the most specific assertion there is, and table-driven cases as subtests (`t.Run`). Messages say what the values don't.
- **Coverage.** CI measures the Go tests' coverage of `apps/` and `pkg/` and fails if it drops below the floor in `tools/coverage/floor.txt`; the [coverage report](content/about/coverage.md) is on the site. New code comes with tests that cover it.
- **Strings the user sees** go into all three catalogs in `pkg/i18n/locales/` (`en-US`, `es`, `fr-CA`); a test enforces it. See [translating](content/development/translating.md).
- **Dependencies.** After adding or updating one, run `tools/third_party_notices.sh` to regenerate `THIRD_PARTY_NOTICES`; CI fails if it's stale or a new license isn't one Blitz accepts.
- **The API** (`proto/blitz/v1`) doesn't break its clients. buf checks each change; a deliberate break is declared with a `Breaking-API: <why>` line in the commit message.
- **Docs.** A change to behaviour updates its pages on the site, its spec, and, where a person has to check it, [manual verification](content/development/manual-verification.md).

## Check before you push

CI runs these on macOS and Linux; run them first:

```bash
tools/check_format.sh                   # gofmt, BUILD files, license headers
bazel test --config=race //...          # every test, Go with the race detector
bazel run //tools:check_deps            # the dependency rules
tools/third_party_notices.sh --check    # THIRD_PARTY_NOTICES is current
bazel run //tools/specs                 # every spec is indexed and names real paths
bazel run //tools/codeowners -- --check # CODEOWNERS matches OWNERS.txt
tools/coverage.sh                       # coverage, and not below the floor (slow: every test, instrumented)
bazel build //docs:site                 # the site builds (a broken link fails it)
```

CI also runs govulncheck, buf's lint and breaking-change checks, the sandbox and gVisor tests (Linux), and builds the release archives on both platforms, which must come out identical.

## Commits and pull requests

- One commit per change, with a subject that says what the change does ("Show the license in the desktop app") and a body that says why.
- Keep pull requests to one change; say how you tested it, and include screenshots for anything visible.
- A maintainer from [`OWNERS.txt`](https://github.com/retail-cortex/blitz/blob/main/OWNERS.txt) reviews every pull request; GitHub requests the review from `.github/CODEOWNERS`, which is generated from it. CI must pass before merging.

## License

Blitz is licensed under the [Apache License 2.0](https://github.com/retail-cortex/blitz/blob/main/LICENSE). By contributing, you agree that your contributions are licensed under it too.
