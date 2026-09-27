# spec_monorepo_028 — Monorepo layout and the Bazel build

| | |
|---|---|
| Status | Implemented (2026-09-27, branch `monorepo`) |
| Source | `MODULE.bazel`, `BUILD.bazel`, `.bazelrc`, `.bazelversion`, `.bazelignore`, `bazel/`, every `BUILD.bazel`, `release/`, `apps/desktop/packaging/`, `go.mod` (`tool` directives) |
| Replaces | [spec_setup_001](spec_setup_001.md) §3–5 (layout, toolchain, Make), GoReleaser in [spec_release_025](spec_release_025.md) |
| Depends on | [spec_setup_001](spec_setup_001.md) |

## 1. Purpose

Blitz is three independent apps (the CLI, the service, the desktop app) over shared packages, built, tested and packaged by Bazel. The owner's decisions (2026-09-27): apps under `apps/`, shared packages under `pkg/` with the engine under `pkg/engine/`, the service its own binary (`blitzd`), Bazel builds everything including the desktop page, protos are always generated (never committed), Bazel manages the tools, and Make goes. They reverse the earlier "No Bazel" decision (2026-09-26): the engine had bled into the front ends, and one build over Go, TypeScript, protos and packages keeps it apart.

## 2. Layout and dependency rules

| Path | What |
|---|---|
| `apps/cli` | `blitz`; `internal/tui` is the REPL |
| `apps/service` | `blitzd`; `internal/daemon` runs it, `internal/server` has the Connect handlers; `servicetest` runs it in other packages' tests (`testonly`) |
| `apps/desktop` | The desktop app (Wails v2, cgo); `web/` its page, `packaging/` its packages |
| `pkg/api` | The contract between front ends and the engine: `Backend` and what crosses it |
| `pkg/client`, `pkg/socket` | `Backend` over the service's API; where the service listens |
| `pkg/engine`, `pkg/engine/…` | The engine (`engine.Open` → `Workspace`, implementing `api.Backend`) |
| `pkg/{config,i18n,images,observability,redact,textutil}` | Shared by every app |
| `proto/blitz/v1` | The service API's protos |
| `bazel/` | Build pieces: code generators, checks, tool wrappers, stamping |
| `release/` | The CLI release archives |

- **MR-01** Apps depend on `pkg/` only, never on each other; tests may run the service through `apps/service/servicetest`. Front ends (the REPL, the desktop app), `pkg/api`, `pkg/client` and `pkg/socket` never reach the engine; only the service and the CLI's `--local` mode link it. One Go module (`github.com/retail-cortex/blitz`).
- **MR-02** Enforced twice: engine libraries' `visibility` lists only the engine, the service, the CLI's `main` package and the three packages whose tests use the engine; and `bazel run //bazel:check_deps` (CI) queries the build graph — libraries only — and fails when a front end's, the contract's or a shared package's library reaches the engine, when an app's library reaches another app, or when an engine library is public. Gazelle keeps an existing `visibility`, so new engine packages start public and fail the check until given one.

## 3. Build

- **MR-10** Bazel 9.2 (`.bazelversion`; Bazelisk picks it), bzlmod only: rules_go and Gazelle (Go from `go.mod`, dependencies through `go_deps`), protobuf, rules_buf, aspect_rules_js with rules_nodejs (Node 22, npm packages from the page's pnpm lockfile), bazel_lib, rules_pkg, rules_shell, apple_support, platforms. `MODULE.bazel.lock` is committed.
- **MR-11** `bazel run //:gazelle` writes the Go rules. Directives in `BUILD.bazel`: the prefix; protos have hand-written rules; `apps/desktop/web`, `apps/desktop/packaging`, `bin` and `dist` aren't Go. `bazel run //bazel:tidy` is the dependency upkeep: `go mod tidy -e` with Bazel's Go (`-e`: the generated proto package has no files outside the build), `bazel mod tidy`, Gazelle. Plain `go`, `pnpm` or `buf` on the host are a last resort; `bazel run @rules_go//go -- …` is the go command.
- **MR-12** Protos: `//proto/blitz/v1:blitzv1_go_proto` generates one Go package, `github.com/retail-cortex/blitz/proto/blitz/v1` (`blitzv1`), with the messages and Connect's clients and handlers (`protoc-gen-connect-go` with `package_suffix=`, `//bazel:connect_go`). `//apps/desktop/web:api_ts` generates the page's TypeScript with `protoc-gen-es` from the same `proto_library` (`bazel/es_proto.bzl`, protoc from protobuf's toolchain, from the sources so comments survive). Nothing generated is committed; plain `go build` doesn't work. Editors use `bazel/gopackagesdriver.sh` (gopls) and tsconfig's `rootDirs` (after `bazel build //apps/desktop/web:api_ts`).
- **MR-13** The page: `//apps/desktop/web:page` runs Vite (outside Bazel's sandbox: Vite resolves `index.html` to its real path, which the sandbox links outside itself); `:test` runs vitest and `:typecheck` runs `tsc`; `:dev` serves it for development. The pnpm workspace file declares that no dependency runs install scripts. The desktop binary embeds the page built for the host platform (`platform_transition_filegroup`), whatever platform the binary is for.
- **MR-14** The desktop app with cgo: Wails's `desktop,production` tags (a Gazelle directive keeps Wails's tagged files), `-framework UniformTypeIdentifiers` (`apps/desktop/link_darwin.go`), macOS 13 minimum (`.bazelrc`), apple_support's Xcode toolchains so both Mac architectures build on either Mac. On Linux, WebKitGTK 4.1 (`webkit2_41`) and GTK's flags from pkg-config (`bazel/pkg_config.bzl`, a patch to two Wails packages), since rules_go ignores `#cgo pkg-config`: the system's libraries, deliberately not hermetic.
- **MR-15** Tests: `bazel test //...` runs every Go test, the page's tests and type check, and the proto lint; `--config=race` runs the Go tests with the race detector (tests only). Tests that start commands in the OS sandbox are tagged `no-sandbox` (macOS's sandbox can't nest in Bazel's); `--test_env=RUNSC_PATH` lets the gVisor tests find `runsc`. Tests don't see the shell's environment (`--incompatible_strict_action_env`), so API keys never reach them.
- **MR-16** Static analysis runs in every compile of the repository's Go code (not dependencies or generated code): nogo with Go vet's analyzers and staticcheck's default checks (SA, S, ST but for the style checks staticcheck leaves off, and SA5011, which only works inside staticcheck); `//nolint:<check>` silences one finding. `bazel/check_format.sh` checks gofmt and that the BUILD files are Gazelle's. The protos: `//proto/blitz/v1:blitzv1_proto_lint` (buf STANDARD), and `bazel run //bazel:buf` for formatting and breaking changes. govulncheck runs on the built binaries (`-mode=binary`). staticcheck, govulncheck and buf's version are pinned in `go.mod` (`tool`) and `MODULE.bazel`.

## 4. Versions and packages

- **MR-20** `--config=release` stamps: `bazel/workspace_status.sh` reports the version (`git describe`, tag without `v`, else the commit), and the binaries take it through `x_defs` (`main.version`); unstamped builds say `dev` and stay cacheable. `//bazel:version` is the version as a file, for package metadata. The desktop page asks the app for its version (`Version()`).
- **MR-21** Binaries: `//apps/cli:blitz`, `//apps/service:blitzd`, `//apps/desktop:blitz-desktop` for the host, each with its own Mach-O UUID and, on macOS, signed ad hoc as `dev.blitz.cli`, `dev.blitz.service` and `dev.blitz.desktop` (`bazel/macos.bzl`). rules_go links with the fixed build ID `redacted`, from which Go's linker derives the UUID, and through a temporary file, which names the linker's signature `a.out`: every Go program had one identity, so macOS took them for one program — its log showed the desktop app as an old test binary, and the folder dialog's service turned it away. `-B` (from `macho_uuid_linkopts`, a fixed value per program and platform, so builds stay reproducible) sets the UUID; `signed_binary` re-signs; `Blitz.app` is signed as a bundle; and the desktop app embeds `apps/desktop/Info.plist` in its executable (`__TEXT,__info_plist`, through a `cc_library` link option), so it has a bundle identifier outside `Blitz.app` too — macOS's open-panel service dismisses the folder dialog of a host without one (`bazel run`); the unsigned `*_unsigned` binaries they come from; `blitz_<os>_<arch>` and `blitzd_<os>_<arch>` (pure Go) for darwin/linux × amd64/arm64 and windows/amd64; `blitz-desktop_darwin_{amd64,arm64}` (cgo).
- **MR-22** `//release:archives`: `blitz_<os>_<arch>.tar.gz` (`.zip` for Windows), each a folder `blitz_<os>_<arch>/` with `blitz`, `blitzd`, `blz` (a link to `blitz`; not on Windows), README, LICENSE and NOTICE.
- **MR-23** `//apps/desktop/packaging:Blitz.app` (macOS): universal `blitz-desktop`, `blitz` and `blitzd` (`lipo`), the stamped `Info.plist` (`darwin/Info.plist.in`), and `iconfile.icns` generated from `appicon.png` (`sips`, `iconutil`). `sign_macos.sh` signs a copy (Developer ID and hardened runtime, or ad hoc), makes the disk image, and notarises and staples it: outside Bazel, since it needs Apple's tools and keys.
- **MR-24** `//apps/desktop/packaging:deb` (Linux, the machine's architecture): the three programs in `/usr/lib/blitz-desktop`, `/usr/bin/blitz-desktop` linking to the app, a launcher and an icon; version from `//bazel:version`.
- **MR-25** Reproducible: the release archives are byte for byte the same when built on macOS and on Linux. Output directories are named after the target platform (`--experimental_platform_in_output_dir`), since generated Go files' paths end up in the binaries; archives have fixed owners and times (rules_pkg).

## 5. Known gaps

- The release archives' macOS binaries (`blitz_darwin_*`, cross-built on Linux) have their own UUIDs but are still signed as `a.out`: `codesign` needs macOS. Building them on the macOS runner, or signing with rcodesign on Linux, would name them (MR-21).
- The generated Go code has no doc comments (the Go proto rules compile descriptor sets without source info); the TypeScript keeps them.
- The Linux desktop build uses the system's GTK and WebKitGTK through pkg-config; a sysroot would make it hermetic.
- Plain `go build`, `go test` and `go list` don't work: tools that need them (gopls) use the packages driver.
