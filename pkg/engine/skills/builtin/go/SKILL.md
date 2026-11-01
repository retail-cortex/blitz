---
name: go
description: Write and change Go code the way the Go project does it (layout, errors, interfaces, concurrency, tests with -race, gofmt and vet), following the module's own conventions first
tags: [go, golang, development]
version: "1.0.0"
author: "Blitz"
category: development
agents: [blitz, coder]
trigger_phrases:
  - write this in go
  - change the go code
  - add a go package
  - fix the go tests
  - golang
---
# Go

## 1. Look first

Read `go.mod` (the module path, the `go` version, the dependencies already in use) and the package you're changing. Follow what's there: its error style, its logger, its test helpers (testify or the standard library), how it builds (`go`, Bazel, a Makefile). Add a dependency only when the standard library or one already required can't do it.

## 2. Write it

- **Layout:** one package per directory, named for what it provides (`invoice`, not `utils`); `internal/` for what other modules mustn't import; `cmd/<name>/main.go` for programs. No package-level state that tests can't reset.
- **Names:** short in small scopes (`r`, `ctx`), descriptive across packages; no stutter (`invoice.New`, not `invoice.NewInvoice`); exported names get a doc comment that starts with the name.
- **Errors:** return them, don't panic. Wrap with context, `fmt.Errorf("reading %s: %w", path, err)`; compare with `errors.Is` and `errors.As`; a sentinel (`var ErrNotFound = errors.New(…)`) when callers branch on it. Handle each error once: log it or return it, not both.
- **Interfaces:** small, defined where they're used, not where they're implemented. Accept interfaces, return concrete types. Don't add one for a single implementation unless a test needs it.
- **Context:** `ctx context.Context` is the first parameter of anything that does I/O or may block; never stored in a struct; respected (`select` on `ctx.Done()` in loops).
- **Concurrency:** every goroutine has a way to stop and someone who waits for it (`sync.WaitGroup`, `errgroup`). Guard shared state with a mutex next to the fields it guards, or own it in one goroutine and use channels. The sender closes a channel, never the receiver.
- **Resources:** `defer f.Close()` right after a successful open; check the error of a `Close` that writes.
- **Generics:** for containers and algorithms that really are type-independent; not to avoid writing two small functions.

## 3. Test it

- Tests sit beside the code (`x_test.go`), table-driven with `t.Run` subtests named for the case. Use `t.TempDir()`, `t.Setenv()` and `t.Cleanup()` rather than hand-rolled setup.
- Test behaviour through the exported API; a fake of a small interface beats a mock framework.
- Run with the race detector: `go test -race ./...` (or the module's own command).

## 4. Check it

Before saying it's done: `gofmt -l .` prints nothing (or the module's formatter is clean), `go vet ./...` passes, the tests pass with `-race`, and `go mod tidy` leaves `go.mod` and `go.sum` unchanged. Say which packages changed and how you ran the checks.
