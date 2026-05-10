# bt exit codes

`bt` uses small integer exit codes so CI and scripts can branch on outcome.

| Code | Meaning |
|------|---------|
| 0 | Success — all executed cases passed (contract strategy: quarantined failures do not fail the run). |
| 1 | Test failures — one or more cases failed and were not skipped or quarantined. |
| 2 | Configuration or schema problem — bad config path, invalid YAML, adapter validation, plan errors, `bt doctor` blocking checks, or similar. |
| 3 | Execution error — for example `bt run` strategy execution returned a transport-layer failure wrapped as `exitcode.ErrExecution`. |

Replay and artifact helpers may still return code 2 for missing artifacts (treated as a configuration/path issue).

Implementation: see `internal/cli/exit.go` (`ExitCodeFor`) and `internal/exitcode` for contract-specific aggregation (`FromContractResults`).
