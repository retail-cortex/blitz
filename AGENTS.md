# Working on Blitz

For coding agents (and people). The full guide is the docs site's development section, [`docs/content/development/_index.md`](docs/content/development/_index.md) (published at https://retail-cortex.github.io/blitz/development/): the layout, the commands and the conventions. Read it before changing code.

The rules that matter most:

- Everything goes through Bazel: `bazel build //...`, `bazel test --config=race //...`, `bazel run //tools:tidy`, `bazel run @rules_go//go -- …`. Host `go`, `pnpm` or `buf` commands are a last resort.
- Apps depend on `pkg/`, never on each other; front ends never on `pkg/engine` (`bazel run //tools:check_deps`).
- Every source file has the Apache header (`bazel run //tools:license_headers`); every package and exported name has a doc comment.
- Go tests use Testify, with table-driven cases as subtests.
- Before committing: `tools/check_format.sh`, and `tools/third_party_notices.sh` after changing dependencies.
- Specs are in [`docs/content/about/specs/`](docs/content/about/specs/_index.md); a feature updates its spec and its pages on the site.
