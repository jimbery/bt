# bt

`bt` is a Go-native backend testing platform for table, property, fuzz, and contract strategies.

## Quick start

```bash
go build -o bt ./cmd/bt
./bt init
./bt validate
```

Configuration lives in `backendtest.yaml` in the working directory unless `--config` is set.

## Development

Before you commit, run the same checks as CI (formatters plus linters):

```bash
make lint
```

That runs `golangci-lint fmt` (gofmt + goimports) and `golangci-lint run`. Install [golangci-lint](https://golangci-lint.run/welcome/install/) v2 locally, or rely on CI after pushing.

```bash
go test ./... -race
go build ./cmd/bt
```

## Module path

This repository uses `github.com/jayimbery/bt`. Replace it with your org’s import path if you fork.
