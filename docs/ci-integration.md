# CI integration for bt

This guide complements `.github/workflows/example-bt.yml`.

## Installing `bt`

From a clone of this repository:

```bash
go build -o bt ./cmd/bt
```

Or run without a separate binary:

```bash
go run ./cmd/bt --help
```

## Example workflow

The workflow [example-bt.yml](.github/workflows/example-bt.yml) demonstrates:

1. Checking out the repository and installing Go.
2. Starting the **orders-api** sample on port `18080` (so it does not require root for port 80).
3. Patching `examples/orders-api/bt/backendtest.yaml` in the job so `target.base_url` points at that instance.
4. Running `bt doctor` then `bt run --strategy contract`.
5. Running the full unit test suite (`go test ./...`).

## Exit codes

See [exit-codes.md](exit-codes.md). In GitHub Actions, map exit code `1` to a failed test gate and `2`/`3` to infrastructure or configuration problems.

## Reports

Use `--output json` and `--output-file` on `bt run` to archive machine-readable reports as workflow artifacts.
