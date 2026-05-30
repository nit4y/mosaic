# Contributing to mosaic

Thanks for your interest in improving **mosaic**! This document covers the
basics for getting a change merged.

## Prerequisites

mosaic is built on [GoCV](https://gocv.io/), which requires **OpenCV 4.x**
installed locally. See the [GoCV install guide](https://gocv.io/getting-started/).
You also need a recent Go toolchain (see `go.mod` for the minimum version).

## Development workflow

```sh
make fmt     # gofmt + goimports
make test    # unit + integration tests (needs OpenCV)
make race    # tests under the race detector
make lint    # golangci-lint
make check   # fmt-check + vet + lint + race (run this before pushing)
```

If you don't have `make`, the underlying commands are:

```sh
go build ./...
go vet ./...
go test -race ./...
golangci-lint run
```

## Guidelines

- **Keep the public API small.** The exported surface is intentionally minimal
  (`Config`/`DefaultConfig`, `Logger`/`NewLogger`, `Kind`, `Direction`, and the
  `Generate*` entry points). Prefer adding unexported helpers over new exported
  symbols.
- **Mind `gocv.Mat` lifecycles.** Every `Mat` must be closed exactly once.
  Functions document who owns the returned `Mat`; follow that contract and run
  `go test -race ./...` to catch regressions.
- **Pair bug fixes with a regression test** that fails before your change and
  passes after. See `regression_test.go` for examples.
- **Comments explain the current behavior**, not the history of the code. Leave
  the changelog to commit messages.
- **Run `make check` before opening a PR.** CI runs the same checks.

## Commit messages

This repo uses [Conventional Commits](https://www.conventionalcommits.org/)
(`feat:`, `fix:`, `docs:`, `refactor:`, …); releases are derived from them.

## Reporting issues

Please include the OpenCV/GoCV versions, your OS, a short clip or description
that reproduces the problem, and the verbose logs (`NewLogger(logger, true)`).
