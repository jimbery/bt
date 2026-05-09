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

```bash
go test ./... -race
go build ./cmd/bt
```

## Module path

This repository uses `github.com/jayimbery/bt`. Replace it with your org’s import path if you fork.
