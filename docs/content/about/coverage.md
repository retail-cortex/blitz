---
title: Coverage
weight: 15
---

How much of Blitz's code its Go tests run: every line in `apps/` and `pkg/`, measured with `bazel coverage` on Linux (where the sandbox and gVisor tests also run) on each push to `main`. Tools, generated code and dependencies aren't counted. The desktop page's TypeScript is measured separately, lines and branches, by `bazel test //apps/desktop/web:coverage` (Vitest's V8 coverage, over `src/` without the generated client and the development stand-ins), which fails below the thresholds in `apps/desktop/web/vite.config.ts`; its report is among that test's outputs.

CI fails if the total drops below the floor in `tools/coverage/floor.txt`. When coverage rises, raise the floor with it, so it can only go up. Packages are listed least covered first: the top of the table is where tests are most needed.

{{< coverage >}}

## Measuring it yourself

```bash
tools/coverage.sh                       # every test, instrumented; fails below the floor
bazel run //docs:serve                  # this page, with your numbers
```

The line-by-line report is Bazel's LCOV file, `bazel-out/_coverage/_coverage_report.dat`; `genhtml` (from lcov) or an editor's coverage view can show it against the source.
